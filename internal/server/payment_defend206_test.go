package server

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/paymentdefend"
)

const paymentDefend206Path = "/console/v1/admin/settings/payment-defend"

func paymentDefend206Fixture(t *testing.T) channel168Fixture {
	t.Helper()
	f := channel168Local(t)
	var err error
	f.s.PaymentDefend206, err = paymentdefend.NewStore(f.p, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

type paymentDefend206View struct {
	Settings    map[string]string `json:"settings"`
	Configured  bool              `json:"configured"`
	Revision    string            `json:"revision"`
	Fingerprint string            `json:"fingerprint"`
	CanUpdate   bool              `json:"can_update"`
}

func paymentDefend206Request(f channel168Fixture, method, suffix, body, token, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, paymentDefend206Path+suffix, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.206:5206"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func paymentDefend206Read(t *testing.T, f channel168Fixture) paymentDefend206View {
	t.Helper()
	w := paymentDefend206Request(f, http.MethodGet, "", "", f.token, f.csrf)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("payment-defend policy requires a private successful GET: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Code int                        `json:"code"`
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || envelope.Code != 0 || len(envelope.Data) != 5 {
		t.Fatalf("payment-defend GET did not return the policy contract: body=%s err=%v", w.Body.String(), err)
	}
	for _, key := range []string{"settings", "configured", "revision", "fingerprint", "can_update"} {
		if _, ok := envelope.Data[key]; !ok {
			t.Fatalf("payment-defend GET omitted %q", key)
		}
	}
	var rawSettings map[string]any
	if err := json.Unmarshal(envelope.Data["settings"], &rawSettings); err != nil || len(rawSettings) != 7 {
		t.Fatal("payment-defend must expose exactly seven original settings", err)
	}
	for _, key := range []string{"pay_verify", "pay_verify_type", "pay_verify_check_second", "pay_verify_check_count", "pay_verify_check_rate", "pay_verify_check_ip", "pay_verify_check_uid"} {
		if _, ok := rawSettings[key].(string); !ok {
			t.Fatalf("payment-defend setting %q must be an explicit string", key)
		}
	}
	var view paymentDefend206View
	raw, err := json.Marshal(envelope.Data)
	if err != nil || json.Unmarshal(raw, &view) != nil {
		t.Fatal("invalid payment-defend public policy types", err)
	}
	revision, err := strconv.ParseUint(view.Revision, 10, 64)
	if err != nil || strconv.FormatUint(revision, 10) != view.Revision || len(view.Fingerprint) != 64 || strings.Trim(view.Fingerprint, "0123456789abcdef") != "" {
		t.Fatal("payment-defend policy lacks a canonical revision and protected fingerprint")
	}
	for _, secret := range []string{"proof_hash", "captcha_key", "api_key", "private_key", f.token, f.csrf} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("payment-defend policy leaked private authority or proof material", secret)
		}
	}
	return view
}

func paymentDefend206Body(t *testing.T, view paymentDefend206View, enabled bool) string {
	t.Helper()
	settings := make(map[string]string, len(view.Settings))
	for key, value := range view.Settings {
		settings[key] = value
	}
	if enabled {
		settings["pay_verify"], settings["pay_verify_type"] = "3", "3"
	}
	raw, err := json.Marshal(map[string]any{"settings": settings, "expected_fingerprint": view.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func paymentDefend206Enable(t *testing.T, f channel168Fixture) paymentDefend206View {
	t.Helper()
	before := paymentDefend206Read(t, f)
	w := paymentDefend206Request(f, http.MethodPatch, "", paymentDefend206Body(t, before, true), f.token, f.csrf)
	var result struct {
		Code int `json:"code"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Code != 0 {
		t.Fatalf("legal payment-defend 3/3 policy was not saved via HTTP: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	after := paymentDefend206Read(t, f)
	if !after.Configured || !after.CanUpdate || after.Settings["pay_verify"] != "3" || after.Settings["pay_verify_type"] != "3" || after.Fingerprint == before.Fingerprint || after.Revision == before.Revision {
		t.Fatal("payment-defend save did not commit the original policy with a new protected revision")
	}
	return after
}

func TestPaymentDefend206ActualHTTPUnconfiguredPolicyIsExplicitlyOff(t *testing.T) {
	f := paymentDefend206Fixture(t)
	view := paymentDefend206Read(t, f)
	if view.Configured || view.Revision != "0" || !view.CanUpdate || view.Settings["pay_verify"] != "0" || view.Settings["pay_verify_type"] != "0" {
		t.Fatal("unconfigured payment-defend policy did not explicitly remain off", view)
	}
	// Other string defaults are intentionally not inferred from encrypted Pro
	// control flow or imported descriptive configuration.
}

func TestPaymentDefend206ActualHTTPLegalSaveAndStaleCAS(t *testing.T) {
	f := paymentDefend206Fixture(t)
	before := paymentDefend206Read(t, f)
	staleBody := paymentDefend206Body(t, before, true)
	after := paymentDefend206Enable(t, f)
	if w := paymentDefend206Request(f, http.MethodPatch, "", staleBody, f.token, f.csrf); w.Code != http.StatusConflict {
		t.Fatalf("stale payment-defend fingerprint accepted: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	if current := paymentDefend206Read(t, f); !reflect.DeepEqual(current, after) {
		t.Fatal("stale CAS changed the committed payment-defend policy")
	}
}

func TestPaymentDefend206ActualHTTPRequiresOriginalPersistentSuperAdministrator(t *testing.T) {
	for _, source := range []string{"anonymous", "operator", "memory-super", "merchant-token"} {
		t.Run(source, func(t *testing.T) {
			f := paymentDefend206Fixture(t)
			before := paymentDefend206Read(t, f)
			body := paymentDefend206Body(t, before, true)
			token, csrf, want := f.token, f.csrf, http.StatusForbidden
			persistent := f.s.Sessions
			switch source {
			case "anonymous":
				token, csrf, want = "", "", http.StatusUnauthorized
			case "operator":
				if _, err := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('defend206-operator','admin',206,'Synthetic Operator206','unused','operator')`); err != nil {
					t.Fatal(err)
				}
				var err error
				token, csrf, err = persistent.Create(identity.User{ID: 206, Role: "admin", Username: "defend206-operator", AdminClass: "operator", AuthVersion: 1})
				if err != nil {
					t.Fatal(err)
				}
			case "memory-super":
				f.s.Sessions = identity.NewSessions(false)
				var err error
				token, csrf, err = f.s.Sessions.Create(identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1})
				if err != nil {
					t.Fatal(err)
				}
			case "merchant-token":
				merchant := buyer190MerchantOn(t, f)
				token, csrf, want = merchant.merchantToken, merchant.merchantCSRF, http.StatusUnauthorized
			}
			for _, method := range []string{http.MethodGet, http.MethodPatch} {
				if w := paymentDefend206Request(f, method, "", body, token, csrf); w.Code != want {
					t.Errorf("non-original policy authority %s/%s: HTTP=%d want%d", source, method, w.Code, want)
				}
			}
			f.s.Sessions = persistent
			if current := paymentDefend206Read(t, f); !reflect.DeepEqual(current, before) {
				t.Fatal("rejected authority changed payment-defend policy")
			}
		})
	}
}

