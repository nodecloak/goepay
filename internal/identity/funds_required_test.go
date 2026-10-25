package identity

import (
	"context"
	"errors"
	"testing"
)

func TestFundsRequiredGrantCannotBeDisabledByOptionalPolicy(t *testing.T) {
	p, f, actor := fundsFixture(t)
	ctx := context.Background()
	action := fundsAction(t, "persistent-plan-authority")
	check := func(token string, want error, commit bool) {
		t.Helper()
		tx, err := p.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		err = f.ConsumeRequiredTx(ctx, tx, actor, token, action, "1:1")
		if !errors.Is(err, want) {
			t.Fatalf("required consume: got %v want %v", err, want)
		}
		if commit {
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	// No global enable row exists: persistent authority still needs a real grant.
	check("", ErrFundsAuthorization, false)
	if err := f.SetPassword(ctx, actor, fundsLogin, "", fundsPassword, ""); err != nil {
		t.Fatal(err)
	}
	grant := grantFor(t, f, actor, action)
	check(grant, nil, false) // rollback must keep the original grant usable.
	check(grant, nil, true)
	check(grant, ErrFundsAuthorization, false)
	var consumed int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM admin_funds_audit WHERE operation='consume'`).Scan(&consumed); err != nil || consumed != 1 {
		t.Fatalf("grant consumption/audit count=%d err=%v", consumed, err)
	}
}
