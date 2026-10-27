package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func merchantScan192Request(f buyer104MerchantFixture, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/merchant/scanpay"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.merchantCSRF)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.merchantToken})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func TestMerchantScan192CapabilitiesAreOwnReadOnlyAndDoNotInventScanSupport(t *testing.T) {
	f := buyer104MerchantLocal(t)
	w := merchantScan192Request(f, "GET", "/capabilities", "")
	if w.Code != 200 {
		t.Fatal("merchant scan capability entry missing", w.Code)
	}
	var body struct {
		Code int
		Data struct {
			CanPrepare bool     `json:"can_prepare"`
			Types      []string `json:"pay_types"`
			Maximum    string   `json:"max_input_money"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Code != 0 || body.Data.CanPrepare || body.Data.Types == nil || len(body.Data.Types) != 0 || body.Data.Maximum != "999999.99" {
		t.Fatal("native-only provider advertised scan or keypad limit changed", body, err)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cashier capability cached")
	}
	for _, private := range []string{f.token, f.csrf, f.merchantToken, f.merchantCSRF, "test-secret", "proof_hash"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("cashier leaked private authority")
		}
	}
	for _, path := range []string{"/capabilities?", "/capabilities?merchant_id=2"} {
		if w = merchantScan192Request(f, "GET", path, ""); w.Code != 400 {
			t.Fatal("capability accepted query authority", path, w.Code)
		}
	}
	if w = merchantScan192Request(f, "POST", "/capabilities", "{}"); w.Code != 405 {
		t.Fatal("capability accepted mutation method", w.Code)
	}
	var effects int
	if err := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM orders)+(SELECT count(*)FROM provider_attempts)+(SELECT count(*)FROM journals)+(SELECT count(*)FROM jobs)`).Scan(&effects); err != nil || effects != 0 {
		t.Fatal("capability read created money/attempt/job", effects, err)
	}
}

func merchantScanGate192Request(f channel168Fixture, method, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/settings/scanpay", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func TestMerchantScan192OriginalPlatformDefaultCASAndMissingSourceCannotReopen(t *testing.T) {
	f := channel168Local(t)
	w := merchantScanGate192Request(f, "GET", "")
	if w.Code != 200 {
		t.Fatal("original platform scanpay setting missing", w.Code)
	}
	first := channel168Data(t, w)
	if first["scanpay_open"] != "1" || first["configured"] != false || first["can_update"] != true {
		t.Fatal("original SQL default1 not preserved", first)
	}
	input, _ := json.Marshal(map[string]any{"scanpay_open": "0", "expected_fingerprint": first["fingerprint"]})
	w = merchantScanGate192Request(f, "PATCH", string(input))
	if w.Code != 200 {
		t.Fatal("scanpay switch not saved", w.Code)
	}
	closed := channel168Data(t, w)
	if closed["scanpay_open"] != "0" || closed["configured"] != true || closed["fingerprint"] == first["fingerprint"] {
		t.Fatal("saved gate not current", closed)
	}
	if w = merchantScanGate192Request(f, "PATCH", string(input)); w.Code != 409 {
		t.Fatal("stale original gate confirmation accepted", w.Code)
	}
	// Simulate privileged source corruption only in this isolated test schema.
	if _, err := f.p.Exec(t.Context(), `ALTER TABLE merchant_scan_policy_head106 DISABLE TRIGGER USER; DELETE FROM merchant_scan_policy_head106; ALTER TABLE merchant_scan_policy_head106 ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if w = merchantScanGate192Request(f, "GET", ""); w.Code != 503 {
		t.Fatal("lost source fell back to original open default", w.Code)
	}
	var effects int
	if err := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM orders)+(SELECT count(*)FROM provider_attempts)+(SELECT count(*)FROM journals)+(SELECT count(*)FROM jobs)`).Scan(&effects); err != nil || effects != 0 {
		t.Fatal("platform config wrote money/attempt/job", effects, err)
	}
}

func TestMerchantScan192ActualRegisteredSDKVisibilityFollowsGateAndCurrentGrantWithoutIO(t *testing.T) {
	setup := parent180RuntimeReady(t)
	body, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "account": map[string]any{"config": map[string]any{"enable_scan_pay": map[string]string{"operation": "replace", "value": "true"}}}})
	if w := parent180HTTP(setup.f, "PATCH", "/"+setup.id, string(body)); w.Code != 200 {
		t.Fatal("local registered SDK scan setup", w.Code)
	}
	f := buyer190MerchantOn(t, setup.f)
	read := func() map[string]any {
		t.Helper()
		w := merchantScan192Request(f, "GET", "/capabilities", "")
		if w.Code != 200 {
			t.Fatal("actual scan capability unavailable", w.Code)
		}
		return channel168Data(t, w)
	}
	first := read()
	types, ok := first["pay_types"].([]any)
	if first["can_prepare"] != true || !ok || len(types) != 1 || types[0] != "wxpay" {
		t.Fatal("registered allowed scan SDK not visible", first)
	}
	state := channel168Data(t, merchantScanGate192Request(setup.f, "GET", ""))
	change, _ := json.Marshal(map[string]any{"scanpay_open": "0", "expected_fingerprint": state["fingerprint"]})
	closed := channel168Data(t, merchantScanGate192Request(setup.f, "PATCH", string(change)))
	if v := read(); v["platform_open"] != false || v["can_prepare"] != false || len(v["pay_types"].([]any)) != 0 {
		t.Fatal("closed original switch still advertised admission", v)
	}
	change, _ = json.Marshal(map[string]any{"scanpay_open": "1", "expected_fingerprint": closed["fingerprint"]})
	channel168Data(t, merchantScanGate192Request(setup.f, "PATCH", string(change)))
	if v := read(); v["can_prepare"] != true {
		t.Fatal("explicit valid reopen not reflected", v)
	}
	setup.f.s.MerchantChannels[1] = nil
	if v := read(); v["can_prepare"] != false || len(v["pay_types"].([]any)) != 0 {
		t.Fatal("read adopted a removed operator grant", v)
	}
	if setup.creates.Load() != 0 || setup.queries.Load() != 0 || setup.refunds.Load() != 0 || setup.bad.Load() != 0 {
		t.Fatal("capability inspection reached upstream", setup.creates.Load(), setup.queries.Load(), setup.refunds.Load(), setup.bad.Load())
	}
}

