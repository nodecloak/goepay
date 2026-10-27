package server

import (
	"context"
	"testing"
	"time"
)

func TestChannel168CUnknownAndFailedConsumeStartsButNeverSuccessfulDailyQuota(t *testing.T) {
	f := channel168CConfirmedLocal(t)
	ctx := context.Background()
	channel168CSetLimits(t, f, map[string]string{"daymaxorder": "1", "rate_limit_seconds": "60", "rate_limit_count": "2"})
	provider := f.s.ProviderFlow.Providers[7].(*channel168CConfirmedProvider)
	provider.states = map[string]string{}
	for i, state := range []string{"USERPAYING", "CLOSED"} {
		no := "unknown-c"
		if i == 1 {
			no = "failed-c"
		}
		if out := f.pay(t, no, "100.00", 7); out["code"] != float64(1) {
			t.Fatal("unsuccessful prior attempt consumed success quota", out)
		}
		o, e := f.s.Store.Order(ctx, 1, no)
		if e != nil {
			t.Fatal(e)
		}
		provider.mu.Lock()
		provider.states[o.TradeNo] = state
		provider.mu.Unlock()
		if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e != nil {
			t.Fatal(e)
		}
	}
	state := channel168Data(t, f.admin("GET", "/7/limits", "", f.csrf))
	usage := state["usage"].(map[string]any)
	if usage["successful_amount"] != "0.00" || usage["successful_count"] != "0" || usage["recent_starts"] != "2" || usage["day_paused"] != false || usage["rate_limited"] != true {
		t.Fatal("success and start semantics were mixed", usage)
	}
	if out := f.pay(t, "third-start-c", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("unknown/failed starts did not consume parent frequency")
	}
}

func TestChannel168CSuccessQuotaIsNotReleasedByRefund(t *testing.T) {
	f := channel168CConfirmedLocal(t)
	ctx := context.Background()
	channel168CSetLimits(t, f, map[string]string{"daytop": "100.00"})
	if out := f.pay(t, "refund-success-c", "100.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(ctx, 1, "refund-success-c")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	refund, e := f.s.Store.ReserveRefund(ctx, 1, o.OutTradeNo, "refund-c", 10000)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Store.ResolveRefund(ctx, refund.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	state := channel168Data(t, f.admin("GET", "/7/limits", "", f.csrf))
	usage := state["usage"].(map[string]any)
	if usage["successful_amount"] != "100.00" || usage["successful_count"] != "1" || usage["day_paused"] != true {
		t.Fatal("refund released successful daily quota", usage)
	}
	if out := f.pay(t, "after-refund-c", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("refunded success reopened parent daily quota")
	}
}

func TestChannel168CUTC8DayBoundaryRestoresOnlyNewDayAdmission(t *testing.T) {
	f := channel168CConfirmedLocal(t)
	ctx := context.Background()
	channel168CSetLimits(t, f, map[string]string{"daymaxorder": "1"})
	local := time.Now().In(time.FixedZone("UTC+8", 8*3600))
	nearEnd := time.Date(local.Year(), local.Month(), local.Day(), 23, 59, 59, 500000000, local.Location())
	if _, e := f.p.Exec(ctx, `UPDATE console_sessions SET expires_at=$1 WHERE username='admin168'`, nearEnd.Add(48*time.Hour)); e != nil {
		t.Fatal(e)
	}
	f = channel168CClock(t, f, nearEnd)
	if out := f.pay(t, "old-day-c", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(ctx, 1, "old-day-c")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "still-old-day-c", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("old day did not reach success threshold")
	}
	if _, e = f.p.Exec(ctx, `UPDATE channel168c_test_clock SET instant=instant+interval '1 second'`); e != nil {
		t.Fatal(e)
	}
	state := channel168Data(t, f.admin("GET", "/7/limits", "", f.csrf))
	usage := state["usage"].(map[string]any)
	if usage["day"] != nearEnd.Add(time.Second).Format("2006-01-02") || usage["successful_count"] != "0" || usage["day_paused"] != false {
		t.Fatal("new UTC8 day inherited old successes", usage)
	}
	if out := f.pay(t, "new-day-c", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("new day admission did not recover", out)
	}
}