func TestPaymentDefend206ActualHTTPMissingOrWrongCSRFCannotSave(t *testing.T) {
	f := paymentDefend206Fixture(t)
	before := paymentDefend206Read(t, f)
	body := paymentDefend206Body(t, before, true)
	for _, csrf := range []string{"", "not-the-persistent-csrf206"} {
		if w := paymentDefend206Request(f, http.MethodPatch, "", body, f.token, csrf); w.Code != http.StatusForbidden {
			t.Errorf("invalid payment-defend CSRF accepted: HTTP=%d", w.Code)
		}
	}
	if current := paymentDefend206Read(t, f); !reflect.DeepEqual(current, before) {
		t.Fatal("rejected CSRF changed payment-defend policy")
	}
}

func TestPaymentDefend206ActualHTTPRejectsUnknownFieldsQueriesAndMethods(t *testing.T) {
	f := paymentDefend206Fixture(t)
	before := paymentDefend206Read(t, f)
	body := paymentDefend206Body(t, before, true)
	for _, nested := range []bool{false, true} {
		var candidate map[string]any
		if err := json.Unmarshal([]byte(body), &candidate); err != nil {
			t.Fatal(err)
		}
		if nested {
			candidate["settings"].(map[string]any)["captcha_key"] = "unsupported-private-field206"
		} else {
			candidate["merchant_id"] = "2"
		}
		raw, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if w := paymentDefend206Request(f, http.MethodPatch, "", string(raw), f.token, f.csrf); w.Code != http.StatusBadRequest {
			t.Errorf("unknown payment-defend field accepted (nested=%v): HTTP=%d", nested, w.Code)
		}
	}
	for _, suffix := range []string{"?", "?uid=1"} {
		for _, method := range []string{http.MethodGet, http.MethodPatch} {
			if w := paymentDefend206Request(f, method, suffix, body, f.token, f.csrf); w.Code != http.StatusBadRequest {
				t.Errorf("payment-defend query context accepted: %s%s HTTP=%d", method, suffix, w.Code)
			}
		}
	}
	if w := paymentDefend206Request(f, http.MethodPost, "", body, f.token, f.csrf); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("unsupported payment-defend method accepted: HTTP=%d", w.Code)
	}
	if current := paymentDefend206Read(t, f); !reflect.DeepEqual(current, before) {
		t.Fatal("invalid input changed payment-defend policy")
	}
}

