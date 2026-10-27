package server

import (
	"context"
	"testing"
)

func TestChannel168CNonLastEventCannotReplaceHistoricalBaselineOrder(t *testing.T) {
	f := channel168CConfirmedLocal(t)
	ctx := context.Background()
	for _, v := range []struct{ no, money string }{{"baseline-b-c", "10.00"}, {"pending-a-c", "1.00"}, {"pending-c-c", "1.00"}} {
		if out := f.pay(t, v.no, v.money, 7); out["code"] != float64(1) {
			t.Fatal(out)
		}
	}
	b, e := f.s.Store.Order(ctx, 1, "baseline-b-c")
	if e != nil {
		t.Fatal(e)
	}
	a, e := f.s.Store.Order(ctx, 1, "pending-a-c")
	if e != nil {
		t.Fatal(e)
	}
	c, e := f.s.Store.Order(ctx, 1, "pending-c-c")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, b.TradeNo); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM runtime_channel_success_events`).Scan(&count); e != nil || count != 0 {
		t.Fatal("preconfiguration success was backfilled", count, e)
	}
	channel168CSetLimits(t, f, map[string]string{"daytop": "10.00"})
	for _, o := range []string{a.TradeNo, c.TradeNo} {
		if _, e = f.s.ProviderFlow.Query(ctx, 1, o); e != nil {
			t.Fatal(e)
		}
	}
	state := channel168Data(t, f.admin("GET", "/7/limits", "", f.csrf))
	u := state["usage"].(map[string]any)
	if u["successful_amount"] != "12.00" || u["successful_count"] != "3" || u["day_paused"] != true {
		t.Fatal("historical baseline setup", u)
	}
	if _, e = f.p.Exec(ctx, `ALTER TABLE runtime_channel_success_events DISABLE TRIGGER runtime_channel_success_events_immutable`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(ctx, `UPDATE runtime_channel_success_events SET order_id=$1 WHERE order_id=$2`, b.ID, a.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(ctx, `ALTER TABLE runtime_channel_success_events ENABLE TRIGGER runtime_channel_success_events_immutable`); e != nil {
		t.Fatal(e)
	}
	if w := f.admin("GET", "/7/limits", "", f.csrf); w.Code != 409 {
		t.Fatal("non-last event reassignment silently reduced trusted daily quota", w.Code, w.Body.String())
	}
	if out := f.pay(t, "after-middle-event-corruption-c", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("corrupt middle event reopened parent daily cap", out)
	}
	var routes, orders, attempts int
	if e = f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_route_assignments),(SELECT count(*) FROM orders),(SELECT count(*) FROM provider_attempts)`).Scan(&routes, &orders, &attempts); e != nil || routes != 3 || orders != 3 || attempts != 3 || f.s.ProviderFlow.Providers[7].(*channel168CConfirmedProvider).sends.Load() != 3 {
		t.Fatal("corruption created new route/order/attempt/send", routes, orders, attempts, e)
	}
}

