package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"goepay/internal/compat"
	"goepay/internal/payment"
)

func TestChannel168ConcurrentSameIntentCannotRepriceResendOrSwitchParent(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	f.configure(t, 7, "99.50")
	f.configure(t, 8, "90.00")
	if _, e := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	done := make(chan map[string]any, 2)
	for i := 0; i < 2; i++ {
		go func() { done <- f.pay(t, "concurrent168", "100.00", 7) }()
	}
	wins := 0
	for i := 0; i < 2; i++ {
		out := <-done
		if out["code"] == float64(1) {
			wins++
		} else if out["code"] != float64(-1) || out["msg"] != "payment outcome requires query" {
			t.Fatal("same intent concurrent result", out)
		}
	}
	if wins == 0 {
		t.Fatal("no original send owner completed")
	}
	if out := f.pay(t, "concurrent168", "100.00", 7); out["code"] != float64(1) {
		t.Fatal("settled attempt material was not replayed", out)
	}
	if out := f.pay(t, "concurrent168", "101.00", 7); out["code"] == float64(1) {
		t.Fatal("changed amount reused quote")
	}
	if out := f.pay(t, "concurrent168", "100.00", 8); out["code"] == float64(1) {
		t.Fatal("changed parent reused quote")
	}
	if f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 1 || f.s.ProviderFlow.Providers[8].(*channel168Provider).sends.Load() != 0 {
		t.Fatal("same intent resent or failed over")
	}
	var routes, quotes, orders, terms, attempts int
	if e := f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_route_assignments),(SELECT count(*) FROM provider_route_fee_quotes),(SELECT count(*) FROM orders),(SELECT count(*) FROM order_route_fee_terms),(SELECT count(*) FROM provider_attempts)`).Scan(&routes, &quotes, &orders, &terms, &attempts); e != nil || routes != 1 || quotes != 1 || orders != 1 || terms != 1 || attempts != 1 {
		t.Fatal("same intent produced extra facts", routes, quotes, orders, terms, attempts, e)
	}
}

func TestChannel168CostEstimateFrozenWithoutSecondMerchantDeduction(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	f.group(t, pointer168("0"))
	f.configure(t, 7, "99.50")
	if _, e := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	provider := f.s.ProviderFlow.Providers[7].(*channel168Provider)
	provider.cost = "0.30"
	p := compat.Params{"type": "alipay", "method": "native", "name": "cost snapshot168", "money": "100.00", "clientip": "192.0.2.1", "out_trade_no": "cost168", "channel_id": "7"}
	if _, _, e := f.s.providerRoutedRequest(ctx, 1, p); e != nil {
		t.Fatal(e)
	}
	provider.cost = "25.00"
	o, e := f.s.Store.CreateOrder(ctx, create168(p, 10000))
	if e != nil || o.MerchantAmount != 9950 || o.PlatformFee != 50 {
		t.Fatal("cost estimate deducted from merchant", o.MerchantAmount, o.PlatformFee, e)
	}
	var raw []byte
	if e = f.p.QueryRow(ctx, `SELECT facts->'Cost' FROM provider_route_fee_quotes WHERE out_trade_no='cost168'`).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	var c struct {
		RateUnits int64
		Revision  string
	}
	if e = json.Unmarshal(raw, &c); e != nil || c.RateUnits != 300000 || len(c.Revision) != 64 {
		t.Fatal("cost estimate was not frozen at route selection", c, e)
	}
	if e = f.s.Store.MarkPaid(ctx, o.TradeNo, "cost-paid168"); e != nil {
		t.Fatal(e)
	}
	b, e := f.s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 9950 {
		t.Fatal("actual paid ledger charged configured cost twice", b, e)
	}
}

func TestChannel168DisabledPolicyAndExpiredQuoteNeverSendOrReassign(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	f.configure(t, 7, "99.50")
	p := compat.Params{"type": "alipay", "method": "native", "name": "disable168", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": "disabled168", "channel_id": "7"}
	if _, _, e := f.s.providerRoutedRequest(ctx, 1, p); e != nil {
		t.Fatal(e)
	}
	o, e := f.s.Store.CreateOrder(ctx, create168(p, 100))
	if e != nil {
		t.Fatal(e)
	}
	state := channel168Data(t, f.admin("GET", "/7", "", f.csrf))
	body := fmt.Sprintf(`{"rate":"99.50","mode":0,"enabled":false,"expected_fingerprint":%q}`, state["fingerprint"])
	channel168Data(t, f.admin("PATCH", "/7", body, f.csrf))
	if _, e = f.s.ProviderFlow.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.1", ""); !errors.Is(e, payment.ErrState) {
		t.Fatal("disabled parent first send", e)
	}
	if out := f.pay(t, "new-disabled168", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("disabled parent remained eligible")
	}
	f.configure(t, 7, "99.50")
	var gid int64
	if e = f.p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"rate":"0","channel":"7","type":"channel"}}}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(1,$1,clock_timestamp()+interval '1 second')`, gid); e != nil {
		t.Fatal(e)
	}
	p["out_trade_no"] = "expires168"
	if _, _, e = f.s.providerRoutedRequest(ctx, 1, p); e != nil {
		t.Fatal(e)
	}
	time.Sleep(1200 * time.Millisecond)
	if _, e = f.s.Store.CreateOrder(ctx, create168(p, 100)); !errors.Is(e, payment.ErrState) {
		t.Fatal("expired quote switched to base group or created order", e)
	}
	if f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 0 {
		t.Fatal("disabled/expired intent reached provider")
	}
	var orders, routes int
	if e = f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM provider_route_assignments)`).Scan(&orders, &routes); e != nil || orders != 1 || routes != 2 {
		t.Fatal("rejected new intent reassigned or born", orders, routes, e)
	}
}

func TestChannel168FinalDeadlineAfterLastQuoteAndBirthWrite(t *testing.T) {
	for _, target := range []string{"provider_route_fee_quotes", "order_route_fee_terms"} {
		t.Run(target, func(t *testing.T) {
			f := channel168Local(t)
			ctx := context.Background()
			f.configure(t, 7, "99.50")
			var gid int64
			if e := f.p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"rate":"0","channel":"-4","type":"channel"}}}') RETURNING id`).Scan(&gid); e != nil {
				t.Fatal(e)
			}
			if _, e := f.p.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(1,$1,clock_timestamp()+interval '1 second')`, gid); e != nil {
				t.Fatal(e)
			}
			if _, e := f.p.Exec(ctx, `CREATE SEQUENCE channel168_last_write;CREATE FUNCTION channel168_last_write_gate() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM nextval('channel168_last_write');PERFORM pg_sleep(1.3);RETURN NEW;END$$;CREATE TRIGGER channel168_last_write_gate BEFORE INSERT ON `+target+` FOR EACH ROW EXECUTE FUNCTION channel168_last_write_gate()`); e != nil {
				t.Fatal(e)
			}
			p := compat.Params{"type": "alipay", "method": "native", "name": "last write168", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": "last-write168"}
			_, _, e := f.s.providerRoutedRequest(ctx, 1, p)
			if target == "order_route_fee_terms" {
				if e != nil {
					t.Fatal(e)
				}
				_, e = f.s.Store.CreateOrder(ctx, create168(p, 100))
			}
			if !errors.Is(e, payment.ErrState) {
				t.Fatal("last potentially waiting write committed expired membership", target, e)
			}
			var entered bool
			if e = f.p.QueryRow(ctx, "SELECT is_called FROM channel168_last_write").Scan(&entered); e != nil || !entered {
				t.Fatal("last-write gate not reached", e)
			}
			var routes, quotes, orders, terms, counters, heads, originalHeads int
			if e = f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_route_assignments),(SELECT count(*) FROM provider_route_fee_quotes),(SELECT count(*) FROM orders),(SELECT count(*) FROM order_route_fee_terms),(SELECT COALESCE(sum(assigned),0) FROM group_route_counters),(SELECT count(*) FROM provider_channel_last_used),(SELECT count(*) FROM provider_channel_last_used WHERE channel_id=7 AND merchant_id=1 AND out_trade_no='last-write168')`).Scan(&routes, &quotes, &orders, &terms, &counters, &heads, &originalHeads); e != nil {
				t.Fatal(e)
			}
			want := 0
			if target == "order_route_fee_terms" {
				want = 1
			}
			if routes != want || quotes != want || heads != want || originalHeads != want || counters != 0 || orders != 0 || terms != 0 {
				t.Fatal("expiry did not roll back last-write transaction", target, routes, quotes, orders, terms, counters, heads, originalHeads)
			}
		})
	}
}

