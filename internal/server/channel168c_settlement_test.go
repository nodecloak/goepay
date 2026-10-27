package server

import (
	"context"
	"testing"
)

func TestChannel168CTrustedGrossAndSuccessAuditAreAtomicWithLedger(t *testing.T) {
	f := channel168CConfirmedLocal(t)
	ctx := context.Background()
	channel168CSetLimits(t, f, map[string]string{"daytop": "1.00"})
	f.group(t, pointer168("0"))
	if _, e := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	f.configure(t, 7, "90.00")
	if out := f.pay(t, "atomic-real-money-c", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(ctx, 1, "atomic-real-money-c")
	if e != nil {
		t.Fatal(e)
	}
	p := f.s.ProviderFlow.Providers[7].(*channel168CConfirmedProvider)
	p.mu.Lock()
	r := p.requests[o.TradeNo]
	r.Amount++
	p.requests[o.TradeNo] = r
	p.mu.Unlock()
	if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e == nil {
		t.Fatal("mismatched confirmed gross was accepted")
	}
	p.mu.Lock()
	r.Amount--
	p.requests[o.TradeNo] = r
	p.mu.Unlock()
	if _, e = f.p.Exec(ctx, `CREATE FUNCTION channel168c_fail_success_audit() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='runtime-channel-success' THEN RAISE EXCEPTION 'synthetic success audit failure';END IF;RETURN NEW;END$$;CREATE TRIGGER channel168c_fail_success_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION channel168c_fail_success_audit()`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e == nil {
		t.Fatal("success audit failure committed")
	}
	var paid, receipts, events, heads, journals int
	if e = f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM orders WHERE status='paid'),(SELECT count(*) FROM verified_payment_receipts),(SELECT count(*) FROM runtime_channel_success_events),(SELECT count(*) FROM runtime_channel_success_days),(SELECT count(*) FROM journals WHERE event_key LIKE 'payment:%' OR event_key LIKE 'payment-fee:%')`).Scan(&paid, &receipts, &events, &heads, &journals); e != nil || paid+receipts+events+heads+journals != 0 {
		t.Fatal("failed success transaction leaked facts or money", paid, receipts, events, heads, journals, e)
	}
	if _, e = f.p.Exec(ctx, `DROP TRIGGER channel168c_fail_success_audit ON backoffice_audit`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e != nil {
		t.Fatal("credible original success could not retry atomically", e)
	}
	state := channel168Data(t, f.admin("GET", "/7/limits", "", f.csrf))
	u := state["usage"].(map[string]any)
	if u["successful_amount"] != "1.00" || u["successful_count"] != "1" || u["day_paused"] != true {
		t.Fatal("quota used merchant net instead of actual confirmed gross", u)
	}
	b, e := f.s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 90 {
		t.Fatal("fee/merchant credit changed", b, e)
	}
}
