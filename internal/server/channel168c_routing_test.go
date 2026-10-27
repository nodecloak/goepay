package server

import (
	"context"
	"fmt"
	"testing"
)

func TestChannel168CAllRoutingStrategiesShareOneParentStartBudget(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	f.s.MerchantChannels[1] = []int64{7}
	channel168CSetLimits(t, f, map[string]string{"rate_limit_seconds": "60", "rate_limit_count": "1"})
	if out := f.pay(t, "common-parent-start", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	var roll1, roll2 int64
	for i, dest := range []*int64{&roll1, &roll2} {
		if e := f.p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('pay-rolls',jsonb_build_object('name',$1::text,'merchant_id','0','type','alipay','strategy','first','channel_ids','7:1','enabled',true,'default_route',false)) RETURNING id`, fmt.Sprint("synthetic-global-", i)).Scan(dest); e != nil {
			t.Fatal(e)
		}
	}
	for i, choice := range []string{"7", "-1", "-5", "-4", fmt.Sprint(roll1), fmt.Sprint(roll2), "-3"} {
		kind := "channel"
		if i >= 4 {
			kind = "roll"
		}
		if _, e := f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||jsonb_build_object('info',jsonb_build_object('alipay',jsonb_build_object('channel',$1::text,'type',$2::text))) WHERE resource='groups' AND id=0`, choice, kind); e != nil {
			t.Fatal(e)
		}
		if out := f.pay(t, fmt.Sprintf("same-parent-strategy-%d", i), "1.00", 7); out["code"] == float64(1) {
			t.Fatal("strategy reset parent starts", choice, kind)
		}
		// Prove this exact strategy is otherwise executable, rather than
		// mistaking a malformed/unreachable route for quota enforcement.
		channel168CSetLimits(t, f, map[string]string{"rate_limit_seconds": "60", "rate_limit_count": "100"})
		if out := f.pay(t, fmt.Sprintf("positive-parent-strategy-%d", i), "1.00", 7); out["code"] != float64(1) {
			t.Fatal("strategy positive control did not reach the actual parent", choice, kind, out)
		}
		channel168CSetLimits(t, f, map[string]string{"rate_limit_seconds": "60", "rate_limit_count": "1"})
	}
	var count int
	if e := f.p.QueryRow(ctx, `SELECT count(*) FROM provider_route_assignments`).Scan(&count); e != nil || count != 8 {
		t.Fatal("denied strategy committed an assignment", count, e)
	}
}

func TestChannel168CCommittedQuoteSurvivesLaterPolicyAndGroupChanges(t *testing.T) {
	f := channel168CConfirmedLocal(t)
	ctx := context.Background()
	channel168CSetLimits(t, f, map[string]string{"paymax": "100.00", "rate_limit_seconds": "60", "rate_limit_count": "1"})
	f.configure(t, 7, "99.50")
	if out := f.pay(t, "frozen-limits-c", "10.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	before, e := f.s.Store.Order(ctx, 1, "frozen-limits-c")
	if e != nil {
		t.Fatal(e)
	}
	var proof []byte
	if e = f.p.QueryRow(ctx, `SELECT proof_hash FROM provider_route_fee_quotes WHERE out_trade_no='frozen-limits-c'`).Scan(&proof); e != nil {
		t.Fatal(e)
	}
	channel168CSetLimits(t, f, map[string]string{"paymax": "1.00", "rate_limit_seconds": "60", "rate_limit_count": "1"})
	f.configure(t, 7, "90.00")
	if _, e = f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"info":{"alipay":{"channel":"8","type":"channel","rate":"95.00"}}}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "frozen-limits-c", "10.00", 7); out["code"] != float64(1) {
		t.Fatal("original paid intent was re-admitted", out)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, before.TradeNo); e != nil {
		t.Fatal("original sent identity could not confirm", e)
	}
	after, e := f.s.Store.Order(ctx, 1, "frozen-limits-c")
	if e != nil || after.ID != before.ID || after.MerchantAmount != before.MerchantAmount || after.PlatformFee != before.PlatformFee {
		t.Fatal("later policy repriced original order", after, e)
	}
	var starts int
	var saved []byte
	if e = f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_route_assignments),proof_hash FROM provider_route_fee_quotes WHERE out_trade_no='frozen-limits-c'`).Scan(&starts, &saved); e != nil || starts != 1 || string(proof) != string(saved) {
		t.Fatal("replay changed authenticated admission", starts, e)
	}
}