func paymentDefend206WireFixture(t *testing.T, rsaMode bool) (channel168Fixture, *rsa.PrivateKey) {
	t.Helper()
	f := paymentDefend206Fixture(t)
	f.s.Sandbox, f.s.ProviderFlow.LocalSimulation = true, true
	if !rsaMode {
		return f, nil
	}
	public, private, err := generateMerchantRSA()
	if err != nil {
		t.Fatal(err)
	}
	key, err := compat.ParsePrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.p.Exec(t.Context(), `UPDATE merchants SET rsa_public_key=$1,signature_algorithm='RSA',keytype=1 WHERE id=1`, public); err != nil {
		t.Fatal(err)
	}
	f.s.PlatformKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return f, key
}

func paymentDefend206Wire(t *testing.T, f channel168Fixture, key *rsa.PrivateKey, path, original string) *httptest.ResponseRecorder {
	t.Helper()
	p := compat.Params{"pid": "1", "out_trade_no": original, "type": "alipay", "name": "Synthetic payment defend206", "money": "1.23", "channel_id": "7", "method": "native", "clientip": "192.0.2.206", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
	if key == nil {
		p["sign"] = compat.SignMD5WithProfile(p, "test-secret", compat.ProfileProServer)
	} else {
		p["sign_type"], p["timestamp"] = "RSA", strconv.FormatInt(time.Now().Unix(), 10)
		var err error
		p["sign"], err = compat.SignRSAWithProfile(p, key, compat.ProfileProServer)
		if err != nil || !compat.VerifyRSAWithProfile(p, p["sign"], &key.PublicKey, compat.ProfileProServer) {
			t.Fatal("fixture did not create a valid original RSA request", err)
		}
	}
	values := url.Values{}
	for k, value := range p {
		values.Set(k, value)
	}
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	r.RemoteAddr = "192.0.2.206:5206"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func paymentDefend206Effects(t *testing.T, f channel168Fixture) string {
	t.Helper()
	var out string
	if err := f.p.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'orders',(SELECT count(*) FROM orders),'attempts',(SELECT count(*) FROM provider_attempts),
 'jobs',(SELECT count(*) FROM jobs),'operations',(SELECT count(*) FROM provider_operations),
 'journals',(SELECT count(*) FROM journals),'refunds',(SELECT count(*) FROM refunds),
 'routes',(SELECT count(*) FROM provider_route_assignments),'quotes',(SELECT count(*) FROM provider_route_fee_quotes),
 'terms',(SELECT count(*) FROM order_route_fee_terms),
 'route_counters',(SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY rule_id),'[]'::jsonb) FROM provider_route_counters c),
 'group_counters',(SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY group_id),'[]'::jsonb) FROM group_route_counters c),
 'last_used',(SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY channel_id),'[]'::jsonb) FROM provider_channel_last_used c)
 )::text`).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPaymentDefend206OffBaselineOriginalMD5AndRSARoutesReallySend(t *testing.T) {
	for _, path := range []string{"/mapi.php", "/api/pay/create", "/api/pay/submit"} {
		t.Run(path, func(t *testing.T) {
			f, key := paymentDefend206WireFixture(t, path != "/mapi.php")
			w := paymentDefend206Wire(t, f, key, path, "defend206-off-baseline")
			if path == "/api/pay/submit" {
				if w.Code != http.StatusFound || !strings.Contains(w.Header().Get("Location"), "/checkout/") {
					t.Fatalf("off baseline RSA submit did not return a real controlled checkout: HTTP=%d body=%s", w.Code, w.Body.String())
				}
			} else {
				var result map[string]any
				wantCode := float64(0)
				if path == "/mapi.php" {
					wantCode = 1
				}
				if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil || result["code"] != wantCode {
					t.Fatalf("off baseline original payment did not succeed: HTTP=%d body=%s", w.Code, w.Body.String())
				}
				if trade, ok := result["trade_no"].(string); !ok || trade == "" {
					t.Fatal("off baseline did not return the original platform trade number")
				}
			}
			var orders, attempts int
			if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM provider_attempts)`).Scan(&orders, &attempts); err != nil || orders != 1 || attempts != 1 || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 1 {
				t.Fatal("off baseline was not one real ordinary in-process provider orchestration", orders, attempts, err)
			}
		})
	}
}