func TestMerchantScan192ValidOldPolicyAndAuditRollbackCannotRestoreOpen(t *testing.T) {
	f := channel168Local(t)
	state := channel168Data(t, merchantScanGate192Request(f, "GET", ""))
	save := func(value string, previous map[string]any) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"scanpay_open": value, "expected_fingerprint": previous["fingerprint"]})
		return channel168Data(t, merchantScanGate192Request(f, "PATCH", string(body)))
	}
	opened := save("1", state)
	var oldBody []byte
	var oldTime time.Time
	var id int64
	if err := f.p.QueryRow(t.Context(), `SELECT id,body,updated_at FROM backoffice_records WHERE resource='settings' AND body->>'key'='scanpay_open'`).Scan(&id, &oldBody, &oldTime); err != nil {
		t.Fatal(err)
	}
	save("0", opened)
	// Restore a genuine old source/head/raw value and roll back its normal audit.
	// The separately immutable latest witness must still prevent open downgrade.
	if _, err := f.p.Exec(t.Context(), `ALTER TABLE merchant_scan_policies106 DISABLE TRIGGER ALL;DELETE FROM merchant_scan_policies106 WHERE revision=2;ALTER TABLE merchant_scan_policies106 ENABLE TRIGGER ALL;UPDATE merchant_scan_policy_head106 SET revision=1;ALTER TABLE backoffice_audit DISABLE TRIGGER USER;DELETE FROM backoffice_audit WHERE resource='merchant-scan-policy106' AND resource_id=2;ALTER TABLE backoffice_audit ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=$2::jsonb,updated_at=$3 WHERE id=$1`, id, string(oldBody), oldTime); err != nil {
		t.Fatal(err)
	}
	if w := merchantScanGate192Request(f, "GET", ""); w.Code != 503 {
		t.Fatal("valid old policy + ordinary audit rollback restored open", w.Code)
	}
}

func TestMerchantScan192GenericSettingsCannotBypassProtectedOriginalSwitch(t *testing.T) {
	f := channel168Local(t)
	first := channel168Data(t, merchantScanGate192Request(f, "GET", ""))
	body, _ := json.Marshal(map[string]any{"scanpay_open": "0", "expected_fingerprint": first["fingerprint"]})
	channel168Data(t, merchantScanGate192Request(f, "PATCH", string(body)))
	var id string
	if err := f.p.QueryRow(t.Context(), `SELECT id::text FROM backoffice_records WHERE resource='settings' AND body->>'key'='scanpay_open'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"PUT", "DELETE"} {
		r := httptest.NewRequest(method, "/console/v1/admin/resources/settings/"+id, strings.NewReader(`{"value":"1"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("generic resource bypassed protected gate", method, w.Code)
		}
	}
	if v := channel168Data(t, merchantScanGate192Request(f, "GET", "")); v["scanpay_open"] != "0" {
		t.Fatal("generic attempt changed effective original switch", v)
	}
}
