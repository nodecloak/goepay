package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync/atomic"
	"testing"

	"goepay/internal/enrollment"
	"goepay/internal/mailaccount"
)

func TestMailAccounts195ActualEnrollmentUnmanagedFallbackThenDisabledOrCorruptAccountDenies(t *testing.T) {
	f := channel168Local(t)
	master := make([]byte, 32)
	key := sha256.Sum256(append([]byte("checkout-signing:"), master...))
	f.s.CheckoutKey = key[:]
	var fallback atomic.Int32
	resolver := &mailaccount.Verification{Store: mailaccount.FromMaster(f.p, master), Fallback: enrollment.SenderFunc(func(context.Context, string, string, string) error { fallback.Add(1); return nil })}
	service, e := enrollment.New(f.p, f.s.Store, master, resolver)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = service.Issue(t.Context(), enrollment.VerificationRequest{Purpose: "recover", Email: "unmanaged195@example.test", Source: "192.0.2.191"}); e != nil || fallback.Load() != 1 {
		t.Fatal("genuine unmanaged installation lost explicit deployment sender", e, fallback.Load())
	}
	state := mail195Data(t, mail195Request(f, "GET", "/smtp", ""))
	state = mail195Save(t, f, state, "smtp.example.invalid", "sender195@example.invalid", "synthetic195-shared-source", 0, false)
	if _, e = service.Issue(t.Context(), enrollment.VerificationRequest{Purpose: "recover", Email: "disabled195@example.test", Source: "192.0.2.192"}); !errors.Is(e, enrollment.ErrUnavailable) {
		t.Fatal("disabled managed account fell back to deployment", e)
	}
	var challenges int
	if e = f.p.QueryRow(t.Context(), `SELECT count(*)FROM enrollment_challenges`).Scan(&challenges); e != nil || challenges != 1 || fallback.Load() != 1 {
		t.Fatal("disabled account created durable send attempt", challenges, fallback.Load(), e)
	}
	if _, e = f.p.Exec(t.Context(), `ALTER TABLE mail_account_head195 DISABLE TRIGGER USER;DELETE FROM mail_account_head195;ALTER TABLE mail_account_head195 ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = service.Issue(t.Context(), enrollment.VerificationRequest{Purpose: "recover", Email: "missing195@example.test", Source: "192.0.2.193"}); !errors.Is(e, enrollment.ErrUnavailable) || fallback.Load() != 1 {
		t.Fatal("missing managed source fell back to deployment", e, fallback.Load())
	}
}
