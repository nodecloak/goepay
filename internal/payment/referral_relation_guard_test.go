package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestReferralTokenConcurrentCASIsOwnerOnlyAndOneAuditedVersion(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 901, Name: "local promoter", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	actor := TransferBatchActor{Role: "merchant", ID: 901, Name: "local-promoter"}
	for _, bad := range []TransferBatchActor{{Role: "merchant", ID: 902, Name: "other"}, {Role: "admin", ID: 1, Name: "admin", Class: "super"}, {}} {
		if _, e := s.RotateReferralToken(ctx, 901, 0, bad); !errors.Is(e, ErrInvalid) {
			t.Fatal("nonowner issued token", e)
		}
	}
	var wg sync.WaitGroup
	var successes atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.RotateReferralToken(ctx, 901, 0, actor); e == nil {
				successes.Add(1)
			} else if !errors.Is(e, ErrConflict) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	var tokens, audits int
	if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM referral_tokens),(SELECT count(*) FROM backoffice_audit WHERE resource='referral-tokens')").Scan(&tokens, &audits); e != nil || tokens != 1 || audits != 1 || successes.Load() != 1 {
		t.Fatal(tokens, audits, successes.Load(), e)
	}
	if _, e := s.pool.Exec(ctx, "UPDATE merchants SET enabled=false WHERE id=901"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.RotateReferralToken(ctx, 901, 1, actor); !errors.Is(e, ErrState) {
		t.Fatal("disabled owner rotated token", e)
	}
}
func TestReferralTokenAuditFailureDoesNotRevokePriorBearer(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 901, Name: "local promoter", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	actor := TransferBatchActor{Role: "merchant", ID: 901, Name: "local-promoter"}
	original, e := s.RotateReferralToken(ctx, 901, 0, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION reject_referral_token_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='referral-tokens' THEN RAISE EXCEPTION 'local token audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_referral_token_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_referral_token_audit()`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.RotateReferralToken(ctx, 901, 1, actor); e == nil {
		t.Fatal("unaudited rotation committed")
	}
	if _, e = s.RevokeReferralToken(ctx, 901, 1, actor); e == nil {
		t.Fatal("unaudited revoke committed")
	}
	current, e := s.ReferralToken(ctx, 901, actor)
	if e != nil || current.Version != 1 || current.ID != original.ID || !current.Enabled {
		t.Fatal(current, e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	challenge := referralRegistrationBirth(t, s, tx, 902)
	if _, e = s.BindReferralAtRegistrationTx(ctx, tx, 902, original.Token, challenge); e != nil {
		t.Fatal("audit failure revoked a previously valid token", e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
}
func TestReferralHistoricalRelationSurvivesRotationRevocationAndPromoterDisable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	r := referralBoundMerchants(t, s, 901, 902)
	_ = referralBoundMerchants(t, s, 903, 904)
	actor := TransferBatchActor{Role: "merchant", ID: 901, Name: "local-referral-promoter"}
	if _, e := s.RevokeReferralToken(ctx, 901, 1, actor); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, "UPDATE merchants SET enabled=false WHERE id=901"); e != nil {
		t.Fatal(e)
	}
	rows, e := s.ReferralInvitees(ctx, 901, 0, 1, actor)
	if e != nil || len(rows) != 1 || rows[0].ID != r.ID || rows[0].PromoterID != 901 || rows[0].InvitedID != 902 {
		t.Fatal(rows, e)
	}
	raw, _ := json.Marshal(rows)
	for _, bad := range []string{r.ChallengeID, "proof_hash", "challenge_id", "token_cipher", "@example.test"} {
		if strings.Contains(string(raw), bad) {
			t.Fatal("invitation list disclosed private registration evidence")
		}
	}
	rows, e = s.ReferralInvitees(ctx, 901, r.ID, 1, actor)
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	if _, e = s.ReferralInvitees(ctx, 903, 0, 10, actor); !errors.Is(e, ErrInvalid) {
		t.Fatal("promoter listed another owner's invitees", e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = s.BindReferralAtRegistrationTx(ctx, tx, 901, "rp_"+strings.Repeat("A", 43), strings.Repeat("C", 43)); !errors.Is(e, ErrState) {
		t.Fatal("old merchant could add a parent", e)
	}
}
func TestReferralRelationUnkeyedOriginalRegistrationCannotBeAdopted(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 901, Name: "local promoter", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	token, e := s.RotateReferralToken(ctx, 901, 0, TransferBatchActor{Role: "merchant", ID: 901, Name: "local-promoter"})
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	challenge := referralRegistrationBirth(t, s, tx, 902)
	var id int64
	if e = tx.QueryRow(ctx, "INSERT INTO referral_relations(invited_id,promoter_id,token_id,token_version,challenge_id,proof_hash) VALUES(902,901,$1,1,$2,$3) RETURNING id", token.ID, challenge, make([]byte, 32)).Scan(&id); e != nil {
		t.Fatal(e)
	}
	changes := fmt.Sprintf(`{"invited_id":"902","promoter_id":"901","token_version":"1"}`)
	if _, e = tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('public',902,'','referral-relations',$1,'bind',$2)", id, changes); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	read, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer read.Rollback(ctx)
	if _, e = s.ReferralRelationTx(ctx, read, 902); !errors.Is(e, ErrConflict) {
		t.Fatal("SQL shape was accepted without the original keyed binding", e)
	}
	if _, e = s.ReferralInvitees(ctx, 901, 0, 10, TransferBatchActor{Role: "merchant", ID: 901, Name: "local-promoter"}); !errors.Is(e, ErrConflict) {
		t.Fatal("unkeyed relation escaped through listing", e)
	}
}
func TestReferralDatabaseBirthGuardRejectsSelfAndCycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	challenges := map[int64]string{901: referralRegistrationBirth(t, s, tx, 901), 902: referralRegistrationBirth(t, s, tx, 902)}
	ids := map[int64]int64{}
	for _, mid := range []int64{901, 902} {
		var tokenID int64
		if e = tx.QueryRow(ctx, "INSERT INTO referral_tokens(merchant_id,version,enabled,token_hash,token_cipher,token_masked,actor_name,proof_hash,expires_at) VALUES($1,1,true,$2,$3,'rp_****test','db-boundary',$4,clock_timestamp()+interval '1 day') RETURNING id", mid, []byte(strings.Repeat(fmt.Sprint(mid%10), 32)), make([]byte, 74), make([]byte, 32)).Scan(&tokenID); e != nil {
			t.Fatal(e)
		}
		ids[mid] = tokenID
		if _, e = tx.Exec(ctx, "INSERT INTO referral_token_current(merchant_id,token_id,version) VALUES($1,$2,1)", mid, ids[mid]); e != nil {
			t.Fatal(e)
		}
	}
	self, e := tx.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = self.Exec(ctx, "INSERT INTO referral_relations(invited_id,promoter_id,token_id,token_version,challenge_id,proof_hash) VALUES(901,901,$1,1,$2,$3)", ids[901], challenges[901], make([]byte, 32)); e == nil {
		t.Fatal("self referral entered the graph")
	}
	if e = self.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "INSERT INTO referral_relations(invited_id,promoter_id,token_id,token_version,challenge_id,proof_hash) VALUES(901,902,$1,1,$2,$3)", ids[902], challenges[901], make([]byte, 32)); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "INSERT INTO referral_relations(invited_id,promoter_id,token_id,token_version,challenge_id,proof_hash) VALUES(902,901,$1,1,$2,$3)", ids[901], challenges[902], make([]byte, 32)); e == nil {
		t.Fatal("a cycle entered the graph")
	}
}
