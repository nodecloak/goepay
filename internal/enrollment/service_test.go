package enrollment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

type sentMessage struct{ email, purpose, code string }
type fakeSender struct {
	mu       sync.Mutex
	messages []sentMessage
	fail     bool
}

func (f *fakeSender) SendVerification(_ context.Context, email, purpose, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("local fake transport rejected")
	}
	f.messages = append(f.messages, sentMessage{email, purpose, code})
	return nil
}
func (f *fakeSender) last(t *testing.T) sentMessage {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.messages) == 0 {
		t.Fatal("sender not invoked")
	}
	return f.messages[len(f.messages)-1]
}
func enrollmentFixture(t *testing.T) (*Service, *fakeSender) {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("real PostgreSQL required")
	}
	ctx := context.Background()
	p, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	n := make([]byte, 8)
	if _, e = rand.Read(n); e != nil {
		t.Fatal(e)
	}
	schema := "enroll_" + hex.EncodeToString(n)
	if _, e = p.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	q, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { q.Close(); p.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); p.Close() })
	if e = db.Migrate(ctx, q); e != nil {
		t.Fatal(e)
	}
	store, e := payment.NewWithKey(q, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	f := &fakeSender{}
	s, e := New(q, store, make([]byte, 32), f)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = q.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"registration_open","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	return s, f
}
func registerVerified(t *testing.T, s *Service, f *fakeSender, email, username, source string) Registered {
	t.Helper()
	ctx := context.Background()
	ch, e := s.Issue(ctx, VerificationRequest{Purpose: "register", Email: email, Source: source})
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.Register(ctx, RegisterInput{ChallengeID: ch.ID, Code: f.last(t).code, Email: email, Source: source, Username: username, Name: "Registered", Password: "correct-test-password"})
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestRegisterVerifiedZeroBalanceAndEncryptedCredentials(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	v := registerVerified(t, s, f, "New@Example.test", "new-user", "192.0.2.1")
	m, e := s.store.Merchant(ctx, v.MerchantID)
	if e != nil || len(m.APIKey) < 32 || !m.Enabled {
		t.Fatalf("merchant %+v %v", m, e)
	}
	b, e := s.store.Balance(ctx, v.MerchantID)
	if e != nil || b.Available != 0 || b.Reserved != 0 {
		t.Fatal(b, e)
	}
	var hash string
	var raw []byte
	if e = s.pool.QueryRow(ctx, "SELECT password_hash FROM console_users WHERE username='new-user' AND role='merchant'").Scan(&hash); e != nil || !identity.CheckPassword(hash, "correct-test-password") {
		t.Fatal("password hash", e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT api_key FROM merchants WHERE id=$1", v.MerchantID).Scan(&raw); e != nil || strings.Contains(string(raw), m.APIKey) {
		t.Fatal("plaintext API key", e)
	}
	var email string
	if e = s.pool.QueryRow(ctx, "SELECT email FROM enrollment_emails WHERE merchant_id=$1", v.MerchantID).Scan(&email); e != nil || email != "new@example.test" {
		t.Fatal(email, e)
	}
}
func TestVerificationIsPurposeBoundPersistentAndFiveAttempts(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	ch, e := s.Issue(ctx, VerificationRequest{Purpose: "register", Email: "otp@example.test", Source: "192.0.2.2"})
	if e != nil {
		t.Fatal(e)
	}
	code := f.last(t).code
	if len(code) != 7 || strings.Contains(ch.ID, code) {
		t.Fatal("unsafe code", ch.ID)
	}
	var stored []byte
	if e = s.pool.QueryRow(ctx, "SELECT code_hash FROM enrollment_challenges WHERE id=$1", ch.ID).Scan(&stored); e != nil || len(stored) != 32 || strings.Contains(string(stored), code) {
		t.Fatal("OTP persisted unsafely", e)
	}
	s2, e := New(s.pool, s.store, make([]byte, 32), f)
	if e != nil {
		t.Fatal(e)
	}
	in := RegisterInput{ChallengeID: ch.ID, Email: "otp@example.test", Source: "192.0.2.2", Username: "otp-user", Name: "OTP", Password: "correct-test-password", Code: "0000000"}
	for i := 0; i < 5; i++ {
		if _, e = s2.Register(ctx, in); !errors.Is(e, ErrCode) {
			t.Fatal(i, e)
		}
	}
	in.Code = code
	if _, e = s.Register(ctx, in); !errors.Is(e, ErrCode) {
		t.Fatal("exhausted challenge accepted", e)
	}
	var attempts int
	if e = s.pool.QueryRow(ctx, "SELECT attempts FROM enrollment_challenges WHERE id=$1", ch.ID).Scan(&attempts); e != nil || attempts != 5 {
		t.Fatal("failed attempts rolled back", attempts, e)
	}
}

func ageSendingCooldown(t *testing.T, s *Service) {
	t.Helper()
	if _, e := s.pool.Exec(context.Background(), "UPDATE enrollment_challenges SET created_at=created_at-interval '61 seconds'"); e != nil {
		t.Fatal(e)
	}
}
func TestEnrollmentDeliveryClosedExpiryAndPurposeFailClosed(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	nilSender, e := New(s.pool, s.store, make([]byte, 32), nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = nilSender.Issue(ctx, VerificationRequest{Purpose: "recover", Email: "unknown@example.test", Source: "192.0.2.10"}); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	f.fail = true
	if _, e = s.Issue(ctx, VerificationRequest{Purpose: "recover", Email: "failed@example.test", Source: "192.0.2.11"}); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	var state string
	if e = s.pool.QueryRow(ctx, "SELECT state FROM enrollment_challenges").Scan(&state); e != nil || state != "failed" {
		t.Fatal("delivery reported success", state, e)
	}
	f.fail = false
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"false"') WHERE resource='settings' AND body->>'key'='registration_open'`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Issue(ctx, VerificationRequest{Purpose: "register", Email: "closed@example.test", Source: "192.0.2.12"}); !errors.Is(e, ErrClosed) {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"true"') WHERE resource='settings' AND body->>'key'='registration_open'`); e != nil {
		t.Fatal(e)
	}
	ch, e := s.Issue(ctx, VerificationRequest{Purpose: "register", Email: "bound@example.test", Source: "192.0.2.12"})
	if e != nil {
		t.Fatal(e)
	}
	code := f.last(t).code
	if e = s.Recover(ctx, RecoverInput{ChallengeID: ch.ID, Code: code, Email: "bound@example.test", Source: "192.0.2.12", Password: "replacement-password"}); !errors.Is(e, ErrCode) {
		t.Fatal("cross-purpose verification accepted", e)
	}
	in := RegisterInput{ChallengeID: ch.ID, Code: code, Email: "other@example.test", Source: "192.0.2.12", Username: "bound-user", Name: "Bound", Password: "replacement-password"}
	if _, e = s.Register(ctx, in); !errors.Is(e, ErrCode) {
		t.Fatal("cross-email verification accepted", e)
	}
	in.Email = "bound@example.test"
	in.Source = "192.0.2.13"
	if _, e = s.Register(ctx, in); !errors.Is(e, ErrCode) {
		t.Fatal("cross-source verification accepted", e)
	}
	in.Source = "192.0.2.12"
	if _, e = s.pool.Exec(ctx, "UPDATE enrollment_challenges SET created_at=statement_timestamp()-interval '11 minutes',expires_at=statement_timestamp()-interval '1 minute' WHERE id=$1", ch.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Register(ctx, in); !errors.Is(e, ErrCode) {
		t.Fatal("expired code accepted", e)
	}
}

func TestEnrollmentConcurrentDeliveryAndSourceBudget(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Issue(ctx, VerificationRequest{Purpose: "recover", Email: "cooldown@example.test", Source: "192.0.2.20"})
			if e == nil {
				successes.Add(1)
			} else if !errors.Is(e, ErrLimited) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 || len(f.messages) != 1 {
		t.Fatalf("concurrent sends=%d messages=%d", successes.Load(), len(f.messages))
	}
	// Rejected attempts also use source budget; rotating mailboxes cannot send.
	if _, e := s.Issue(ctx, VerificationRequest{Purpose: "recover", Email: "rotated@example.test", Source: "192.0.2.20"}); !errors.Is(e, ErrLimited) {
		t.Fatal("source rotation escaped limit", e)
	}
	for i := 0; i < 7; i++ {
		if _, e := s.Issue(ctx, VerificationRequest{Purpose: "recover", Email: "daily@example.test", Source: "192.0.2.21"}); e != nil {
			t.Fatal(i, e)
		}
		ageSendingCooldown(t, s)
	}
	if _, e := s.Issue(ctx, VerificationRequest{Purpose: "recover", Email: "daily@example.test", Source: "192.0.2.22"}); !errors.Is(e, ErrLimited) {
		t.Fatal("mailbox budget escaped across source", e)
	}
}

func TestEnrollmentInvitationConcurrentLimitAndAtomicRollback(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"registration_invite_required","value":"true"}'),('invitation-codes','{"code":"ONE-ONLY","enabled":true,"max_uses":1}')`); e != nil {
		t.Fatal(e)
	}
	inputs := []RegisterInput{}
	for i := 0; i < 2; i++ {
		email := []string{"one@example.test", "two@example.test"}[i]
		user := []string{"one-user", "two-user"}[i]
		ch, e := s.Issue(ctx, VerificationRequest{Purpose: "register", Email: email, Source: "192.0.2.30"})
		if e != nil {
			t.Fatal(e)
		}
		inputs = append(inputs, RegisterInput{ChallengeID: ch.ID, Code: f.last(t).code, Email: email, Source: "192.0.2.30", Username: user, Name: user, Password: "correct-test-password", InviteCode: "ONE-ONLY"})
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for _, in := range inputs {
		wg.Add(1)
		go func(in RegisterInput) {
			defer wg.Done()
			_, e := s.Register(ctx, in)
			if e == nil {
				successes.Add(1)
			} else if !errors.Is(e, ErrInvalid) {
				t.Error(e)
			}
		}(in)
	}
	wg.Wait()
	var merchants, uses, consumed int
	if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM merchants),(SELECT count(*) FROM enrollment_invitation_uses),(SELECT count(*) FROM enrollment_challenges WHERE consumed_at IS NOT NULL)").Scan(&merchants, &uses, &consumed); e != nil || successes.Load() != 1 || merchants != 1 || uses != 1 || consumed != 1 {
		t.Fatalf("success=%d merchants=%d uses=%d consumed=%d error=%v", successes.Load(), merchants, uses, consumed, e)
	}
	// Failed invite validation rolls back accounts, identity and OTP consumption.
	if _, e := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{max_uses}','2') WHERE resource='invitation-codes'`); e != nil {
		t.Fatal(e)
	}
	for _, in := range inputs {
		var used bool
		if e := s.pool.QueryRow(ctx, "SELECT consumed_at IS NOT NULL FROM enrollment_challenges WHERE id=$1", in.ChallengeID).Scan(&used); e != nil {
			t.Fatal(e)
		}
		if !used {
			if _, e := s.Register(ctx, in); e != nil {
				t.Fatal("failed transaction consumed OTP", e)
			}
		}
	}
}