func TestChannel168LossOfBothFeeFactsNeverDowngradesNewOrderToHistorical(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	f.configure(t, 7, "99.50")
	if out := f.pay(t, "bothfacts168", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(ctx, 1, "bothfacts168")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(ctx, `UPDATE orders SET runtime_fee_quote_required=false WHERE id=$1`, o.ID); e == nil {
		t.Fatal("birth marker was mutable")
	}
	visible, _ := json.Marshal(o)
	if strings.Contains(string(visible), "runtime_fee_quote_required") || strings.Contains(string(visible), "routeFeeRequired") {
		t.Fatal("private birth marker leaked DTO")
	}
	if _, e = f.p.Exec(ctx, `ALTER TABLE order_route_fee_terms DISABLE TRIGGER order_route_fee_terms_immutable;ALTER TABLE provider_route_fee_quotes DISABLE TRIGGER provider_route_fee_quote_immutable;DELETE FROM order_route_fee_terms;DELETE FROM provider_route_fee_quotes;ALTER TABLE provider_route_fee_quotes ENABLE TRIGGER provider_route_fee_quote_immutable;ALTER TABLE order_route_fee_terms ENABLE TRIGGER order_route_fee_terms_immutable`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: o.OutTradeNo, Name: o.Name, Type: o.Type, Amount: o.Amount}); !errors.Is(e, payment.ErrConflict) {
		t.Errorf("both fee facts lost but replay downgraded new order to history: %v", e)
	}
	if e = f.s.Store.MarkPaid(ctx, o.TradeNo, "lost-both168"); !errors.Is(e, payment.ErrConflict) {
		t.Errorf("both fee facts lost but paid downgraded new order to history: %v", e)
	}
	for _, sql := range []string{`ALTER TABLE orders DISABLE TRIGGER order_runtime_fee_quote_marker_immutable`, `UPDATE orders SET runtime_fee_quote_required=false`, `ALTER TABLE orders ENABLE TRIGGER order_runtime_fee_quote_marker_immutable`} {
		if _, e = f.p.Exec(ctx, sql); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = f.s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: o.OutTradeNo, Name: o.Name, Type: o.Type, Amount: o.Amount}); !errors.Is(e, payment.ErrConflict) {
		t.Errorf("cleared marker and missing facts bypassed authenticated birth commitment: %v", e)
	}
	if e = f.s.Store.MarkPaid(ctx, o.TradeNo, "lost-marker168"); !errors.Is(e, payment.ErrConflict) {
		t.Errorf("cleared marker and missing facts bypassed paid birth commitment: %v", e)
	}
}

