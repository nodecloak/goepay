package server

import (
	"context"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"testing"
)

func TestRisk177CheckReadRecoversOriginalOnlyAndDetectsTampering(t *testing.T) {
	f, _, _ := risk177Flow(t)
	v := risk177Data(t, f.request("GET", "", f.csrf))
	v = risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) {
		s["auto_check_payip"] = "1"
		s["check_payip_second"] = "3600"
		s["check_payip_count"] = "1"
	}), f.csrf))
	if p := f.pay(t, "risk177-read-original", "192.0.2.177"); p["code"] != float64(1) {
		t.Fatal(p)
	}
	f.check(t, v, "risk177-read-original-run")
	read := risk177Data(t, f.riskPath("GET", "/check-runs/risk177-read-original-run", ""))
	if read["external_no"] != "risk177-read-original-run" || read["policy_revision"] != v["revision"] || read["actions"] != float64(1) {
		t.Fatal("read did not recover exact original run")
	}
	if w := f.riskPath("GET", "/check-runs/risk177-no-such-run", ""); w.Code != 404 {
		t.Fatal("missing run became new run", w.Code)
	}
	if w := f.riskPath("POST", "/check-runs/risk177-read-original-run", `{}`); w.Code != 405 {
		t.Fatal("read recovery mutated", w.Code)
	}
	if _, e := f.s.Pool.Exec(context.Background(), `ALTER TABLE core_risk_check_runs DISABLE TRIGGER USER;UPDATE core_risk_check_runs SET facts=jsonb_set(facts,'{Actions}','9')WHERE external_no='risk177-read-original-run';ALTER TABLE core_risk_check_runs ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if w := f.riskPath("GET", "/check-runs/risk177-read-original-run", ""); w.Code != 409 {
		t.Fatal("corrupt original result was displayed", w.Code)
	}
	var count int
	if e := f.s.Pool.QueryRow(context.Background(), `SELECT count(*)FROM core_risk_check_runs`).Scan(&count); e != nil || count != 1 {
		t.Fatal("recovery produced another run", count, e)
	}
}

func TestRisk177CheckCountsOriginalRoutedOrdersBeforeProviderAttempt(t *testing.T) {
	f, _, creates := risk177Flow(t)
	ctx := context.Background()
	v := risk177Data(t, f.request("GET", "", f.csrf))
	v = risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) {
		s["auto_check_channel"] = "1"
		s["check_channel_second"] = "3600"
		s["check_channel_failcount"] = "1"
	}), f.csrf))
	p := compat.Params{"name": "synthetic routed only", "type": "alipay", "out_trade_no": "risk177-route-before-attempt", "money": "1.00", "channel_id": "7", "method": "native", "clientip": "192.0.2.177"}
	channel, method, e := f.s.providerRoutedRequest(ctx, 1, p)
	if e != nil {
		t.Fatal(e)
	}
	cost, e := f.s.ProviderFlow.PaymentCostQuote(channel, method)
	if e != nil {
		t.Fatal(e)
	}
	o, e := f.s.Store.CreateOrder(ctx, payment.CreateOrderInput{Version: "v1", PaymentCost: cost, MerchantID: 1, OutTradeNo: p["out_trade_no"], Name: "synthetic routed only", Type: "alipay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Store.SetOrderClientIP(ctx, o.ID, p["clientip"]); e != nil {
		t.Fatal(e)
	}
	f.check(t, v, "risk177-route-before-attempt-check")
	var actions int
	if e = f.s.Pool.QueryRow(ctx, `SELECT count(*) FROM core_risk_actions WHERE kind='channel'`).Scan(&actions); e != nil || actions != 1 {
		t.Fatal("original route without attempt omitted", actions, e)
	}
	if newer := f.pay(t, "risk177-route-now-blocked", "192.0.2.178"); newer["code"] == float64(1) || creates.Load() != 0 {
		t.Fatal("route-only channel action did not restrict first send", creates.Load())
	}
}
