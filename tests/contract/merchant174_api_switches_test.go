package contract_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func apiSwitchState174(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("personal API switches HTTP=%d want200", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("personal API policy was cacheable")
	}
	v := credentialData162(t, w)
	if len(v) != 9 || v["merchant_id"] != "2" || v["can_update"] != true {
		t.Fatal("personal API policy identity/shape", v)
	}
	if fp, ok := v["fingerprint"].(string); !ok || len(fp) != 64 {
		t.Fatal("missing independent fingerprint")
	}
	return v
}
func apiSwitchBody174(fp string, keylogin, refund, transfer int) string {
	raw, _ := json.Marshal(map[string]any{"keylogin": keylogin, "refund": refund, "transfer": transfer, "expected_fingerprint": fp})
	return string(raw)
}
func TestMerchant174OriginalSwitchDefaultsCASAndNoGrantEscalation(t *testing.T) {
	f := signatureFixture(t, 2)
	ctx := context.Background()
	if _, e := f.p.Exec(ctx, `UPDATE merchants SET profile=profile||'{"qq":"123456","pay_minmoney":"1.00","pay_maxmoney":"9.00","unrelated174":"keep"}'::jsonb WHERE id=2`); e != nil {
		t.Fatal(e)
	}
	before, e := f.s.Store.Merchant(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	var profile string
	if e = f.p.QueryRow(ctx, `SELECT profile::text FROM merchants WHERE id=2`).Scan(&profile); e != nil {
		t.Fatal(e)
	}
	state := apiSwitchState174(t, f.request("GET", "api-switches", "", f.csrf))
	if state["keylogin"] != float64(1) || state["refund"] != float64(1) || state["transfer"] != float64(0) || state["configured"] != false || state["revision"] != "0" || state["transfer_granted"] != false {
		t.Fatal("original defaults changed", state)
	}
	body := apiSwitchBody174(state["fingerprint"].(string), 0, 0, 1)
	after := apiSwitchState174(t, f.request("PATCH", "api-switches", body, f.csrf))
	if after["keylogin"] != float64(0) || after["refund"] != float64(0) || after["transfer"] != float64(1) || after["transfer_granted"] != false || after["revision"] != "1" {
		t.Fatal("personal preference did not preserve independent administrator grant", after)
	}
	if w := f.request("PATCH", "api-switches", body, f.csrf); w.Code != 409 {
		t.Fatal("stale personal CAS accepted", w.Code)
	}
	var saved string
	if e = f.p.QueryRow(ctx, `SELECT profile::text FROM merchants WHERE id=2`).Scan(&saved); e != nil || saved != profile {
		t.Fatal("API switches changed 172/profile/admin fields", e)
	}
	m, e := f.s.Store.Merchant(ctx, 2)
	if e != nil || m.APIKey != before.APIKey || m.RSAPublicKey != before.RSAPublicKey || m.Keytype != nil || m.SignatureAlgorithm != before.SignatureAlgorithm {
		t.Fatal("API switch edit changed payment credentials", e)
	}
}
func TestMerchant174KeyLoginIssuesOnlyOriginalPersistentMerchantSession(t *testing.T) {
	f := signatureFixture(t, 2)
	m, e := f.s.Store.Merchant(context.Background(), 2)
	if e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]string{"merchant_id": "2", "api_key": m.APIKey, "code": ""})
	r := httptest.NewRequest("POST", "/console/v1/merchant/key-login", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("valid enabled original key login HTTP=%d want200", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), m.APIKey) {
		t.Fatal("key login leaked/cached a credential")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "goepay_merchant" || !cookies[0].HttpOnly {
		t.Fatal("merchant login cookie missing")
	}
	session, ok := f.s.Sessions.Get(cookies[0].Value, "merchant")
	if !ok || session.User.ID != 2 || session.User.Username != "merchant" || session.Issuer != nil {
		t.Fatal("key login did not create durable original owner session")
	}
}
