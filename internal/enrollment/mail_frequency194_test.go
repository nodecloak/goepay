package enrollment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goepay/internal/identity"
	"goepay/internal/mailpolicy"
)

func TestMailFrequency194LowerThenRaiseCannotReduceConsumedDailyAttempts(t *testing.T) {
	s, f := enrollmentFixture(t)
	p, a := mail194Admin(t, s)
	mail194Save(t, p, a, mailpolicy.Values{EmailInterval: "1", IPInterval: "1", EmailDaily: "3", IPDaily: "100"})
	for i := 0; i < 3; i++ {
		ageSendingCooldown(t, s)
		if _, e := s.Issue(t.Context(), VerificationRequest{Purpose: "recover", Email: "monotonic194@example.test", Source: fmt.Sprintf("192.0.2.%d", 190+i)}); e != nil {
			t.Fatal(e)
		}
	}
	mail194Save(t, p, a, mailpolicy.Values{EmailInterval: "1", IPInterval: "1", EmailDaily: "1", IPDaily: "100"})
	ageSendingCooldown(t, s)
	if _, e := s.Issue(t.Context(), VerificationRequest{Purpose: "recover", Email: "monotonic194@example.test", Source: "192.0.2.193"}); !errors.Is(e, ErrLimited) {
		t.Fatal("lower daily limit not applied", e)
	}
	mail194Save(t, p, a, mailpolicy.Values{EmailInterval: "1", IPInterval: "1", EmailDaily: "3", IPDaily: "100"})
	if _, e := s.Issue(t.Context(), VerificationRequest{Purpose: "recover", Email: "monotonic194@example.test", Source: "192.0.2.194"}); !errors.Is(e, ErrLimited) {
		t.Fatal("lowering then raising policy reduced consumed attempts", e)
	}
	if len(f.messages) != 3 {
		t.Fatal("policy rotation recovered consumed budget", len(f.messages))
	}
}

