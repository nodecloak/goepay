package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/payment"
)

// Exercise the durable store boundary directly as well as the public route, so
// a schema rejection reports its original error rather than only HTTP503.
func TestNativeScan202StoreExplicitAuthorizationPersistsCurrentBinding(t *testing.T) {
	f := channel168Local(t)
	session, ok := f.s.Sessions.Get(f.token, "admin")
	if !ok {
		t.Fatal("native settings fixture lost original admin session")
	}
	actor := payment.ParentChannelActor{User: session.User, Token: f.token, CSRF: f.csrf}
	before, err := f.s.Store.ScanJSSettings202(t.Context(), actor)
	if err != nil {
		t.Fatalf("native settings initial durable read: %T %v", err, err)
	}
	binding := payment.ScanJSBinding202{ProfileID: 202, ProfileRevision: 1, ProfileFingerprint: strings.Repeat("a", 64)}
	after, err := f.s.Store.UpdateScanJSSettings202(t.Context(), actor, true, binding, before.Fingerprint)
	if err != nil {
		t.Fatalf("native settings first explicit durable save: %T %v", err, err)
	}
	if !after.Enabled || !after.Configured || after.Revision != "1" || after.Binding != binding || after.Fingerprint == before.Fingerprint {
		t.Fatal("native settings first durable save lost its explicitly confirmed binding", after)
	}
	reread, err := f.s.Store.ScanJSSettings202(t.Context(), actor)
	if err != nil || reread.Fingerprint != after.Fingerprint || reread.Binding != binding {
		t.Fatal("native settings durable reread changed confirmed binding", reread, err)
	}
}

