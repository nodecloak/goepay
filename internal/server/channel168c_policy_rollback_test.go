package server

import (
	"bytes"
	"context"
	"testing"
)

func TestChannel168CAuthenticatedPriorLimitsCannotRollbackAndOldSentQuoteRecovers(t *testing.T) {
	f := channel168CConfirmedLocal(t)
	ctx := context.Background()
	channel168CSetLimits(t, f, map[string]string{"paymax": "0"})
	if _, e := f.p.Exec(ctx, `CREATE TABLE synthetic_saved_limits168c AS TABLE runtime_channel_limits`); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "rollback-original-c", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("original positive send", out)
	}
	o, e := f.s.Store.Order(ctx, 1, "rollback-original-c")
	if e != nil {
		t.Fatal(e)
	}
	var quoteBefore, quoteAfter []byte
	if e = f.p.QueryRow(ctx, `SELECT to_jsonb(q)::text FROM provider_route_fee_quotes q WHERE merchant_id=1 AND out_trade_no='rollback-original-c'`).Scan(&quoteBefore); e != nil {
		t.Fatal(e)
	}
	channel168CSetLimits(t, f, map[string]string{"paymax": "0.01"})
	if _, e = f.p.Exec(ctx, `UPDATE runtime_channel_limits p SET pay_type=s.pay_type,identity_hash=s.identity_hash,daytop=s.daytop,daymaxorder=s.daymaxorder,paymin=s.paymin,paymax=s.paymax,timestart=s.timestart,timestop=s.timestop,rate_limit_seconds=s.rate_limit_seconds,rate_limit_count=s.rate_limit_count,revision=s.revision,proof_hash=s.proof_hash FROM synthetic_saved_limits168c s WHERE p.channel_id=s.channel_id`); e != nil {
		t.Fatal(e)
	}
	if w := f.admin("GET", "/7/limits", "", f.csrf); w.Code != 409 {
		t.Errorf("authentic old parent limits presented current: GET=%d", w.Code)
	}
	if out := f.pay(t, "rollback-fresh-c", "1.00", 7); out["code"] == float64(1) {
		t.Errorf("authentic old policy reopened fresh payment: code=%v", out["code"])
	}
	var fresh, audits int
	if e = f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_route_assignments WHERE merchant_id=1 AND out_trade_no='rollback-fresh-c')+(SELECT count(*) FROM orders WHERE merchant_id=1 AND out_trade_no='rollback-fresh-c'),(SELECT count(*) FROM backoffice_audit WHERE resource='runtime-channel-limits' AND resource_id=7 AND operation='runtime-channel-limits-update')`).Scan(&fresh, &audits); e != nil {
		t.Fatal(e)
	}
	p := f.s.ProviderFlow.Providers[7].(*channel168CConfirmedProvider)
	if fresh != 0 || p.sends.Load() != 1 || audits != 2 {
		t.Errorf("rolled-back authority allocated/sent new funds: fresh=%d sends=%d retainedaudits=%d", fresh, p.sends.Load(), audits)
	}
	// Already dispatched intent continues with its exact original immutable quote.
	if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e != nil {
		t.Fatal("original trusted success recovery changed by current policy corruption", e)
	}
	if out := f.pay(t, "rollback-original-c", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("old exact replay was rerouted or refused", out)
	}
	if e = f.p.QueryRow(ctx, `SELECT to_jsonb(q)::text FROM provider_route_fee_quotes q WHERE merchant_id=1 AND out_trade_no='rollback-original-c'`).Scan(&quoteAfter); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(quoteBefore, quoteAfter) {
		t.Fatal("original quote/proof was rewritten")
	}
	balance, e := f.s.Store.Balance(ctx, 1)
	if e != nil || balance.Available != 100 || p.sends.Load() != 1 {
		t.Errorf("original success ledger or single-send changed: balance=%v sends=%d error=%v", balance, p.sends.Load(), e)
	}
}

func TestChannel168CSameValueLimitsAndUnrelatedFeePolicyRevisionStayUsable(t *testing.T) {
	f := channel168Local(t)
	first := channel168CSetLimits(t, f, map[string]string{"paymax": "0"})
	second := channel168CSetLimits(t, f, map[string]string{"paymax": "0"})
	if first["revision"] != "1" || second["revision"] != "2" {
		t.Fatal("same-value limits saves did not advance origin once each")
	}
	f.configure(t, 7, "98")
	after := channel168Data(t, f.admin("GET", "/7/limits", "", f.csrf))
	if after["revision"] != "2" {
		t.Fatal("fee config changed independent limits revision")
	}
	if out := f.pay(t, "same-value-valid-c", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("valid current limits rejected after unrelated parent change", out)
	}
}