func TestChannel168CAuditOriginCannotLoseSchemaAndAdmitNewIntent(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	channel168CSetLimits(t, f, map[string]string{"paymax": "0.01"})
	if _, e := f.p.Exec(ctx, `DROP TABLE runtime_channel_success_days;DROP TABLE runtime_channel_success_events;DROP TABLE runtime_channel_limits;DELETE FROM schema_migrations WHERE name='000099_runtime_channel_limits.up.sql'`); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "new-intent-after-schema-anchor-loss", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("prior global policy origin downgraded a fresh intent to pre99", out)
	}
	var routes, orders, attempts int
	if e := f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_route_assignments),(SELECT count(*) FROM orders),(SELECT count(*) FROM provider_attempts)`).Scan(&routes, &orders, &attempts); e != nil || routes+orders+attempts != 0 || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 0 {
		t.Fatal("schema loss consumed new route/birth/attempt/send", routes, orders, attempts, e)
	}
}

func TestChannel168CAuthenticatedAdmissionCannotLoseSchemaAndMigrationAnchor(t *testing.T) {
	f := channel168CConfirmedLocal(t)
	ctx := context.Background()
	channel168CSetLimits(t, f, map[string]string{"daymaxorder": "1"})
	if out := f.pay(t, "c-schema-and-anchor-loss", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(ctx, 1, "c-schema-and-anchor-loss")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(ctx, `DROP TABLE runtime_channel_success_days;DROP TABLE runtime_channel_success_events;DROP TABLE runtime_channel_limits;DELETE FROM schema_migrations WHERE name='000099_runtime_channel_limits.up.sql'`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e == nil {
		t.Fatal("authenticated 99 admission became pre99 settlement after schema+anchor loss")
	}
	o, e = f.s.Store.Order(ctx, 1, o.OutTradeNo)
	if e != nil || o.Status == "paid" {
		t.Fatal("source loss committed paid money", o.Status, e)
	}
}

func TestChannel168CLostSchemaOrQuoteCommitmentCannotDowngrade(t *testing.T) {
	for _, change := range []string{"partial-schema", "all-schema", "policy-mac", "remove-admission"} {
		t.Run(change, func(t *testing.T) {
			f := channel168Local(t)
			ctx := context.Background()
			channel168CSetLimits(t, f, map[string]string{"paymax": "10.00"})
			if out := f.pay(t, "sealed-global-c", "1.00", 7); out["code"] != float64(1) {
				t.Fatal(out)
			}
			sql := ""
			switch change {
			case "partial-schema":
				sql = `DROP TABLE runtime_channel_success_days`
			case "all-schema":
				sql = `DROP TABLE runtime_channel_success_days;DROP TABLE runtime_channel_success_events;DROP TABLE runtime_channel_limits`
			case "policy-mac":
				sql = `UPDATE runtime_channel_limits SET paymax=1000000`
			case "remove-admission":
				sql = `ALTER TABLE provider_route_fee_quotes DISABLE TRIGGER provider_route_fee_quote_immutable;UPDATE provider_route_fee_quotes SET facts=facts-'GlobalLimits';ALTER TABLE provider_route_fee_quotes ENABLE TRIGGER provider_route_fee_quote_immutable`
			}
			if _, e := f.p.Exec(ctx, sql); e != nil {
				t.Fatal(e)
			}
			if change == "remove-admission" {
				if out := f.pay(t, "sealed-global-c", "1.00", 7); out["code"] == float64(1) {
					t.Fatal("admission removal forged old nil encoding")
				}
			} else if out := f.pay(t, "after-global-damage-c", "1.00", 7); out["code"] == float64(1) {
				t.Fatal("lost/corrupted global facts downgraded new payment", change)
			}
			var orders int
			if e := f.p.QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&orders); e != nil || orders != 1 {
				t.Fatal("corruption changed original order set", orders, e)
			}
		})
	}
}

func TestChannel168CLostPolicyNeverBecomesUnconfigured(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	channel168CSetLimits(t, f, map[string]string{"daymaxorder": "1"})
	if _, e := f.p.Exec(ctx, `DELETE FROM runtime_channel_limits WHERE channel_id=7`); e == nil {
		t.Fatal("global policy could be deleted")
	}
	for _, sql := range []string{`ALTER TABLE runtime_channel_limits DISABLE TRIGGER runtime_channel_limits_no_delete`, `DELETE FROM runtime_channel_limits WHERE channel_id=7`, `ALTER TABLE runtime_channel_limits ENABLE TRIGGER runtime_channel_limits_no_delete`} {
		if _, e := f.p.Exec(ctx, sql); e != nil {
			t.Fatal(e)
		}
	}
	if w := f.admin("GET", "/7/limits", "", f.csrf); w.Code != 409 {
		t.Fatal("policy loss reset configured state", w.Code)
	}
	if out := f.pay(t, "lost-policy-c", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("lost limits policy admitted new payment")
	}
}

func TestChannel168CSuccessFactLossOrCorruptionCannotResetDailyQuota(t *testing.T) {
	for _, change := range []string{"head", "events-and-head", "head-money", "last-proof"} {
		t.Run(change, func(t *testing.T) {
			f := channel168CConfirmedLocal(t)
			ctx := context.Background()
			channel168CSetLimits(t, f, map[string]string{"daymaxorder": "2"})
			if out := f.pay(t, "integrity-paid-c", "1.00", 7); out["code"] != float64(1) {
				t.Fatal(out)
			}
			o, e := f.s.Store.Order(ctx, 1, "integrity-paid-c")
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e != nil {
				t.Fatal(e)
			}
			if _, e = f.p.Exec(ctx, `DELETE FROM runtime_channel_success_events WHERE order_id=$1`, o.ID); e == nil {
				t.Fatal("success event was mutable")
			}
			statements := []string{}
			switch change {
			case "head":
				statements = []string{`ALTER TABLE runtime_channel_success_days DISABLE TRIGGER runtime_channel_success_days_no_delete`, `DELETE FROM runtime_channel_success_days`, `ALTER TABLE runtime_channel_success_days ENABLE TRIGGER runtime_channel_success_days_no_delete`}
			case "events-and-head":
				statements = []string{`ALTER TABLE runtime_channel_success_days DISABLE TRIGGER runtime_channel_success_days_no_delete`, `DELETE FROM runtime_channel_success_days`, `ALTER TABLE runtime_channel_success_days ENABLE TRIGGER runtime_channel_success_days_no_delete`, `ALTER TABLE runtime_channel_success_events DISABLE TRIGGER runtime_channel_success_events_immutable`, `DELETE FROM runtime_channel_success_events`, `ALTER TABLE runtime_channel_success_events ENABLE TRIGGER runtime_channel_success_events_immutable`}
			case "head-money":
				statements = []string{`UPDATE runtime_channel_success_days SET realmoney=1`}
			case "last-proof":
				statements = []string{`ALTER TABLE runtime_channel_success_events DISABLE TRIGGER runtime_channel_success_events_immutable`, `UPDATE runtime_channel_success_events SET proof_hash=decode(repeat('11',32),'hex')`, `ALTER TABLE runtime_channel_success_events ENABLE TRIGGER runtime_channel_success_events_immutable`}
			}
			for _, sql := range statements {
				if _, e = f.p.Exec(ctx, sql); e != nil {
					t.Fatal(e)
				}
			}
			if w := f.admin("GET", "/7/limits", "", f.csrf); w.Code != 409 {
				t.Fatal("damaged successful quota was represented as zero/valid", change, w.Code)
			}
			if out := f.pay(t, "after-source-loss-c", "1.00", 7); out["code"] == float64(1) {
				t.Fatal("damaged quota admitted new payment", change)
			}
			var orders int
			if e = f.p.QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&orders); e != nil || orders != 1 {
				t.Fatal("damaged source left new order", orders, e)
			}
		})
	}
}
