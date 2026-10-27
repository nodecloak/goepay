package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func (f risk177Fixture) riskPath(method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/payment-risk"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func TestRisk177ActionsReadAndOriginalSuperCASRelease(t *testing.T) {
	f, _, creates := risk177Flow(t)
	v := risk177Data(t, f.request("GET", "", f.csrf))
	v = risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) {
		s["auto_check_payip"] = "1"
		s["check_payip_second"] = "3600"
		s["check_payip_count"] = "1"
	}), f.csrf))
	first := f.pay(t, "risk177-action-original", "192.0.2.177")
	if first["code"] != float64(1) {
		t.Fatal(first)
	}
	f.check(t, v, "risk177-action-check")
	w := f.riskPath("GET", "/actions", "")
	data := risk177Data(t, w)
	items, ok := data["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatal("missing action list", data)
	}
	item := items[0].(map[string]any)
	id, ok := item["id"].(string)
	if !ok || item["active"] != true {
		t.Fatal("action full-ID or state wrong", item)
	}
	for _, secret := range []string{"192.0.2.177", "subject_key", "proof_hash", "Previous", "api_key", "lease_token"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("action DTO exposed private source", secret)
		}
	}
	body, _ := json.Marshal(map[string]any{"expected_fingerprint": item["fingerprint"], "expected_policy_fingerprint": v["fingerprint"]})
	path := fmt.Sprintf("/actions/%s/release", id)
	risk177Data(t, f.riskPath("POST", path, string(body)))
	if got := f.riskPath("POST", path, string(body)); got.Code != 409 {
		t.Fatal("stale release did not conflict", got.Code)
	}
	p := f.pay(t, "risk177-action-after-release", "192.0.2.177")
	if p["code"] != float64(1) || creates.Load() != 2 {
		t.Fatal("CAS release did not actually restore new payment", p["code"], creates.Load())
	}
	if _, e := f.s.ProviderFlow.Query(context.Background(), 1, first["trade_no"].(string)); e != nil {
		t.Fatal(e)
	}
}

func TestRisk177ReleaseNeverReopensManualMerchantPayPermission(t *testing.T) {
	f, _, creates := risk177Flow(t)
	ctx := context.Background()
	v := risk177Data(t, f.request("GET", "", f.csrf))
	v = risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) {
		s["auto_check_sucrate"] = "1"
		s["check_sucrate_second"] = "3600"
		s["check_sucrate_count"] = "1"
		s["check_sucrate_value"] = "50"
	}), f.csrf))
	original := f.pay(t, "risk177-pay-manual-first", "192.0.2.177")
	if original["code"] != float64(1) {
		t.Fatal(original)
	}
	f.check(t, v, "risk177-pay-manual-check")
	rows := risk177Data(t, f.riskPath("GET", "/actions", ""))["items"].([]any)
	if len(rows) != 1 {
		t.Fatal("merchant block missing")
	}
	a := rows[0].(map[string]any)
	if _, e := f.s.Pool.Exec(ctx, `UPDATE merchants SET pay=0 WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]any{"expected_fingerprint": a["fingerprint"], "expected_policy_fingerprint": v["fingerprint"]})
	risk177Data(t, f.riskPath("POST", "/actions/"+a["id"].(string)+"/release", string(body)))
	var pay int
	if e := f.s.Pool.QueryRow(ctx, `SELECT pay FROM merchants WHERE id=1`).Scan(&pay); e != nil || pay != 0 {
		t.Fatal("risk release reopened manual merchant permission", pay, e)
	}
	if out := f.pay(t, "risk177-pay-remains-off", "192.0.2.178"); out["code"] == float64(1) || creates.Load() != 1 {
		t.Fatal("new payment bypassed manual closed pay")
	}
	if _, e := f.s.ProviderFlow.Query(ctx, 1, original["trade_no"].(string)); e != nil {
		t.Fatal("original confirmed query was blocked by manual pay/risk release", e)
	}
}
