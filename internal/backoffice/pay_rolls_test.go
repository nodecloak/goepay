package backoffice

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func payRollValues() map[string]any {
	return map[string]any{"name": "routing", "merchant_id": "101", "type": "alipay", "enabled": true, "strategy": "round_robin", "channel_ids": "7:2,8:1", "limits": `{"min_amount":"1.00","max_amount":"10.00","start_hour":22,"end_hour":2,"rate_limit_seconds":60,"rate_limit_count":2}`, "rules": `{"7":{"daily_count":5,"daily_amount":"100.00"}}`}
}

func TestPayRollSaveRequiresExecutableScopeAndStrictJSON(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	v := payRollValues()
	create := func(values map[string]any) error {
		raw := map[string]json.RawMessage{}
		for key, value := range values {
			raw[key], _ = json.Marshal(value)
		}
		_, e := s.Create(ctx, admin, "pay-rolls", raw)
		return e
	}
	for _, bad := range []map[string]any{{"name": "legacy", "strategy": "round_robin", "channel_ids": "7:1", "enabled": true}, {"name": "invalid", "merchant_id": "101", "type": "alipay", "strategy": "first", "channel_ids": "7:1", "enabled": true, "limits": `{"daily_count":1,"daily_count":100}`}} {
		if e := create(bad); e == nil {
			t.Fatal("invalid rule persisted", bad)
		}
	}
	if e := create(v); e != nil {
		t.Fatal(e)
	}
	if e := create(v); e == nil {
		t.Fatal("multiple enabled rules for same scope were accepted")
	}
	var records, audits int
	if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM backoffice_records WHERE resource='pay-rolls'),(SELECT count(*) FROM backoffice_audit WHERE resource='pay-rolls')").Scan(&records, &audits); e != nil || records != 1 || audits != 1 {
		t.Fatal("invalid rule leaked record or audit", records, audits, e)
	}
}
func TestPayRollStrictDefinitionAndUTC8Limit(t *testing.T) {
	r, e := ParsePayRoll(payRollValues())
	if e != nil {
		t.Fatal(e)
	}
	if r.MerchantID != 101 || len(r.Channels) != 2 || r.Channels[0].Weight != 2 || r.Rules[7].DailyAmount != 10000 {
		t.Fatal(r)
	}
	for _, hour := range []int{22, 23, 0, 1, 2} {
		if !r.Limits.Allows(100, time.Date(2026, 10, 23, hour, 0, 0, 0, time.FixedZone("UTC+8", 28800))) {
			t.Fatal("cross-midnight interval excluded", hour)
		}
	}
	if r.Limits.Allows(100, time.Date(2026, 10, 23, 3, 0, 0, 0, time.FixedZone("UTC+8", 28800))) || r.Limits.Allows(99, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("limit bypass")
	}
	for _, change := range []struct {
		key   string
		value any
	}{{"merchant_id", ""}, {"merchant_id", "-1"}, {"merchant_id", "01"}, {"type", "sandbox"}, {"enabled", "true"}, {"strategy", "random"}, {"channel_ids", "7:1,7:2"}, {"channel_ids", "7:1,8:-1"}, {"channel_ids", "7:1e2"}, {"limits", `{"min_amount":1.2}`}, {"limits", `{"min_amount":"1.00","min_amount":"2.00"}`}, {"limits", `{"rate_limit_count":1e2,"rate_limit_seconds":60}`}, {"limits", `{"rate_limit_seconds":60}`}, {"limits", `{"start_hour":22}`}, {"limits", `{"start_hour":24,"end_hour":1}`}, {"limits", `{"daily_count":1.5}`}, {"limits", `{"unknown":1}`}, {"limits", `{"min_amount":"2.00","max_amount":"1.00"}`}, {"rules", `{"9":{"daily_count":2}}`}, {"rules", `{"7":{"daily_count":2},"7":{"daily_count":3}}`}} {
		v := payRollValues()
		v[change.key] = change.value
		if _, e := ParsePayRoll(v); e == nil {
			t.Fatal("unsafe routing definition accepted", change.key, change.value)
		}
	}
	v := payRollValues()
	delete(v, "merchant_id")
	if _, e := ParsePayRoll(v); e == nil {
		t.Fatal("legacy record became executable")
	}
	v = payRollValues()
	v["strategy"] = "weighted"
	v["channel_ids"] = "7,8"
	if _, e := ParsePayRoll(v); e == nil {
		t.Fatal("missing PHP weights became an invented positive total")
	}
}
