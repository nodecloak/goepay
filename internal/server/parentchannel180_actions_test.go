package server

import (
	"encoding/json"
	"goepay/internal/payment"
	"testing"
)

func TestParentChannel180ActualHTTPCopyCannotExpandOriginalMerchantGrantSnapshot(t *testing.T) {
	f, body := parent180ReadyCreate(t)
	first := parent180Post(t, f, body)
	ctx := t.Context()
	if e := f.s.Store.CreateMerchant(ctx, payment.Merchant{ID: 2, Name: "later synthetic operator owner", APIKey: "synthetic180-owner2"}); e != nil {
		t.Fatal(e)
	}
	f.s.MerchantChannels[2] = []int64{8}
	w := parent180HTTP(f, "GET", "/templates", "")
	var choices struct {
		Data struct{ Items []map[string]any }
	}
	if json.Unmarshal(w.Body.Bytes(), &choices) != nil || w.Code != 200 || len(choices.Data.Items) != 1 {
		t.Fatal("new trusted operator template missing", w.Code)
	}
	body["expected_source_fingerprint"] = choices.Data.Items[0]["fingerprint"]
	body["copy_from_channel_id"] = first["id"]
	body["expected_copy_fingerprint"] = first["fingerprint"]
	body["request_id"] = "abababababababababababababababab"
	body["name"] = "synthetic restricted copy180"
	body["account"] = map[string]any{}
	copied := parent180Post(t, f, body)
	var count int
	if e := f.p.QueryRow(ctx, `SELECT count(*) FROM runtime_parent_channel_grants WHERE channel_id=$1 AND merchant_id=2`, copied["id"]).Scan(&count); e != nil || count != 0 {
		t.Fatal("copy widened original immutable owner grant", count, e)
	}
	if e := f.p.QueryRow(ctx, `SELECT count(*) FROM runtime_parent_channel_grants WHERE channel_id=$1 AND merchant_id=1`, copied["id"]).Scan(&count); e != nil || count != 1 {
		t.Fatal("copy lost original admitted owner", count, e)
	}
}

func TestParentChannel180ActualHTTPOriginalParentActions(t *testing.T) {
	f, body := parent180ReadyCreate(t)
	first := parent180Post(t, f, body)
	id := first["id"].(string)
	t.Run("copy-server-secret-default-off", func(t *testing.T) {
		copyBody := map[string]any{}
		for k, v := range body {
			copyBody[k] = v
		}
		copyBody["request_id"] = "abcdef1234567890abcdef1234567890"
		copyBody["name"] = "复制的父通道180"
		copyBody["account"] = map[string]any{}
		copyBody["copy_from_channel_id"] = id
		copyBody["expected_copy_fingerprint"] = first["fingerprint"]
		raw, _ := json.Marshal(copyBody)
		w := parent180HTTP(f, "POST", "", string(raw))
		if w.Code != 200 {
			t.Fatalf("server copy unavailable: %d %s", w.Code, w.Body.String())
		}
		var out struct{ Data map[string]any }
		json.Unmarshal(w.Body.Bytes(), &out)
		if out.Data["id"] == id || out.Data["enabled"] != false {
			t.Fatal("copy did not create distinct disabled parent")
		}
	})
	t.Run("atomic-batch-conflict", func(t *testing.T) {
		secondBody := map[string]any{}
		for k, v := range body {
			secondBody[k] = v
		}
		secondBody["name"] = "批量父通道180"
		secondBody["request_id"] = "11111111111111111111111111111111"
		second := parent180Post(t, f, secondBody)
		request := map[string]any{"action": "open", "items": []any{map[string]any{"id": id, "expected_fingerprint": first["fingerprint"]}, map[string]any{"id": second["id"], "expected_fingerprint": "0000000000000000000000000000000000000000000000000000000000000000"}}}
		raw, _ := json.Marshal(request)
		w := parent180HTTP(f, "POST", "/batch", string(raw))
		if w.Code != 409 {
			t.Fatalf("stale atomic batch not rejected: %d", w.Code)
		}
		var enabled int
		if e := f.p.QueryRow(t.Context(), `SELECT count(*) FROM runtime_channel_policies WHERE enabled`).Scan(&enabled); e != nil || enabled != 0 {
			t.Fatal("batch partially committed", enabled, e)
		}
	})
	t.Run("archive-keeps-account", func(t *testing.T) {
		raw, _ := json.Marshal(map[string]any{"expected_fingerprint": first["fingerprint"], "archived": true})
		w := parent180HTTP(f, "PATCH", "/"+id, string(raw))
		if w.Code != 200 {
			t.Fatalf("archive unavailable: %d %s", w.Code, w.Body.String())
		}
		var n int
		if e := f.p.QueryRow(t.Context(), `SELECT count(*) FROM runtime_parent_channel_accounts WHERE channel_id=$1`, id).Scan(&n); e != nil || n != 1 {
			t.Fatal("archive destroyed account history", n, e)
		}
	})
}

