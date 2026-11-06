package main

import (
	"bytes"
	"context"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestSeedAdminStartsWithMerchantKeyLoginClosed(t *testing.T) {
	p := migrationSeedPool234(t)
	ctx := context.Background()
	store, e := payment.NewWithKey(p, bytes.Repeat([]byte{4}, 32))
	if e != nil {
		t.Fatal(e)
	}
	if before, e := store.PublicMerchantAccess(ctx); e != nil || before.CloseKeyLogin != 0 {
		t.Fatal("unconfigured installations keep the original open default", before, e)
	}
	if created, e := seedMigrationAdmin234(ctx, p, "synthetic-strong-password-seed"); e != nil || !created {
		t.Fatal("fresh initialization failed", e)
	}
	admin := identity.User{ID: 1, Username: "admin"}
	if _, e = store.InitializePlatformMerchantAccess(ctx, identity.User{ID: 2, Username: "nobody"}, "1", "1"); e == nil {
		t.Fatal("defaults attributed to a non-administrator")
	}
	initialized, e := store.InitializePlatformMerchantAccess(ctx, admin, "1", "1")
	if e != nil || !initialized {
		t.Fatal("new installation defaults not recorded", e)
	}
	after, e := store.PublicMerchantAccess(ctx)
	if e != nil || after.CloseKeyLogin != 1 || after.UserRefund != 1 {
		t.Fatal("merchant key login is not closed by default", after, e)
	}
	again, e := store.InitializePlatformMerchantAccess(ctx, admin, "0", "0")
	if e != nil || again {
		t.Fatal("an existing configuration was replaced", e)
	}
	if kept, e := store.PublicMerchantAccess(ctx); e != nil || kept != after {
		t.Fatal("existing switches changed", kept, e)
	}
	var audits int
	if e = p.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE operation='platform-access-update' AND actor_name='admin'`).Scan(&audits); e != nil || audits != 1 {
		t.Fatal("initialization is not audited exactly once", audits, e)
	}
}
