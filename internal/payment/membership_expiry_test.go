package payment

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func TestMembershipExpiryRestoresAdministratorBaselineAndPreservesSnapshots(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 601, 1000, "platform")
	var base int64
	if e := s.pool.QueryRow(ctx, "SELECT (profile->>'group_id')::bigint FROM merchants WHERE id=601").Scan(&base); e != nil {
		t.Fatal(e)
	}
	var vip int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"vip","enabled":true,"rate_bps":"100","price_cents":"0","duration_days":"30"}') RETURNING id`).Scan(&vip); e != nil {
		t.Fatal(e)
	}
	if _, e := s.PurchaseMembership(ctx, 601, vip, "purchase", "merchant:601"); e != nil {
		t.Fatal(e)
	}
	first, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 601, OutTradeNo: "vip", Amount: 10000})
	if e != nil || first.FeeRateBPS != 100 {
		t.Fatalf("active membership %+v %v", first, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE memberships SET expires_at=now()-interval '1 second' WHERE merchant_id=601"); e != nil {
		t.Fatal(e)
	}
	expired, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 601, OutTradeNo: "expired", Amount: 10000})
	if e != nil || expired.FeeRateBPS != 1000 || expired.FeeGroupID != base {
		t.Fatalf("expired membership retained discount %+v %v", expired, e)
	}
	old, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 601, OutTradeNo: "vip", Amount: 10000})
	if e != nil || old.FeeRateBPS != 100 {
		t.Fatalf("historical snapshot rewritten %+v %v", old, e)
	}
	var manual int64
	if e = s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"manual","enabled":true,"rate_bps":"500"}') RETURNING id`).Scan(&manual); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PurchaseMembership(ctx, 601, vip, "renew", "merchant:601"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{group_id}',to_jsonb($2::text)) WHERE id=$1`, int64(601), fmt.Sprint(manual)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE memberships SET expires_at=now()-interval '1 second' WHERE merchant_id=601"); e != nil {
		t.Fatal(e)
	}
	assigned, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 601, OutTradeNo: "manual", Amount: 10000})
	if e != nil || assigned.FeeRateBPS != 500 || assigned.FeeGroupID != manual {
		t.Fatalf("manual baseline lost %+v %v", assigned, e)
	}
}

func TestMembershipBaselineUpgradeRecoversAuditAndRejectsAmbiguity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 602, 1200, "platform")
	var baseline int64
	if e := s.pool.QueryRow(ctx, "SELECT base_group_id FROM merchants WHERE id=602").Scan(&baseline); e != nil {
		t.Fatal(e)
	}
	if e := s.CreateMerchant(ctx, Merchant{ID: 603, Name: "legacy", APIKey: "test-only-legacy-secret"}); e != nil {
		t.Fatal(e)
	}
	var vip int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"legacy-vip","enabled":true,"rate_bps":"100","price_cents":"0","duration_days":"30"}') RETURNING id`).Scan(&vip); e != nil {
		t.Fatal(e)
	}
	for _, mid := range []int64{602, 603} {
		if _, e := s.PurchaseMembership(ctx, mid, vip, "legacy", "merchant"); e != nil {
			t.Fatal(e)
		}
		if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{group_id}',to_jsonb($2::text)) WHERE id=$1`, mid, fmt.Sprint(vip)); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',1,'admin','merchants',602,'update',jsonb_build_object('group_id',$1::text))`, fmt.Sprint(baseline)); e != nil {
		t.Fatal(e)
	}
	// Reconstruct the schema immediately before the additive upgrade.
	if _, e := s.pool.Exec(ctx, "DROP TRIGGER merchant_group_baseline_sync ON merchants; ALTER TABLE merchants DROP COLUMN base_group_id,DROP COLUMN group_baseline_unresolved"); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("../../db/migrations/000018_membership_baseline.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	var restored int64
	var unresolved bool
	if e = s.pool.QueryRow(ctx, "SELECT base_group_id,group_baseline_unresolved FROM merchants WHERE id=602").Scan(&restored, &unresolved); e != nil || restored != baseline || unresolved {
		t.Fatalf("audit recovery %d,%v %v", restored, unresolved, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE memberships SET expires_at=now()-interval '1 second'"); e != nil {
		t.Fatal(e)
	}
	recovered, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 602, OutTradeNo: "recovered", Amount: 10000})
	if e != nil || recovered.FeeRateBPS != 1200 {
		t.Fatalf("recovered baseline %+v %v", recovered, e)
	}
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 603, OutTradeNo: "ambiguous", Amount: 10000}); e == nil {
		t.Fatal("ambiguous historical baseline silently zeroed or discounted")
	}
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{email}','"new@example.invalid"') WHERE id=603`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 603, OutTradeNo: "still-ambiguous", Amount: 10000}); e == nil {
		t.Fatal("unrelated profile edit cleared ambiguity")
	}
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{group_id}',to_jsonb($1::text)) WHERE id=603`, fmt.Sprint(baseline)); e != nil {
		t.Fatal(e)
	}
	fixed, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 603, OutTradeNo: "fixed", Amount: 10000})
	if e != nil || fixed.FeeRateBPS != 1200 {
		t.Fatalf("manual recovery %+v %v", fixed, e)
	}
}
