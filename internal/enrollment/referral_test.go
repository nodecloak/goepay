package enrollment

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/payment"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func referralPromoter(t *testing.T, s *Service) payment.IssuedReferralToken {
	t.Helper()
	ctx := context.Background()
	if e := s.store.CreateMerchant(ctx, payment.Merchant{ID: 901, Name: "local promoter", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	r, e := s.store.RotateReferralToken(ctx, 901, 0, payment.TransferBatchActor{Role: "merchant", ID: 901, Name: "local-promoter"})
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func TestRegistrationReferralAuditFaultRollsBackConsumedProofAndAdmission(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	promoter := referralPromoter(t, s)
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('invitation-codes','{"code":"RELATION-FAULT","enabled":true,"max_uses":1}');
	CREATE FUNCTION reject_referral_bind_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='referral-relations' THEN RAISE EXCEPTION 'local relation audit failure'; END IF; RETURN NEW; END $$;
	CREATE TRIGGER reject_referral_bind_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_referral_bind_audit()`); e != nil {
		t.Fatal(e)
	}
	in := referralRegistrationInput(t, s, f, "referral-fault@example.test", "referral-fault")
	in.InviteCode, in.PromotionToken = "RELATION-FAULT", promoter.Token
	if _, e := s.Register(ctx, in); e == nil {
		t.Fatal("relation was committed without its original actor audit")
	}
	var children, accounts, users, uses, relations, consumed int
	if e := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM merchants WHERE id<>901),(SELECT count(*) FROM accounts WHERE owner_id<>0 AND owner_id<>901),
	(SELECT count(*) FROM console_users WHERE role='merchant'),(SELECT count(*) FROM enrollment_invitation_uses),(SELECT count(*) FROM referral_relations),
	(SELECT count(*) FROM enrollment_challenges WHERE consumed_at IS NOT NULL)`).Scan(&children, &accounts, &users, &uses, &relations, &consumed); e != nil || children+accounts+users+uses+relations+consumed != 0 {
		t.Fatal("audit failure left partial registration", children, accounts, users, uses, relations, consumed, e)
	}
	if _, e := s.pool.Exec(ctx, "DROP TRIGGER reject_referral_bind_audit ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Register(ctx, in); e != nil {
		t.Fatal("rolled back OTP/admission could not be retried", e)
	}
}

func TestRegistrationConcurrentReferralConsumesOriginalProofOnce(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	promoter := referralPromoter(t, s)
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('invitation-codes','{"code":"ONE-REFERRED","enabled":true,"max_uses":1}')`); e != nil {
		t.Fatal(e)
	}
	in := referralRegistrationInput(t, s, f, "parallel-referral@example.test", "parallel-referral")
	in.InviteCode, in.PromotionToken = "ONE-REFERRED", promoter.Token
	var wg sync.WaitGroup
	var success atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Register(ctx, in)
			if e == nil {
				success.Add(1)
			} else if !errors.Is(e, ErrCode) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	var children, users, uses, relations, consumed, audits, journals int
	if e := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM merchants WHERE id<>901),(SELECT count(*) FROM console_users WHERE role='merchant'),
	(SELECT count(*) FROM enrollment_invitation_uses),(SELECT count(*) FROM referral_relations),(SELECT count(*) FROM enrollment_challenges WHERE consumed_at IS NOT NULL),
	(SELECT count(*) FROM backoffice_audit WHERE resource='referral-relations'),(SELECT count(*) FROM journals)`).Scan(&children, &users, &uses, &relations, &consumed, &audits, &journals); e != nil || success.Load() != 1 || children != 1 || users != 1 || uses != 1 || relations != 1 || consumed != 1 || audits != 1 || journals != 0 {
		t.Fatal("duplicate or partial referral registration", success.Load(), children, users, uses, relations, consumed, audits, journals, e)
	}
}

func TestRegistrationRevokedAndDisabledPromoterDoNotConsumeProof(t *testing.T) {
	for _, mode := range []string{"revoked", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			s, f := enrollmentFixture(t)
			ctx := context.Background()
			promoter := referralPromoter(t, s)
			if mode == "revoked" {
				if _, e := s.store.RevokeReferralToken(ctx, 901, 1, payment.TransferBatchActor{Role: "merchant", ID: 901, Name: "local-promoter"}); e != nil {
					t.Fatal(e)
				}
			} else if _, e := s.pool.Exec(ctx, "UPDATE merchants SET enabled=false WHERE id=901"); e != nil {
				t.Fatal(e)
			}
			in := referralRegistrationInput(t, s, f, mode+"@example.test", mode+"-referral")
			in.PromotionToken = promoter.Token
			if _, e := s.Register(ctx, in); !errors.Is(e, ErrInvalid) {
				t.Fatal("unavailable promoter accepted", e)
			}
			var children, consumed int
			if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM merchants WHERE id<>901),(SELECT count(*) FROM enrollment_challenges WHERE consumed_at IS NOT NULL)").Scan(&children, &consumed); e != nil || children != 0 || consumed != 0 {
				t.Fatal(children, consumed, e)
			}
			in.PromotionToken = ""
			if _, e := s.Register(ctx, in); e != nil {
				t.Fatal("optional promotion failure consumed proof", e)
			}
		})
	}
}