func TestEnrollmentArchivedInvitationNeverAdmitsEvenIfEnabled(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('invitation-codes','{"code":"ARCHIVED-ADMISSION","enabled":true,"archived":true,"max_uses":1}')`); e != nil {
		t.Fatal(e)
	}
	ch, e := s.Issue(ctx, VerificationRequest{Purpose: "register", Email: "archived@example.test", Source: "192.0.2.41"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Register(ctx, RegisterInput{ChallengeID: ch.ID, Code: f.last(t).code, Email: "archived@example.test", Source: "192.0.2.41", Username: "archived-user", Name: "Archived", Password: "correct-test-password", InviteCode: "ARCHIVED-ADMISSION"})
	if !errors.Is(e, ErrInvalid) {
		t.Fatalf("archived invitation admitted: %v", e)
	}
	var merchants, uses, consumed int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM merchants),(SELECT count(*) FROM enrollment_invitation_uses),(SELECT count(*) FROM enrollment_challenges WHERE consumed_at IS NOT NULL)`).Scan(&merchants, &uses, &consumed); e != nil || merchants != 0 || uses != 0 || consumed != 0 {
		t.Fatalf("rejected enrollment changed facts %d %d %d %v", merchants, uses, consumed, e)
	}
}

func TestEnrollmentRecoveryBoundIdentityRevokesSessionsPreservesMFA(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	u := registerVerified(t, s, f, "restore@example.test", "restore-user", "192.0.2.40")
	sessions := identity.NewPersistentSessions(s.pool, false)
	token, _, e := sessions.Create(identity.User{Username: u.Username, Role: "merchant", ID: u.MerchantID, AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	secret := []byte("existing-encrypted-totp-secret")
	if _, e = s.pool.Exec(ctx, "INSERT INTO console_mfa(username,role,secret,last_counter) VALUES($1,'merchant',$2,42)", u.Username, secret); e != nil {
		t.Fatal(e)
	}
	ageSendingCooldown(t, s)
	ch, e := s.Issue(ctx, VerificationRequest{Purpose: "recover", Email: "RESTORE@example.test", Source: "192.0.2.40"})
	if e != nil {
		t.Fatal(e)
	}
	in := RecoverInput{ChallengeID: ch.ID, Code: f.last(t).code, Email: "restore@example.test", Source: "192.0.2.40", Password: "replacement-password"}
	if e = s.Recover(ctx, in); e != nil {
		t.Fatal(e)
	}
	if _, ok := sessions.Get(token, "merchant"); ok {
		t.Fatal("old session survived password recovery")
	}
	var hash string
	var epoch int64
	var remaining int
	var after []byte
	if e = s.pool.QueryRow(ctx, "SELECT password_hash,auth_version FROM console_users WHERE username=$1 AND role='merchant'", u.Username).Scan(&hash, &epoch); e != nil || !identity.CheckPassword(hash, "replacement-password") || epoch != 2 {
		t.Fatal("password/epoch", epoch, e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT secret FROM console_mfa WHERE username=$1", u.Username).Scan(&after); e != nil || string(after) != string(secret) {
		t.Fatal("recovery bypassed MFA", e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM console_sessions WHERE subject_id=$1", u.MerchantID).Scan(&remaining); e != nil || remaining != 0 {
		t.Fatal("sessions not deleted", remaining, e)
	}
	if e = s.Recover(ctx, in); !errors.Is(e, ErrCode) {
		t.Fatal("consumed code replayed", e)
	}
}

func TestEnrollmentUnknownRecoveryAndProfileEmailCannotAuthorize(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	if e := s.store.CreateMerchant(ctx, payment.Merchant{ID: 800, Name: "Legacy", APIKey: "legacy-secret"}); e != nil {
		t.Fatal(e)
	}
	hash, e := identity.HashPassword("original-password")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('legacy','merchant',800,'Legacy',$1);`, hash); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET profile='{"email":"profile@example.test"}' WHERE id=800`); e != nil {
		t.Fatal(e)
	}
	for _, email := range []string{"profile@example.test", "unknown@example.test"} {
		ch, e := s.Issue(ctx, VerificationRequest{Purpose: "recover", Email: email, Source: "192.0.2.50"})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Recover(ctx, RecoverInput{ChallengeID: ch.ID, Code: f.last(t).code, Email: email, Source: "192.0.2.50", Password: "replacement-password"}); e != nil {
			t.Fatal("enumerating recovery result", email, e)
		}
	}
	var after string
	if e = s.pool.QueryRow(ctx, "SELECT password_hash FROM console_users WHERE username='legacy'").Scan(&after); e != nil || after != hash {
		t.Fatal("unverified profile email authorized reset", e)
	}
}

func TestEnrollmentResendAndCredentialChangeInvalidateOlderProof(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	u := registerVerified(t, s, f, "fresh@example.test", "fresh-user", "192.0.2.80")
	ageSendingCooldown(t, s)
	old, e := s.Issue(ctx, VerificationRequest{Purpose: "recover", Email: "fresh@example.test", Source: "192.0.2.80"})
	if e != nil {
		t.Fatal(e)
	}
	oldCode := f.last(t).code
	ageSendingCooldown(t, s)
	newer, e := s.Issue(ctx, VerificationRequest{Purpose: "recover", Email: "fresh@example.test", Source: "192.0.2.80"})
	if e != nil {
		t.Fatal(e)
	}
	newCode := f.last(t).code
	if e = s.Recover(ctx, RecoverInput{ChallengeID: old.ID, Code: oldCode, Email: "fresh@example.test", Source: "192.0.2.80", Password: "stolen-old-password"}); !errors.Is(e, ErrCode) {
		t.Fatal("resend left older OTP usable", e)
	}
	hash, e := identity.HashPassword("new-current-password")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE console_users SET password_hash=$1 WHERE username=$2 AND role='merchant'", hash, u.Username); e != nil {
		t.Fatal(e)
	}
	// A password change after issuance revokes that outstanding recovery proof,
	// while preserving the non-enumerating completed response.
	if e = s.Recover(ctx, RecoverInput{ChallengeID: newer.ID, Code: newCode, Email: "fresh@example.test", Source: "192.0.2.80", Password: "stale-proof-password"}); e != nil {
		t.Fatal(e)
	}
	var after string
	if e = s.pool.QueryRow(ctx, "SELECT password_hash FROM console_users WHERE username=$1 AND role='merchant'", u.Username).Scan(&after); e != nil || after != hash {
		t.Fatal("pre-change OTP reset newer credentials", e)
	}
}

func TestEnrollmentRegistrationRollbackKeepsProofAndInvitation(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('invitation-codes','{"code":"ROLLBACK","enabled":true,"max_uses":1}');
	 CREATE FUNCTION fail_enrollment_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='enrollment' THEN RAISE EXCEPTION 'test audit fault'; END IF; RETURN NEW; END $$;
	 CREATE TRIGGER fail_enrollment_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION fail_enrollment_audit()`); e != nil {
		t.Fatal(e)
	}
	ch, e := s.Issue(ctx, VerificationRequest{Purpose: "register", Email: "atomic@example.test", Source: "192.0.2.90"})
	if e != nil {
		t.Fatal(e)
	}
	in := RegisterInput{ChallengeID: ch.ID, Code: f.last(t).code, Email: "atomic@example.test", Source: "192.0.2.90", Username: "atomic-user", Name: "Atomic", Password: "correct-test-password", InviteCode: "ROLLBACK"}
	if _, e = s.Register(ctx, in); e == nil {
		t.Fatal("injected audit failure ignored")
	}
	var merchants, accounts, users, claims, uses, consumed int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM merchants),(SELECT count(*) FROM accounts WHERE owner_id<>0),(SELECT count(*) FROM console_users),(SELECT count(*) FROM enrollment_emails),(SELECT count(*) FROM enrollment_invitation_uses),(SELECT count(*) FROM enrollment_challenges WHERE consumed_at IS NOT NULL)`).Scan(&merchants, &accounts, &users, &claims, &uses, &consumed); e != nil || merchants+accounts+users+claims+uses+consumed != 0 {
		t.Fatalf("partial registration committed: %d %d %d %d %d %d %v", merchants, accounts, users, claims, uses, consumed, e)
	}
	if _, e = s.pool.Exec(ctx, "DROP TRIGGER fail_enrollment_audit ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Register(ctx, in); e != nil {
		t.Fatal("transaction failure burned proof/invitation", e)
	}
}

func TestEnrollmentDuplicateIdentityAndConcurrentProofConsumption(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	ch, e := s.Issue(ctx, VerificationRequest{Purpose: "register", Email: "unique@example.test", Source: "192.0.2.91"})
	if e != nil {
		t.Fatal(e)
	}
	in := RegisterInput{ChallengeID: ch.ID, Code: f.last(t).code, Email: "unique@example.test", Source: "192.0.2.91", Username: "unique-user", Name: "Unique", Password: "correct-test-password"}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Register(ctx, in)
			if e == nil {
				successes.Add(1)
			} else if !errors.Is(e, ErrCode) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("proof consumption was not atomic", successes.Load())
	}
	for _, same := range []string{"email", "username"} {
		email, user := "other@example.test", "other-user"
		if same == "email" {
			email = "unique@example.test"
			ageSendingCooldown(t, s)
		} else {
			user = "unique-user"
		}
		ch, e := s.Issue(ctx, VerificationRequest{Purpose: "register", Email: email, Source: "192.0.2.92"})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.Register(ctx, RegisterInput{ChallengeID: ch.ID, Code: f.last(t).code, Email: email, Source: "192.0.2.92", Username: user, Name: "Duplicate", Password: "correct-test-password"}); !errors.Is(e, ErrConflict) {
			t.Fatal("duplicate identity accepted", same, e)
		}
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM merchants").Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate attempt left merchant", count, e)
	}
}