func scanJSSettingsRequest202(f channel168Fixture, method, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/settings/scanpay-js", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

// This catches a missing independent gate or any implicit adoption of the
// existing payment/OAuth authorization. It only uses public HTTP boundaries.
func TestNativeScan202IndependentSettingsDefaultDisabled(t *testing.T) {
	f := channel168Local(t)
	w := scanJSSettingsRequest202(f, "GET", "")
	if w.Code != 200 {
		t.Fatalf("native scan explicit authorization settings HTTP=%d want200", w.Code)
	}
	v := channel168Data(t, w)
	if v["enabled"] != false || v["configured"] != false || v["profile_id"] != "" || v["profile_revision"] != "" || v["can_update"] != true {
		t.Fatal("native scanning inherited authorization instead of remaining explicitly disabled", v)
	}
	if _, ok := v["profiles"].([]any); !ok {
		t.Fatal("native scan selection catalogue must be an explicit array", v)
	}
	fingerprint, ok := v["fingerprint"].(string)
	if !ok || len(fingerprint) != 64 {
		t.Fatal("native scan settings missing current confirmation fingerprint", v)
	}
	body, _ := json.Marshal(map[string]any{"enabled": true, "profile_id": "7", "profile_revision": "1", "expected_fingerprint": fingerprint})
	if w = scanJSSettingsRequest202(f, "PATCH", string(body)); w.Code != 422 {
		t.Fatal("enabled native scan accepted an unconfigured application", w.Code)
	}
}

func TestNativeScan202DisabledConfigDoesNotCreateFinancialEffects(t *testing.T) {
	f := buyer104MerchantLocal(t)
	w := merchantScan192Request(f, "POST", "/jsconfig", `{"url":"https://merchant.example.invalid/merchant/scanpay"}`)
	if w.Code != 503 {
		t.Fatalf("disabled native scan configuration HTTP=%d want503", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("native scan config response can be cached")
	}
	var effects int
	if err := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM orders)+(SELECT count(*)FROM provider_attempts)+(SELECT count(*)FROM journals)+(SELECT count(*)FROM jobs)`).Scan(&effects); err != nil || effects != 0 {
		t.Fatal("native config created money/attempt/job", effects, err)
	}
	for _, private := range []string{f.token, f.csrf, f.merchantToken, f.merchantCSRF, "test-secret", "access_token", "jsapi_ticket", "appsecret"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("native configuration leaked private authority", private)
		}
	}
}

func TestNativeScan202ConfigurationUsesPostAndCSRF(t *testing.T) {
	f := buyer104MerchantLocal(t)
	if w := merchantScan192Request(f, "GET", "/jsconfig", ""); w.Code != 405 {
		t.Fatal("native config did not require POST", w.Code)
	}
	r := httptest.NewRequest("POST", "/console/v1/merchant/scanpay/jsconfig", strings.NewReader(`{"url":"https://merchant.example.invalid/merchant/scanpay"}`))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.merchantToken})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("native config accepted absent CSRF", w.Code)
	}
}

type nativeSigner202 struct {
	calls  atomic.Int32
	page   string
	during func()
}

func (s *nativeSigner202) Sign(_ context.Context, page string) (adapter.WechatJSConfig202, error) {
	s.calls.Add(1)
	s.page = page
	if s.during != nil {
		s.during()
	}
	return adapter.WechatJSConfig202{AppID: "wx-native202", Timestamp: 1793000000, NonceStr: "native202-nonce", Signature: strings.Repeat("a", 40), JSAPIList: []string{"scanQRCode"}}, nil
}
func nativeScanReady202(t *testing.T) (buyer104MerchantFixture, *nativeSigner202) {
	t.Helper()
	_, f := merchantScan192Ready(t)
	var channel int64
	for _, id := range f.s.MerchantChannels[1] {
		if p := f.s.ProviderFlow.Providers[id]; p != nil && p.ID() == "wxpay" {
			channel = id
			break
		}
	}
	if channel <= 0 {
		t.Fatal("native scan fixture missing current wxpay grant")
	}
	signer := &nativeSigner202{}
	f.s.ScanJSSiteOrigin202 = "https://cashier.example.invalid"
	f.s.ScanJSProfiles202 = map[int64]channelconfig.ScanJSProfile202{202: {Profile: channelconfig.IdentityProfile{ID: 202, Revision: 1, ChannelID: channel, Provider: "wxpay", Kind: "web", AppID: "wx-native202", MerchantIDs: []int64{1}, Fingerprint: strings.Repeat("a", 64)}, Signer: signer}}
	state := channel168Data(t, scanJSSettingsRequest202(f.channel168Fixture, "GET", ""))
	body, _ := json.Marshal(map[string]any{"enabled": true, "profile_id": "202", "profile_revision": "1", "expected_fingerprint": state["fingerprint"]})
	if w := scanJSSettingsRequest202(f.channel168Fixture, "PATCH", string(body)); w.Code != 200 {
		t.Fatal("explicit native scan fixture authorization failed", w.Code, w.Body.String())
	}
	return f, signer
}

func TestNativeScan202CurrentExplicitProfileSignsOnlyConfiguredCashierURLWithoutFinancialEffects(t *testing.T) {
	f, signer := nativeScanReady202(t)
	page := "https://cashier.example.invalid/merchant/scanpay?terminal=1"
	body, _ := json.Marshal(map[string]string{"url": page})
	w := merchantScan192Request(f, "POST", "/jsconfig", string(body))
	if w.Code != 200 || signer.calls.Load() != 1 || signer.page != page {
		t.Fatal("explicit native config unavailable", w.Code, signer.calls.Load())
	}
	var reply struct{ Data adapter.WechatJSConfig202 }
	if json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Data.AppID != "wx-native202" || len(reply.Data.JSAPIList) != 1 || reply.Data.JSAPIList[0] != "scanQRCode" {
		t.Fatal("native config lost original SDK contract", w.Body.String())
	}
	for _, page := range []string{"https://other.example.invalid/merchant/scanpay", "https://cashier.example.invalid.evil.invalid/merchant/scanpay", "https://user@cashier.example.invalid/merchant/scanpay", "https://cashier.example.invalid/merchant/scanpay#fragment", "https://cashier.example.invalid/merchant/scanpay#", "https://cashier.example.invalid/merchant/orders", "https://cashier.example.invalid/merchant/%73canpay", "https://cashier.example.invalid/merchant/scanpay?x=%0d%0a", "http://cashier.example.invalid/merchant/scanpay"} {
		body, _ := json.Marshal(map[string]string{"url": page})
		if w = merchantScan192Request(f, "POST", "/jsconfig", string(body)); w.Code != 422 {
			t.Fatal("native scan signed outside configured cashier URL", page, w.Code)
		}
	}
	for _, body := range []string{`{"url":"https://cashier.example.invalid/merchant/scanpay","url":"https://other.invalid/merchant/scanpay"}`, `{"url":null}`, `{"url":"https://cashier.example.invalid/merchant/scanpay","merchant_id":"2"}`, `{"url":"https://cashier.example.invalid/merchant/scanpay","auth_code":"134567890123456789"}`} {
		if w = merchantScan192Request(f, "POST", "/jsconfig", body); w.Code != 400 {
			t.Fatal("ambiguous native authority accepted", w.Code)
		}
	}
	if signer.calls.Load() != 1 {
		t.Fatal("invalid input reached signer", signer.calls.Load())
	}
	var effects int
	if err := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM orders)+(SELECT count(*)FROM provider_attempts)+(SELECT count(*)FROM journals)+(SELECT count(*)FROM jobs)`).Scan(&effects); err != nil || effects != 0 {
		t.Fatal("native SDK config created financial effects", effects, err)
	}
}

