package contract_test

import (
	"context"
	"goepay/internal/identity"
	"strings"
	"testing"
)

func TestMerchant174DelegationCannotReadOrChangeOwnerAPIPreference(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx := context.Background()
	var av, mv int64
	if e := f.p.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='admin'").Scan(&av); e != nil {
		t.Fatal(e)
	}
	if e := f.p.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='merchant'").Scan(&mv); e != nil {
		t.Fatal(e)
	}
	issuer := identity.User{ID: 1, Role: "admin", Username: "admin", AuthVersion: av, AdminClass: "super"}
	issuerToken, _, e := f.s.Sessions.Create(issuer)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := f.p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	token, csrf, _, e := f.s.Sessions.CreateDelegated(ctx, tx, identity.User{ID: 2, Role: "merchant", Username: "merchant", AuthVersion: mv}, issuer, issuerToken)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	f.token, f.csrf = token, csrf
	for _, method := range []string{"GET", "PATCH"} {
		if w := f.request(method, "api-switches", apiSwitchBody174(strings.Repeat("a", 64), 0, 0, 0), csrf); w.Code != 403 {
			t.Fatal("delegated owner capability exposed", method, w.Code)
		}
	}
}