func TestEnrollmentRegistrationRechecksOpenPolicyAtCompletion(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	ch, e := s.Issue(ctx, VerificationRequest{Purpose: "register", Email: "policy@example.test", Source: "192.0.2.93"})
	if e != nil {
		t.Fatal(e)
	}
	in := RegisterInput{ChallengeID: ch.ID, Code: f.last(t).code, Email: "policy@example.test", Source: "192.0.2.93", Username: "policy-user", Name: "Policy", Password: "correct-test-password"}
	if _, e = s.pool.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'='registration_open'`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Register(ctx, in); !errors.Is(e, ErrClosed) {
		t.Fatal("missing policy opened registration", e)
	}
	c, e := s.Capabilities(ctx)
	if e != nil || c.RegistrationOpen {
		t.Fatal(c, e)
	}
	if _, e = s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"registration_open","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Register(ctx, in); e != nil {
		t.Fatal("closed registration consumed proof", e)
	}
}

func TestEnrollmentRecoveryAuditFailureRollsBackCredentialsAndSessions(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	u := registerVerified(t, s, f, "recover-atomic@example.test", "recover-atomic", "192.0.2.94")
	sessions := identity.NewPersistentSessions(s.pool, false)
	token, _, e := sessions.Create(identity.User{Username: u.Username, Role: "merchant", ID: u.MerchantID, AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	ageSendingCooldown(t, s)
	ch, e := s.Issue(ctx, VerificationRequest{Purpose: "recover", Email: "recover-atomic@example.test", Source: "192.0.2.94"})
	if e != nil {
		t.Fatal(e)
	}
	in := RecoverInput{ChallengeID: ch.ID, Code: f.last(t).code, Email: "recover-atomic@example.test", Source: "192.0.2.94", Password: "replacement-password"}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION fail_recovery_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation='password-recovery' THEN RAISE EXCEPTION 'test recovery fault'; END IF; RETURN NEW; END $$;
	 CREATE TRIGGER fail_recovery_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION fail_recovery_audit()`); e != nil {
		t.Fatal(e)
	}
	if e = s.Recover(ctx, in); e == nil {
		t.Fatal("audit fault ignored")
	}
	if _, ok := sessions.Get(token, "merchant"); !ok {
		t.Fatal("failed recovery revoked sessions outside transaction")
	}
	var hash string
	var version int64
	var consumed bool
	if e = s.pool.QueryRow(ctx, "SELECT password_hash,auth_version FROM console_users WHERE username=$1 AND role='merchant'", u.Username).Scan(&hash, &version); e != nil || version != 1 || !identity.CheckPassword(hash, "correct-test-password") {
		t.Fatal("failed recovery changed credentials", version, e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT consumed_at IS NOT NULL FROM enrollment_challenges WHERE id=$1", ch.ID).Scan(&consumed); e != nil || consumed {
		t.Fatal("failed recovery consumed proof", e)
	}
	if _, e = s.pool.Exec(ctx, "DROP TRIGGER fail_recovery_audit ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	if e = s.Recover(ctx, in); e != nil {
		t.Fatal(e)
	}
	if _, ok := sessions.Get(token, "merchant"); ok {
		t.Fatal("successful recovery retained old session")
	}
}
