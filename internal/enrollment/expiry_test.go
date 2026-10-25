package enrollment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

// The host and PostgreSQL clocks need not agree. The Go deadline below only
// bounds the test; both actual expiry and the observed blocker are proven by PG.
func waitEnrollmentBlocked(t *testing.T, s *Service, pid int) {
	t.Helper()
	ctx := context.Background()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		var blocked bool
		if e := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid)))", pid).Scan(&blocked); e != nil {
			t.Fatal(e)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("enrollment operation never waited for the held row")
}
func waitEnrollmentExpired(t *testing.T, locker pgx.Tx, id string) {
	t.Helper()
	ctx := context.Background()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		var expired bool
		if e := locker.QueryRow(ctx, "SELECT clock_timestamp()>=expires_at FROM enrollment_challenges WHERE id=$1", id).Scan(&expired); e != nil {
			t.Fatal(e)
		}
		if expired {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("database did not observe expiry while blocker remained locked")
}
func shortenEnrollmentExpiry(t *testing.T, s *Service, id string) {
	t.Helper()
	if _, e := s.pool.Exec(context.Background(), "UPDATE enrollment_challenges SET expires_at=clock_timestamp()+interval '2 seconds',attempts=2 WHERE id=$1", id); e != nil {
		t.Fatal(e)
	}
}
func assertEnrollmentNotConsumed(t *testing.T, s *Service, id string) {
	t.Helper()
	var consumed bool
	var attempts int
	if e := s.pool.QueryRow(context.Background(), "SELECT consumed_at IS NOT NULL,attempts FROM enrollment_challenges WHERE id=$1", id).Scan(&consumed, &attempts); e != nil || consumed || attempts != 2 {
		t.Fatalf("expired proof changed: consumed=%v attempts=%d error=%v", consumed, attempts, e)
	}
}
func TestEnrollmentExpiryAfterChallengeRowLockWait(t *testing.T) {
	for _, purpose := range []string{"register", "recover"} {
		t.Run(purpose, func(t *testing.T) {
			s, f := enrollmentFixture(t)
			ctx := context.Background()
			email := "expiry@example.test"
			source := "192.0.2.110"
			if purpose == "recover" {
				registerVerified(t, s, f, email, "expiry-user", source)
				ageSendingCooldown(t, s)
			}
			ch, e := s.Issue(ctx, VerificationRequest{Purpose: purpose, Email: email, Source: source})
			if e != nil {
				t.Fatal(e)
			}
			code := f.last(t).code
			shortenEnrollmentExpiry(t, s, ch.ID)
			locker, e := s.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer locker.Rollback(ctx)
			var pid int
			if e = locker.QueryRow(ctx, "SELECT pg_backend_pid() FROM enrollment_challenges WHERE id=$1 FOR UPDATE", ch.ID).Scan(&pid); e != nil {
				t.Fatal(e)
			}
			result := make(chan error, 1)
			go func() {
				if purpose == "register" {
					_, e := s.Register(ctx, RegisterInput{ChallengeID: ch.ID, Code: code, Email: email, Source: source, Username: "expiry-user", Name: "Expiry", Password: "replacement-password"})
					result <- e
				} else {
					result <- s.Recover(ctx, RecoverInput{ChallengeID: ch.ID, Code: code, Email: email, Source: source, Password: "replacement-password"})
				}
			}()
			waitEnrollmentBlocked(t, s, pid)
			waitEnrollmentExpired(t, locker, ch.ID)
			if e = locker.Rollback(ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-result:
			case <-time.After(5 * time.Second):
				t.Fatal("enrollment did not finish after unlock")
			}
			if !errors.Is(e, ErrCode) {
				t.Fatalf("%s accepted proof after lock-held expiry: %v", purpose, e)
			}
			assertEnrollmentNotConsumed(t, s, ch.ID)
			if purpose == "register" {
				var n int
				if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM merchants").Scan(&n); e != nil || n != 0 {
					t.Fatal("expired proof registered merchant", n, e)
				}
			} else {
				var hash string
				if e = s.pool.QueryRow(ctx, "SELECT password_hash FROM console_users WHERE username='expiry-user' AND role='merchant'").Scan(&hash); e != nil || !identity.CheckPassword(hash, "correct-test-password") {
					t.Fatal("expired proof changed password", e)
				}
			}
		})
	}
}
func TestEnrollmentExpiryDuringInvitationLockRollsBackRegistration(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	var invitationID int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('invitation-codes','{"code":"EXPIRY","enabled":true,"max_uses":1}') RETURNING id`).Scan(&invitationID); e != nil {
		t.Fatal(e)
	}
	ch, e := s.Issue(ctx, VerificationRequest{Purpose: "register", Email: "invite-expiry@example.test", Source: "192.0.2.111"})
	if e != nil {
		t.Fatal(e)
	}
	code := f.last(t).code
	shortenEnrollmentExpiry(t, s, ch.ID)
	locker, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer locker.Rollback(ctx)
	var pid int
	if e = locker.QueryRow(ctx, "SELECT pg_backend_pid() FROM backoffice_records WHERE id=$1 FOR UPDATE", invitationID).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	result := make(chan error, 1)
	go func() {
		_, e := s.Register(ctx, RegisterInput{ChallengeID: ch.ID, Code: code, Email: "invite-expiry@example.test", Source: "192.0.2.111", Username: "invite-expiry", Name: "Invite Expiry", Password: "correct-test-password", InviteCode: "EXPIRY"})
		result <- e
	}()
	waitEnrollmentBlocked(t, s, pid)
	waitEnrollmentExpired(t, locker, ch.ID)
	if e = locker.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("registration did not finish after invitation unlock")
	}
	if !errors.Is(e, ErrCode) {
		t.Fatal("invitation lock bypassed final expiry", e)
	}
	assertEnrollmentNotConsumed(t, s, ch.ID)
	var n int
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM merchants)+(SELECT count(*) FROM console_users)+(SELECT count(*) FROM enrollment_emails)+(SELECT count(*) FROM enrollment_invitation_uses)+(SELECT count(*) FROM backoffice_audit WHERE resource='enrollment')").Scan(&n); e != nil || n != 0 {
		t.Fatal("expired registration left partial records", n, e)
	}
	var attempts int
	if e = s.pool.QueryRow(ctx, "SELECT attempts FROM enrollment_limits WHERE key_hash=$1", s.digest("verify-source", "192.0.2.111")).Scan(&attempts); e != nil || attempts != 1 {
		t.Fatal("expired attempt lost persistent source limit", attempts, e)
	}
}

func TestEnrollmentExpiryDuringRecoveryAccountLockKeepsCredentialsAndSession(t *testing.T) {
	s, f := enrollmentFixture(t)
	ctx := context.Background()
	u := registerVerified(t, s, f, "account-expiry@example.test", "account-expiry", "192.0.2.112")
	sessions := identity.NewPersistentSessions(s.pool, false)
	token, _, e := sessions.Create(identity.User{Username: u.Username, Role: "merchant", ID: u.MerchantID, AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	ageSendingCooldown(t, s)
	ch, e := s.Issue(ctx, VerificationRequest{Purpose: "recover", Email: "account-expiry@example.test", Source: "192.0.2.112"})
	if e != nil {
		t.Fatal(e)
	}
	code := f.last(t).code
	shortenEnrollmentExpiry(t, s, ch.ID)
	locker, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer locker.Rollback(ctx)
	var pid int
	if e = locker.QueryRow(ctx, "SELECT pg_backend_pid() FROM console_users WHERE username=$1 AND role='merchant' FOR UPDATE", u.Username).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	result := make(chan error, 1)
	go func() {
		result <- s.Recover(ctx, RecoverInput{ChallengeID: ch.ID, Code: code, Email: "account-expiry@example.test", Source: "192.0.2.112", Password: "replacement-password"})
	}()
	waitEnrollmentBlocked(t, s, pid)
	waitEnrollmentExpired(t, locker, ch.ID)
	if e = locker.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("recovery did not finish after user unlock")
	}
	if !errors.Is(e, ErrCode) {
		t.Fatal("account lock bypassed final expiry", e)
	}
	assertEnrollmentNotConsumed(t, s, ch.ID)
	var hash string
	var version int64
	if e = s.pool.QueryRow(ctx, "SELECT password_hash,auth_version FROM console_users WHERE username=$1 AND role='merchant'", u.Username).Scan(&hash, &version); e != nil || version != 1 || !identity.CheckPassword(hash, "correct-test-password") {
		t.Fatal("expired recovery changed credentials", version, e)
	}
	if _, ok := sessions.Get(token, "merchant"); !ok {
		t.Fatal("expired recovery revoked old session")
	}
	var audits int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE resource='enrollment' AND operation='password-recovery'").Scan(&audits); e != nil || audits != 0 {
		t.Fatal("expired recovery committed success audit", audits, e)
	}
	var attempts int
	if e = s.pool.QueryRow(ctx, "SELECT attempts FROM enrollment_limits WHERE key_hash=$1", s.digest("verify-source", "192.0.2.112")).Scan(&attempts); e != nil || attempts != 2 {
		t.Fatal("expired recovery lost persistent source limit", attempts, e)
	}
}