func mail194Admin(t *testing.T, s *Service) (*mailpolicy.Store, mailpolicy.Actor) {
	t.Helper()
	if _, e := s.pool.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class)VALUES('mail194-admin','admin',194,'Synthetic mail admin','unused-hash','super')`); e != nil {
		t.Fatal(e)
	}
	sessions := identity.NewPersistentSessions(s.pool, false)
	user := identity.User{ID: 194, Username: "mail194-admin", Role: "admin", AdminClass: "super", AuthVersion: 1}
	token, _, e := sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	return mailpolicy.FromMaster(s.pool, make([]byte, 32)), mailpolicy.Actor{User: user, Token: token}
}
func mail194Save(t *testing.T, p *mailpolicy.Store, a mailpolicy.Actor, values mailpolicy.Values) {
	t.Helper()
	state, e := p.Read(t.Context(), a)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Save(t.Context(), a, values, state.Fingerprint); e != nil {
		t.Fatal(e)
	}
}
func TestMailFrequency194ManagedIPIntervalActuallyBlocksDifferentMailboxAndPurpose(t *testing.T) {
	s, f := enrollmentFixture(t)
	p, a := mail194Admin(t, s)
	mail194Save(t, p, a, mailpolicy.Values{EmailInterval: "120", IPInterval: "120", EmailDaily: "10", IPDaily: "20"})
	if _, e := s.Issue(t.Context(), VerificationRequest{Purpose: "recover", Email: "first194@example.test", Source: "192.0.2.194"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Issue(t.Context(), VerificationRequest{Purpose: "register", Email: "second194@example.test", Source: "192.0.2.194"}); !errors.Is(e, ErrLimited) {
		t.Fatal("different mailbox/purpose bypassed original shared IP interval", e)
	}
	if len(f.messages) != 1 {
		t.Fatal("blocked request reached sender", len(f.messages))
	}
}
func TestMailFrequency194SaveDoesNotResetExistingDailyBudgetOrCooldown(t *testing.T) {
	s, f := enrollmentFixture(t)
	p, a := mail194Admin(t, s)
	if _, e := s.Issue(t.Context(), VerificationRequest{Purpose: "recover", Email: "budget194@example.test", Source: "192.0.2.194"}); e != nil {
		t.Fatal(e)
	}
	mail194Save(t, p, a, mailpolicy.Values{EmailInterval: "1", IPInterval: "1", EmailDaily: "1", IPDaily: "20"})
	ageSendingCooldown(t, s)
	if _, e := s.Issue(t.Context(), VerificationRequest{Purpose: "register", Email: "budget194@example.test", Source: "192.0.2.195"}); !errors.Is(e, ErrLimited) {
		t.Fatal("policy takeover reset existing shared daily budget", e)
	}
	mail194Save(t, p, a, mailpolicy.Values{EmailInterval: "1", IPInterval: "1", EmailDaily: "2", IPDaily: "20"})
	// The rejected attempt has consumed the retained mailbox budget. Saving a
	// second revision must not produce a fresh counter namespace.
	if _, e := s.Issue(t.Context(), VerificationRequest{Purpose: "recover", Email: "budget194@example.test", Source: "192.0.2.196"}); !errors.Is(e, ErrLimited) {
		t.Fatal("revision reset rejected-attempt budget", e)
	}
	if len(f.messages) != 1 {
		t.Fatal("configuration change re-sent mail", len(f.messages))
	}
}
func TestMailFrequency194ConcurrentIPIntervalAndFailedDeliveryRemainBounded(t *testing.T) {
	s, _ := enrollmentFixture(t)
	p, a := mail194Admin(t, s)
	mail194Save(t, p, a, mailpolicy.Values{EmailInterval: "1", IPInterval: "60", EmailDaily: "10", IPDaily: "20"})
	var sends atomic.Int32
	s.sender = SenderFunc(func(context.Context, string, string, string) error {
		sends.Add(1)
		return errors.New("synthetic accepted reply lost")
	})
	var wg sync.WaitGroup
	var unavailable, limited atomic.Int32
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := s.Issue(t.Context(), VerificationRequest{Purpose: "recover", Email: []string{"a194@example.test", "b194@example.test", "c194@example.test", "d194@example.test", "e194@example.test", "f194@example.test"}[i], Source: "192.0.2.194"})
			if errors.Is(e, ErrUnavailable) {
				unavailable.Add(1)
			} else if errors.Is(e, ErrLimited) {
				limited.Add(1)
			} else {
				t.Error("unexpected concurrent send result", e)
			}
		}(i)
	}
	wg.Wait()
	if sends.Load() != 1 || unavailable.Load() != 1 || limited.Load() != 5 {
		t.Fatal("IP interval did not serialize actual failed delivery", sends.Load(), unavailable.Load(), limited.Load())
	}
}
func TestMailFrequency194MissingManagedSourceCannotFallbackToLegacySend(t *testing.T) {
	s, f := enrollmentFixture(t)
	p, a := mail194Admin(t, s)
	mail194Save(t, p, a, mailpolicy.Values{EmailInterval: "60", IPInterval: "60", EmailDaily: "10", IPDaily: "20"})
	if _, e := s.pool.Exec(t.Context(), `ALTER TABLE mail_rate_head194 DISABLE TRIGGER USER;DELETE FROM mail_rate_head194;ALTER TABLE mail_rate_head194 ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, e := s.Issue(ctx, VerificationRequest{Purpose: "recover", Email: "source194@example.test", Source: "192.0.2.194"}); !errors.Is(e, ErrUnavailable) {
		t.Fatal("missing managed policy fell back to legacy sending", e)
	}
	var challenges int
	if e := s.pool.QueryRow(t.Context(), `SELECT count(*)FROM enrollment_challenges`).Scan(&challenges); e != nil || challenges != 0 || len(f.messages) != 0 {
		t.Fatal("invalid policy spent challenge or sent", challenges, len(f.messages), e)
	}
}