func TestParentChannel180ActualHTTPFiltersDetailAndAtomicEnable(t *testing.T) {
	f, body := parent180ReadyCreate(t)
	first := parent180Post(t, f, body)
	id := first["id"].(string)
	w := parent180HTTP(f, "GET", "?kw="+id+"&type=wxpay&plugin=wxpay&dstatus=0&page=1&size=20", "")
	if w.Code != 200 {
		t.Fatalf("original filters unavailable %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Data struct {
			Items []map[string]any
			Total int
		}
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.Data.Total != 1 || len(result.Data.Items) != 1 || result.Data.Items[0]["id"] != id {
		t.Fatal("filter does not match actual runtime ID")
	}
	w = parent180HTTP(f, "GET", "/"+id, "")
	if w.Code != 200 {
		t.Fatal("detail unavailable", w.Code)
	}
	raw, _ := json.Marshal(map[string]any{"action": "open", "items": []any{map[string]any{"id": id, "expected_fingerprint": first["fingerprint"]}}})
	w = parent180HTTP(f, "POST", "/batch", string(raw))
	if w.Code != 200 {
		t.Fatalf("batch enable unavailable %d %s", w.Code, w.Body.String())
	}
	for _, query := range []string{"?kw=1&kw=2", "?size=1001", "?unknown=1", "?page=0", "?dstatus=2"} {
		if w := parent180HTTP(f, "GET", query, ""); w.Code != 400 {
			t.Fatal("malformed filter accepted", query, w.Code)
		}
	}
}

func TestParentChannel180ActualHTTPAdoptsRegisteredParentWithoutPaymentOrInventingFee(t *testing.T) {
	f, body := parent180ReadyCreate(t)
	raw, _ := json.Marshal(map[string]any{"expected_source_fingerprint": body["expected_source_fingerprint"]})
	w := parent180HTTP(f, "POST", "/8/adopt", string(raw))
	if w.Code != 200 {
		t.Fatalf("registered parameter entry unavailable: %d %s", w.Code, w.Body.String())
	}
	var out struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Data["id"] != "8" || out.Data["rate"] != nil || out.Data["account_version"] != "1" {
		t.Fatal("adopt fabricated fees or another ID")
	}
	var n int
	if e := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM provider_attempts)+(SELECT count(*) FROM runtime_channel_policies WHERE channel_id=8)`).Scan(&n); e != nil || n != 0 {
		t.Fatal("configuration adoption created payment or financial policy", n, e)
	}
}

func TestParentChannel180ActualHTTPStatsUsesActualParentAndUnknownCost(t *testing.T) {
	f, body := parent180ReadyCreate(t)
	first := parent180Post(t, f, body)
	w := parent180HTTP(f, "GET", "/"+first["id"].(string)+"/statistics", "")
	if w.Code != 200 {
		t.Fatalf("parent statistics unavailable %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			ChannelID string           `json:"channel_id"`
			Items     []map[string]any `json:"items"`
		}
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Data.ChannelID != first["id"] || len(out.Data.Items) != 7 {
		t.Fatal("statistics dimension mismatch", e)
	}
	for _, day := range out.Data.Items {
		if day["realmoney"] != "0.00" || day["profitmoney"] != nil {
			t.Fatal("unknown source fabricated profit", day)
		}
	}
}

func TestParentChannel180ActualHTTPUnknownCreateReadsOnlyOriginalRequest(t *testing.T) {
	f, body := parent180ReadyCreate(t)
	first := parent180Post(t, f, body)
	w := parent180HTTP(f, "GET", "/requests/"+body["request_id"].(string), "")
	if w.Code != 200 {
		t.Fatalf("original create request lookup unavailable %d", w.Code)
	}
	var out struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Data["id"] != first["id"] {
		t.Fatal("lookup selected another created channel")
	}
	if w := parent180HTTP(f, "GET", "/requests/22222222222222222222222222222222", ""); w.Code != 404 {
		t.Fatal("missing original request fabricated success", w.Code)
	}
	var n int
	if e := f.p.QueryRow(t.Context(), `SELECT count(*) FROM runtime_parent_channel_requests`).Scan(&n); e != nil || n != 1 {
		t.Fatal("lookup wrote request", n, e)
	}
}

func TestParentChannel180ActualTLSStatisticsConsumesVerifiedActualParentReceipt(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	if out := parent180WxPay(t, f, setup.id, "parent180-statistics-paid"); out["code"] != float64(1) {
		t.Fatal(out)
	}
	order, e := f.s.Store.Order(t.Context(), 1, "parent180-statistics-paid")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(t.Context(), 1, order.TradeNo); e != nil {
		t.Fatal(e)
	}
	w := parent180HTTP(f, "GET", "/"+setup.id+"/statistics", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		Data struct{ Items []map[string]any }
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	today := out.Data.Items[len(out.Data.Items)-1]
	if today["realmoney"] != "100.00" || today["orderCount"] != "1" || today["successCount"] != "1" || today["successRate"] != "100.00" || today["profitmoney"] != nil {
		t.Fatal("verified parent receipt was not reflected exactly", today)
	}
	if setup.queries.Load() != 1 {
		t.Fatal("statistics contacted provider", setup.queries.Load())
	}
}