func TestPaymentDefend206EnabledPolicyBlocksOriginalMD5AndRSABeforeAnyPaymentEffects(t *testing.T) {
	for _, path := range []string{"/mapi.php", "/api/pay/create", "/api/pay/submit"} {
		t.Run(path, func(t *testing.T) {
			// This is a distinct fixture from the successful off baseline. The
			// gate is activated only through the original administrator HTTP API.
			f, key := paymentDefend206WireFixture(t, path != "/mapi.php")
			paymentDefend206Enable(t, f)
			before := paymentDefend206Effects(t, f)
			w := paymentDefend206Wire(t, f, key, path, "defend206-on-must-not-send")
			var result map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result["code"] != float64(-1) {
				t.Errorf("enabled 3/3 policy did not block the valid original payment: HTTP=%d body=%s err=%v", w.Code, w.Body.String(), err)
			}
			if w.Header().Get("Location") != "" {
				t.Error("blocked payment returned an executable checkout redirect")
			}
			for _, key := range []string{"trade_no", "payurl", "pay_info", "material", "qrcode", "prepay_id", "url"} {
				if _, exists := result[key]; exists {
					t.Errorf("blocked payment exposed payment material field %q", key)
				}
			}
			if after := paymentDefend206Effects(t, f); after != before {
				t.Errorf("blocked payment consumed order/attempt/queue/route work: before=%s after=%s", before, after)
			}
			if sends := f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load(); sends != 0 {
				t.Errorf("blocked payment reached the provider %d times, want0", sends)
			}
		})
	}
}
