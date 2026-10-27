package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestSubchannel181ParentLimitsAndFrequencyApplyOnceAcrossChildren(t *testing.T) {
	f, factory := subchannel168BLocal(t)
	child168BCreate(t, f, "limit-child-a", true)
	child168BCreate(t, f, "limit-child-b", true)
	channel168CSetLimits(t, f, map[string]string{"paymin": "1.00", "paymax": "2.00", "rate_limit_seconds": "60", "rate_limit_count": "1"})
	ctx := context.Background()
	for i, money := range []string{"0.99", "2.01"} {
		if _, _, e := f.s.providerRoutedRequest(ctx, 1, child168BParams(fmt.Sprint("invalid181-", i), money)); e == nil {
			t.Fatal("child bypassed parent original-money bounds", money)
		}
	}
	if out := f.pay(t, "limited181-first", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("eligible child cannot acquire the same parent admission", out)
	}
	if out := f.pay(t, "limited181-first", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("original child replay lost admission", out)
	}
	if out := f.pay(t, "limited181-second", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("another child bypassed shared parent start budget", out)
	}
	var starts, sources, quotes int
	var bound bool
	if e := f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_route_assignments),(SELECT count(*) FROM payment_subchannel_routes),(SELECT count(*) FROM provider_route_fee_quotes),EXISTS(SELECT 1 FROM provider_route_fee_quotes WHERE facts->>'Version'='2' AND facts->'child_execution' IS NOT NULL AND facts->'GlobalLimits'->>'Channel'='7')`).Scan(&starts, &sources, &quotes, &bound); e != nil || starts != 1 || sources != 1 || quotes != 1 || !bound {
		t.Fatal("child/global facts were not one original intent", starts, sources, quotes, bound, e)
	}
	var sends int64
	for _, p := range factory.providers {
		sends += p.sends.Load()
	}
	if sends != 1 {
		t.Fatal("same intent or rejected child caused another create", sends)
	}
}

func TestSubchannel181SuccessfulQuotaUsesTrustedGrossAndOriginalChildQuery(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	ctx := context.Background()
	if _, e := f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) SELECT 'settings','{"key":"payment_fee_enabled","value":"true"}'::jsonb WHERE NOT EXISTS(SELECT 1 FROM backoffice_records WHERE resource='settings' AND body->>'key'='payment_fee_enabled');UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"true"') WHERE resource='settings' AND body->>'key'='payment_fee_enabled'`); e != nil {
		t.Fatal(e)
	}
	policy := channel168Data(t, f.admin("GET", "/7", "", f.csrf))
	body, _ := json.Marshal(map[string]any{"enabled": true, "mode": 0, "rate": "99.00", "expected_fingerprint": policy["fingerprint"]})
	channel168Data(t, f.admin("PATCH", "/7", string(body), f.csrf))
	channel168CSetLimits(t, f, map[string]string{"daytop": "1.00"})
	if out := f.pay(t, "gross181-child", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("configured parent refused first child", out)
	}
	o, e := f.s.Store.Order(ctx, 1, "gross181-child")
	if e != nil {
		t.Fatal(e)
	}
	if o.Amount != 100 || o.MerchantAmount != 99 || o.PlatformFee != 1 {
		t.Fatal("original fee split changed", o.Amount, o.MerchantAmount, o.PlatformFee)
	}
	usage := channel168Data(t, f.admin("GET", "/7/limits", "", f.csrf))["usage"].(map[string]any)
	if usage["successful_count"] != "0" {
		t.Fatal("unconfirmed child consumed success quota", usage)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e != nil {
		t.Fatal("query did not restore original child", e)
	}
	usage = channel168Data(t, f.admin("GET", "/7/limits", "", f.csrf))["usage"].(map[string]any)
	if usage["successful_count"] != "1" || usage["successful_amount"] != "1.00" || usage["day_paused"] != true {
		t.Fatal("parent quota used net or omitted child receipt", usage)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "gross181-over-cap", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("paid child failed to close shared parent daily cap")
	}
	if factory.providers["products-v1"].creates.Load() != 1 {
		t.Fatal("quota/query replay created another payment")
	}
	var events int
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM runtime_channel_success_events`).Scan(&events); e != nil || events != 1 {
		t.Fatal("trusted receipt replay duplicated parent success", events, e)
	}
}

func TestSubchannel181LastChildAndQuoteWritesCannotCrossParentClosingHour(t *testing.T) {
	for _, target := range []string{"payment_subchannel_last_used", "provider_route_fee_quotes"} {
		t.Run(target, func(t *testing.T) {
			f, factory := subchannel168BLocal(t)
			child168BCreate(t, f, "hour181-child", true)
			local := time.Now().In(time.FixedZone("UTC+8", 8*3600))
			channel168CSetLimits(t, f, map[string]string{"timestart": fmt.Sprint((local.Hour() + 22) % 24), "timestop": fmt.Sprint(local.Hour())})
			near := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), 59, 59, 500000000, local.Location())
			f = channel168CClock(t, f, near)
			ctx := context.Background()
			if _, e := f.p.Exec(ctx, `CREATE SEQUENCE child181_clock_entered;CREATE FUNCTION child181_cross_hour()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM nextval('child181_clock_entered');UPDATE channel168c_test_clock SET instant=date_trunc('hour',instant)+interval '1 hour 1 millisecond';RETURN NEW;END$$;CREATE TRIGGER child181_cross_hour AFTER INSERT ON `+target+` FOR EACH ROW EXECUTE FUNCTION child181_cross_hour()`); e != nil {
				t.Fatal(e)
			}
			_, _, e := f.s.providerRoutedRequest(ctx, 1, child168BParams("hour181-intent", "1.00"))
			var entered bool
			if x := f.p.QueryRow(ctx, `SELECT is_called FROM child181_clock_entered`).Scan(&entered); x != nil || !entered {
				t.Fatal("test did not reach the original final-write boundary", entered, x, e)
			}
			if e == nil {
				t.Fatal("child route committed after original parent closing hour", target)
			}
			var routes, sources, heads, quotes, orders int
			if e = f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_route_assignments),(SELECT count(*) FROM payment_subchannel_routes),(SELECT count(*) FROM payment_subchannel_last_used),(SELECT count(*) FROM provider_route_fee_quotes),(SELECT count(*) FROM orders)`).Scan(&routes, &sources, &heads, &quotes, &orders); e != nil || routes+sources+heads+quotes+orders != 0 {
				t.Fatal("expired parent admission did not roll back all child facts", routes, sources, heads, quotes, orders, e)
			}
			if factory.providers["hour181-child"].sends.Load() != 0 {
				t.Fatal("expired route reached external create")
			}
		})
	}
}