func TestChannel168LostCurrentSchemaCannotImpersonatePre96(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	o, e := f.s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "schema-loss168", Name: "old-style order", Type: "alipay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(ctx, `DROP TABLE order_route_fee_terms;DROP TABLE provider_route_fee_quotes;DROP TABLE runtime_channel_policies;ALTER TABLE orders DROP COLUMN runtime_fee_quote_required CASCADE`); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Store.MarkPaid(ctx, o.TradeNo, "lost-schema168"); !errors.Is(e, payment.ErrConflict) {
		t.Fatal("registered96 with all pricing schema lost was treated as pre96", e)
	}
	if _, e = f.s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: o.OutTradeNo, Name: o.Name, Type: o.Type, Amount: o.Amount}); !errors.Is(e, payment.ErrConflict) {
		t.Fatal("lost current schema replay treated as history", e)
	}
}

func TestChannel168LostExplicitPolicyCannotDowngradeNewIntentsToLegacy(t *testing.T) {
	for _, offline := range []bool{false, true} {
		t.Run(fmt.Sprint(offline), func(t *testing.T) {
			f := channel168Local(t)
			ctx := context.Background()
			f.configure(t, 7, "99.50")
			if !offline {
				if _, e := f.p.Exec(ctx, `DELETE FROM runtime_channel_policies WHERE channel_id=7`); e == nil {
					t.Fatal("explicit runtime policy could be deleted")
				}
				return
			}
			var guarded bool
			if e := f.p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='runtime_channel_policies'::regclass AND tgname='runtime_channel_policy_no_delete')`).Scan(&guarded); e != nil {
				t.Fatal(e)
			}
			if guarded {
				if _, e := f.p.Exec(ctx, `ALTER TABLE runtime_channel_policies DISABLE TRIGGER runtime_channel_policy_no_delete`); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := f.p.Exec(ctx, `DELETE FROM runtime_channel_policies WHERE channel_id=7`); e != nil {
				t.Fatal(e)
			}
			if guarded {
				if _, e := f.p.Exec(ctx, `ALTER TABLE runtime_channel_policies ENABLE TRIGGER runtime_channel_policy_no_delete`); e != nil {
					t.Fatal(e)
				}
			}
			if w := f.admin("GET", "/7", "", f.csrf); w.Code != 409 {
				t.Errorf("lost explicit policy represented as never configured: %d", w.Code)
			}
			if out := f.pay(t, "policy-loss168", "1.00", 7); out["code"] == float64(1) {
				t.Error("lost explicit policy used legacy fee on new intent")
			}
			if f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 0 {
				t.Error("lost policy passed provider send ownership")
			}
		})
	}
}

func TestChannel168LostPolicyCannotReopenHistoricalFirstSendButKeepsOriginalAttempt(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	existing, e := f.s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "old-attempt168", Name: "old attempt", Type: "alipay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	first, e := f.s.ProviderFlow.Submit(ctx, existing.TradeNo, 7, "native", "192.0.2.1", "")
	if e != nil {
		t.Fatal(e)
	}
	p := compat.Params{"type": "alipay", "method": "native", "name": "old born168", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": "old-pending168", "channel_id": "7"}
	pending, e := f.s.Store.CreateOrder(ctx, create168(p, 100))
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = f.s.providerRoutedRequest(ctx, 1, p); e != nil {
		t.Fatal(e)
	}
	f.configure(t, 7, "99.50")
	if _, e = f.s.ProviderFlow.Submit(ctx, pending.TradeNo, 7, "native", "192.0.2.1", ""); !errors.Is(e, payment.ErrState) {
		t.Fatal("historical firstsend accepted typed policy without quote", e)
	}
	for _, sql := range []string{`ALTER TABLE runtime_channel_policies DISABLE TRIGGER runtime_channel_policy_no_delete`, `DELETE FROM runtime_channel_policies WHERE channel_id=7`, `ALTER TABLE runtime_channel_policies ENABLE TRIGGER runtime_channel_policy_no_delete`} {
		if _, e = f.p.Exec(ctx, sql); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = f.s.ProviderFlow.Submit(ctx, pending.TradeNo, 7, "native", "192.0.2.1", ""); !errors.Is(e, payment.ErrConflict) {
		t.Errorf("lost policy reopened historical firstsend: %v", e)
	}
	replay, e := f.s.ProviderFlow.Submit(ctx, existing.TradeNo, 7, "native", "192.0.2.1", "")
	if e != nil || replay.ID != first.ID || replay.Material != first.Material {
		t.Fatal("original historical attempt no longer recoverable", e)
	}
	if f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 1 {
		t.Error("historical firstsend or original replay submitted another payment")
	}
}