func TestPublicRegistrationReferralIsExplicitStrictJSONAndNeverEchoed(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	promoter := referralPromoter(t, s)
	h := NewHandler(s, "http://localhost")
	for i, withToken := range []bool{true, false} {
		email, username := "referral-http@example.test", "referral-http"
		if i > 0 {
			ageSendingCooldown(t, s)
			email, username = "query-only@example.test", "query-only"
		}
		w := publicRequest(t, h, "POST", "verification-code", `{"purpose":"register","email":"`+email+`"}`, "http://localhost")
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		var ch struct {
			Data Challenge `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &ch); e != nil {
			t.Fatal(e)
		}
		body := map[string]any{"challenge_id": ch.Data.ID, "code": f.last(t).code, "email": email, "username": username, "name": "local referred HTTP", "password": "correct-test-password"}
		if withToken {
			body["promoter_id"] = "901"
			raw, _ := json.Marshal(body)
			w = publicRequest(t, h, "POST", "register", string(raw), "http://localhost")
			if w.Code != 400 {
				t.Fatal("caller-controlled promoter accepted", w.Code)
			}
			delete(body, "promoter_id")
			body["promotion_token"] = promoter.Token
		}
		raw, _ := json.Marshal(body)
		w = publicRequest(t, h, "POST", "register?promotion_token=rp_untrusted&promoter_id=901", string(raw), "http://localhost")
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), promoter.Token) || strings.Contains(w.Body.String(), "proof_hash") || len(w.Result().Cookies()) != 0 {
			t.Fatal("registration exposed referral credential or session")
		}
	}
	var relations, journals int
	if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM referral_relations),(SELECT count(*) FROM journals)").Scan(&relations, &journals); e != nil || relations != 1 || journals != 0 {
		t.Fatal("query string promoted an unbound account", relations, journals, e)
	}
}
func referralRegistrationInput(t *testing.T, s *Service, f *fakeSender, email, user string) RegisterInput {
	t.Helper()
	ctx := context.Background()
	ch, e := s.Issue(ctx, VerificationRequest{Purpose: "register", Email: email, Source: "192.0.2.175"})
	if e != nil {
		t.Fatal(e)
	}
	return RegisterInput{ChallengeID: ch.ID, Code: f.last(t).code, Email: email, Source: "192.0.2.175", Username: user, Name: "Local referred user", Password: "correct-test-password"}
}
func TestRegistrationPromotionTokenIsIndependentFromAdmissionAndBindsVerifiedBirth(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	promoter := referralPromoter(t, s)
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"registration_invite_required","value":"true"}'),('invitation-codes','{"code":"ADMISSION","enabled":true,"max_uses":1}')`); e != nil {
		t.Fatal(e)
	}
	in := referralRegistrationInput(t, s, f, "referred@example.test", "referred-user")
	in.InviteCode = "ADMISSION"
	in.PromotionToken = promoter.Token
	registered, e := s.Register(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	relation, e := s.store.ReferralRelationTx(ctx, tx, registered.MerchantID)
	if e != nil || relation.PromoterID != 901 || relation.TokenID != promoter.ID || relation.TokenVersion != 1 || relation.ChallengeID != in.ChallengeID {
		t.Fatal("verified registration did not bind its original promotion token", relation, e)
	}
	var uses, consumed, rewards int
	if e = tx.QueryRow(ctx, "SELECT (SELECT count(*) FROM enrollment_invitation_uses WHERE merchant_id=$1),(SELECT count(*) FROM enrollment_challenges WHERE id=$2 AND consumed_at IS NOT NULL),(SELECT count(*) FROM journals)", registered.MerchantID, in.ChallengeID).Scan(&uses, &consumed, &rewards); e != nil || uses != 1 || consumed != 1 || rewards != 0 {
		t.Fatal(uses, consumed, rewards, e)
	}
	b, e := s.store.Balance(ctx, registered.MerchantID)
	if e != nil || b.Available != 0 || b.Reserved != 0 {
		t.Fatal(b, e)
	}
}
func TestRegistrationInvalidPromotionRollsBackAccountInvitationAndOTPConsumption(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	promoter := referralPromoter(t, s)
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('invitation-codes','{"code":"ATOMIC-ADMIT","enabled":true,"max_uses":1}')`); e != nil {
		t.Fatal(e)
	}
	in := referralRegistrationInput(t, s, f, "atomic-referral@example.test", "atomic-referral")
	in.InviteCode = "ATOMIC-ADMIT"
	in.PromotionToken = "rp_" + strings.Repeat("A", 43)
	if _, e := s.Register(ctx, in); !errors.Is(e, ErrInvalid) {
		t.Fatal("invalid promotion token was silently dropped", e)
	}
	var accounts, users, uses, relations, consumed int
	if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM accounts WHERE owner_id<>0 AND owner_id<>901),(SELECT count(*) FROM console_users WHERE role='merchant'),(SELECT count(*) FROM enrollment_invitation_uses),(SELECT count(*) FROM referral_relations),(SELECT count(*) FROM enrollment_challenges WHERE consumed_at IS NOT NULL)").Scan(&accounts, &users, &uses, &relations, &consumed); e != nil || accounts+users+uses+relations+consumed != 0 {
		t.Fatal("invalid promotion committed partial registration", accounts, users, uses, relations, consumed, e)
	}
	in.PromotionToken = promoter.Token
	if _, e := s.Register(ctx, in); e != nil {
		t.Fatal("failed promotion burned the verified proof/admission code", e)
	}
}