func TestSubchannel181LostGlobalSchemaCannotExposeOldChildCapabilities(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	channel168CSetLimits(t, f, map[string]string{"daymaxorder": "2"})
	if out := f.pay(t, "schema181-child", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("original admitted child create", out)
	}
	ctx := context.Background()
	o, e := f.s.Store.Order(ctx, 1, "schema181-child")
	if e != nil {
		t.Fatal(e)
	}
	before, e := f.s.ProviderFlow.OriginalOrderCapabilities(ctx, 1, []int64{o.ID})
	if e != nil || !before[o.ID].Query {
		t.Fatal("positive original child capability missing", e)
	}
	if _, e = f.p.Exec(ctx, `DROP TABLE runtime_channel_success_days,runtime_channel_success_events,runtime_channel_limits CASCADE;DELETE FROM schema_migrations WHERE name='000099_runtime_channel_limits.up.sql'`); e != nil {
		t.Fatal(e)
	}
	after, e := f.s.ProviderFlow.OriginalOrderCapabilities(ctx, 1, []int64{o.ID})
	if len(after) != 0 {
		t.Fatal("batch capabilities borrowed parent despite loss of authenticated global source", after, e)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e == nil {
		t.Fatal("single original child query ignored lost global source")
	}
	if factory.providers["products-v1"].queries.Load() != 0 || factory.providers["products-v1"].creates.Load() != 1 {
		t.Fatal("source-loss GET or query performed upstream I/O")
	}
}

func TestSubchannel181NewLimitsAndAccountVersionDoNotRepriceOriginalRecovery(t *testing.T) {
	f, factory, row := subchannel168BProductLocal(t)
	ctx := context.Background()
	channel168CSetLimits(t, f, map[string]string{"daytop": "100.00"})
	if out := f.pay(t, "recovery181-child", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("original child", out)
	}
	o, e := f.s.Store.Order(ctx, 1, "recovery181-child")
	if e != nil {
		t.Fatal(e)
	}
	current := channel168Data(t, f.childHTTP("GET", "/"+row["id"].(string), ""))
	change, _ := json.Marshal(map[string]any{"expected_fingerprint": current["fingerprint"], "values": map[string]any{"app": map[string]string{"operation": "replace", "value": "products-v2"}, "secret": map[string]string{"operation": "keep"}}})
	channel168Data(t, f.childHTTP("PATCH", "/"+row["id"].(string), string(change)))
	channel168CSetLimits(t, f, map[string]string{"paymin": "2.00", "rate_limit_seconds": "60", "rate_limit_count": "1"})
	if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e != nil {
		t.Fatal("new policy or current child replaced original query", e)
	}
	refund, e := f.s.ProviderFlow.Refund(ctx, 1, o.TradeNo, "recovery181-refund", 100)
	if e != nil || refund.Status != "succeeded" {
		t.Fatal("new policy or current child replaced original refund", refund.Status, e)
	}
	if out := f.pay(t, "recovery181-new-blocked", "2.00", 7); out["code"] == float64(1) {
		t.Fatal("new child bypassed shared parent frequency")
	}
	old := factory.providers["products-v1"]
	next := factory.providers["products-v2"]
	if old.creates.Load() != 1 || old.queries.Load() != 1 || old.refunds.Load() != 1 || next.creates.Load() != 0 || next.queries.Load() != 0 || next.refunds.Load() != 0 {
		t.Fatal("original private account not used exactly once")
	}
	after, e := f.s.Store.Order(ctx, 1, o.OutTradeNo)
	if e != nil || after.Amount != o.Amount || after.MerchantAmount != o.MerchantAmount || after.PlatformFee != o.PlatformFee {
		t.Fatal("old fee terms repriced", e)
	}
}