func TestNativeScan202RotationRequiresFreshSelectionAndSignedSettingsRejectTampering(t *testing.T) {
	f, signer := nativeScanReady202(t)
	old := channel168Data(t, scanJSSettingsRequest202(f.channel168Fixture, "GET", ""))
	p := f.s.ScanJSProfiles202[202]
	p.Profile.Fingerprint = strings.Repeat("b", 64)
	f.s.ScanJSProfiles202[202] = p
	if w := merchantScan192Request(f, "POST", "/jsconfig", `{"url":"https://cashier.example.invalid/merchant/scanpay"}`); w.Code != 503 || signer.calls.Load() != 0 {
		t.Fatal("rotated secret silently inherited old authorization", w.Code)
	}
	current := channel168Data(t, scanJSSettingsRequest202(f.channel168Fixture, "GET", ""))
	if current["available"] != false || current["fingerprint"] == old["fingerprint"] {
		t.Fatal("rotation did not require fresh selection", current)
	}
	body, _ := json.Marshal(map[string]any{"enabled": true, "profile_id": "202", "profile_revision": "1", "expected_fingerprint": old["fingerprint"]})
	if w := scanJSSettingsRequest202(f.channel168Fixture, "PATCH", string(body)); w.Code != 409 {
		t.Fatal("stale catalogue confirmation accepted", w.Code)
	}
	body, _ = json.Marshal(map[string]any{"enabled": true, "profile_id": "202", "profile_revision": "1", "expected_fingerprint": current["fingerprint"]})
	if w := scanJSSettingsRequest202(f.channel168Fixture, "PATCH", string(body)); w.Code != 200 {
		t.Fatal("fresh explicit rotated selection rejected", w.Code)
	}
	if _, err := f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=jsonb_set(body,'{facts,Binding,ProfileFingerprint}',to_jsonb(repeat('c',64)))WHERE resource='scan-js-settings202'`); err != nil {
		t.Fatal(err)
	}
	if w := merchantScan192Request(f, "POST", "/jsconfig", `{"url":"https://cashier.example.invalid/merchant/scanpay"}`); w.Code == 200 || signer.calls.Load() != 0 {
		t.Fatal("tampered native binding signed page", w.Code)
	}
}

func TestNativeScan202RevokedGrantAndExpiryDuringSigningCannotReturnConfiguration(t *testing.T) {
	t.Run("grant", func(t *testing.T) {
		f, signer := nativeScanReady202(t)
		f.s.MerchantChannels[1] = nil
		if w := merchantScan192Request(f, "POST", "/jsconfig", `{"url":"https://cashier.example.invalid/merchant/scanpay"}`); w.Code != 503 || signer.calls.Load() != 0 {
			t.Fatal("removed merchant grant reached signer", w.Code)
		}
	})
	t.Run("final-session", func(t *testing.T) {
		f, signer := nativeScanReady202(t)
		signer.during = func() {
			if _, err := f.p.Exec(t.Context(), `UPDATE console_sessions SET expires_at=clock_timestamp()-interval '1 second'WHERE username='buyer104-merchant'`); err != nil {
				t.Fatal(err)
			}
		}
		w := merchantScan192Request(f, "POST", "/jsconfig", `{"url":"https://cashier.example.invalid/merchant/scanpay"}`)
		if w.Code != 403 || strings.Contains(w.Body.String(), "signature") {
			t.Fatal("expired session received native signature", w.Code, w.Body.String())
		}
	})
}
