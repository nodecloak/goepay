package server

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"goepay/internal/identity"
)

func TestConsoleAdminAuthorityProofIsBoundToRequestAndTransaction(t *testing.T) {
	f := channel168Local(t)
	ctx := t.Context()
	session, ok := f.s.Sessions.Get(f.token, "admin")
	if !ok {
		t.Fatal("original session unavailable")
	}
	r := httptest.NewRequest("PATCH", "/console/v1/admin/resources/admin-users/1", nil)
	r = f.s.withConsoleAdminAuthority(r, session, f.token, 1, []string{"admin", "resources", "admin-users", "1"})
	tx, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err = requireConsoleAdminAuthorityTx(r.Context(), tx); err != nil {
		t.Fatal(err)
	}
	// An independent revocation cannot commit through the authority locks.
	probe, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	_, err = f.p.Exec(probe, "DELETE FROM console_sessions WHERE username='admin168'")
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("original session was not locked through the transaction: %v", err)
	}
	// A legitimate self-edit may invalidate its old epoch in this transaction.
	if _, err = tx.Exec(ctx, "UPDATE console_users SET auth_version=auth_version+1 WHERE username='admin168'"); err != nil {
		t.Fatal(err)
	}
	if err = requireConsoleAdminAuthorityTx(r.Context(), tx); err != nil {
		t.Fatal("same transaction lost its already-locked authority", err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = requireConsoleAdminAuthorityTx(r.Context(), tx); err == nil {
		t.Fatal("closed transaction reused cached authority")
	}
	second, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Rollback(context.Background())
	if err = requireConsoleAdminAuthorityTx(r.Context(), second); !errors.Is(err, identity.ErrFundsAuthentication) {
		t.Fatal("new transaction reused another transaction's authority", err)
	}
	// Another request with a stale token cannot inherit the first request's proof,
	// even when the caller supplies the current database epoch as its snapshot.
	r = f.s.withConsoleAdminAuthority(httptest.NewRequest("PATCH", "/console/v1/admin/resources/admin-users/1", nil), session, f.token, 2, []string{"admin", "resources", "admin-users", "1"})
	if err = requireConsoleAdminAuthorityTx(r.Context(), second); !errors.Is(err, identity.ErrFundsAuthentication) {
		t.Fatal("new request inherited old authority", err)
	}
	if err = second.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = requireConsoleAdminAuthorityTx(r.Context(), second); err == nil {
		t.Fatal("rolled back transaction retained authority")
	}
}
