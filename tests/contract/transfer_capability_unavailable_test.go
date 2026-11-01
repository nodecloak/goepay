package contract_test

import (
	"fmt"
	"testing"

	"goepay/internal/identity"
)

func TestTransferCapabilitiesDisabledGroupPreservesHistoryWithoutInventingQuote(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := t.Context()
	batch, provider := seedMerchantHistoryBatch(t, s, pool, "disabled-capability-history", false)
	var group int64
	if err := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('groups','{"name":"capability-history-group","enabled":true,"user_transfer":"1","transfer_rate":"1.00"}')RETURNING id`).Scan(&group); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{group_id}',to_jsonb($1::bigint::text)) WHERE id=1`, group); err != nil {
		t.Fatal(err)
	}
	token, csrf, err := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	path := "/console/v1/merchant/transfers/capabilities"
	w := lookupHTTP(t, s.Handler(), "GET", path, "", token, "merchant", csrf)
	if w.Code != 200 {
		t.Fatal("healthy group capability unavailable", w.Code, w.Body.String())
	}
	healthy := lookupData(t, w)
	if healthy["fee_policy_available"] != true || healthy["funds_policy_available"] != true || healthy["transfer_rate"] != "1.00" {
		t.Error("healthy quote contract lost effective fee", healthy)
	}
	if _, err := pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"enabled":false,"transfer_rate":"malformed-disabled-fee"}'::jsonb WHERE id=$1`, group); err != nil {
		t.Fatal(err)
	}
	w = lookupHTTP(t, s.Handler(), "GET", path, "", token, "merchant", csrf)
	if w.Code != 200 {
		t.Errorf("disabled business permission erased capability/history: %d %s", w.Code, w.Body.String())
	} else {
		data := lookupData(t, w)
		if data["fee_policy_available"] != false || data["funds_policy_available"] != false || data["writes_enabled"] != false || data["effective_enabled"] != false || data["permission_source"] != "group_disabled" || data["effective_group_id"] != fmt.Sprint(group) {
			t.Error("disabled group advertised a quote or write capability", data)
		}
		if data["ledger_available_money"] != "8.00" || data["reserved_money"] != "2.00" {
			t.Error("capability invented raw balances", data)
		}
		for _, key := range []string{"transfer_rate", "available_money", "today_blocked_money", "settle_type"} {
			if _, exists := data[key]; exists {
				t.Error("unavailable policy was replaced with a guessed value", key, data[key])
			}
		}
		channels, ok := data["channels"].([]any)
		if !ok || len(channels) != 1 || channels[0].(map[string]any)["id"] != "7" {
			t.Error("disabled projection lost genuine channel metadata", data["channels"])
		}
		for _, key := range []string{"minimum_money", "maximum_money", "daily_recipient_limit", "default_remark", "default_title"} {
			if data[key] != healthy[key] {
				t.Error("read-only projection changed genuine global limits", key, data[key], healthy[key])
			}
		}
	}
	var before [3]int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM transfers),(SELECT count(*) FROM transfer_batches),(SELECT count(*) FROM journals)`).Scan(&before[0], &before[1], &before[2]); err != nil {
		t.Fatal(err)
	}
	for _, history := range []string{"/console/v1/merchant/transfers", fmt.Sprintf("/console/v1/merchant/transfer-batches/%d", batch.ID)} {
		if r := lookupHTTP(t, s.Handler(), "GET", history, "", token, "merchant", csrf); r.Code != 200 {
			t.Error("disabled group lost historical business state", history, r.Code)
		}
	}
	if r := lookupHTTP(t, s.Handler(), "POST", "/console/v1/merchant/transfers", `{"channel_id":"7","type":"alipay","account":"synthetic-new-denied","amount":"1.00","external_no":"disabled-group-new-denied"}`, token, "merchant", csrf); r.Code != 422 && r.Code != 403 {
		t.Error("disabled group was not a strict new-write refusal", r.Code, r.Body.String())
	}
	var after [3]int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM transfers),(SELECT count(*) FROM transfer_batches),(SELECT count(*) FROM journals)`).Scan(&after[0], &after[1], &after[2]); err != nil || before != after {
		t.Error("capability/history/new refusal modified money", before, after, err)
	}
	if b, e := s.Store.Balance(ctx, 1); e != nil || b.Available != 800 || b.Reserved != 200 {
		t.Error("disabled projection changed real balances", b, e)
	}
	if provider.writes.Load() != 0 || provider.queries.Load() != 0 {
		t.Error("read-only capability/history invoked upstream", provider.writes.Load(), provider.queries.Load())
	}
	// Only the explicit group-disabled condition may become a partial DTO.
	if _, err := pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','true') WHERE id=$1`, group); err != nil {
		t.Fatal(err)
	}
	if r := lookupHTTP(t, s.Handler(), "GET", path, "", token, "merchant", csrf); r.Code != 422 {
		t.Error("genuine malformed fee silently downgraded to a successful projection", r.Code)
	}
	if _, err := pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"transfer_rate":"1.00","settle_type":"bad"}'::jsonb WHERE id=$1`, group); err != nil {
		t.Fatal(err)
	}
	if r := lookupHTTP(t, s.Handler(), "GET", path, "", token, "merchant", csrf); r.Code != 422 {
		t.Error("genuine malformed maturity silently downgraded to success", r.Code)
	}
}
