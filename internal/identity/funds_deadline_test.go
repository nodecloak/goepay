package identity

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestFundsCredentialRequiresExistingTOTPAndFailsWithoutKey(t *testing.T) {
	p, f, a := fundsFixture(t)
	ctx := context.Background()
	m, e := NewMFA(p, bytes.Repeat([]byte{17}, 32))
	if e != nil {
		t.Fatal(e)
	}
	raw := []byte("temporary-mfa-secret")
	encrypted, e := m.seal(raw, a.User.Username, "admin")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, "INSERT INTO console_mfa(username,role,secret,last_counter) VALUES($1,'admin',$2,-1)", a.User.Username, encrypted); e != nil {
		t.Fatal(e)
	}
	if e = f.SetPassword(ctx, a, fundsLogin, "", fundsPassword, ""); !errors.Is(e, ErrFundsAuthentication) {
		t.Fatal("missing key bypassed MFA", e)
	}
	f = NewFundsAuthorization(p, m)
	var now time.Time
	if e = p.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		t.Fatal(e)
	}
	code := totpCode(raw, now.Unix()/30)
	if e = f.SetPassword(ctx, a, fundsLogin, "", fundsPassword, code); e != nil {
		t.Fatal(e)
	}
	if e = f.SetPassword(ctx, a, fundsLogin, fundsPassword, "changed-funds-password", code); !errors.Is(e, ErrFundsAuthentication) {
		t.Fatal("TOTP replay accepted", e)
	}
}

func TestFundsPasswordSessionExpiresWhileWaitingForCredentialLock(t *testing.T) {
	p, f, a := fundsFixture(t)
	ctx := context.Background()
	if e := f.SetPassword(ctx, a, fundsLogin, "", fundsPassword, ""); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Exec(ctx, "UPDATE console_sessions SET expires_at=clock_timestamp()+interval '700 milliseconds'"); e != nil {
		t.Fatal(e)
	}
	tx, e := p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT username FROM admin_funds_credentials WHERE username='funds' FOR UPDATE"); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- f.SetPassword(ctx, a, fundsLogin, fundsPassword, "changed-funds-password", "") }()
	time.Sleep(900 * time.Millisecond)
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-done; !errors.Is(e, ErrFundsAuthentication) {
		t.Fatal("expired session changed credential", e)
	}
}
