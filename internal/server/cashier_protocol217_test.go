package server

import (
	"bytes"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"goepay/internal/compat"
	"goepay/internal/notification"
)

func cashierProtocol217Submit(t *testing.T, f channel168Fixture, key *rsa.PrivateKey, path, order, kind string) string {
	t.Helper()
	fields := compat.Params{"pid": "1", "out_trade_no": order, "name": "Synthetic cashier217", "money": "1.23", "notify_url": "https://merchant.example.invalid/payments/notify", "return_url": "https://merchant.example.invalid/payments/return", "method": "native", "type": kind, "sign_type": "MD5"}
	if key == nil {
		fields["sign"] = compat.SignMD5(fields, "test-secret")
	} else {
		fields["sign_type"] = "RSA"
		fields["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
		var err error
		fields["sign"], err = compat.SignRSA(fields, key)
		if err != nil {
			t.Fatal(err)
		}
	}
	values := url.Values{}
	for name, value := range fields {
		values.Set(name, value)
	}
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "192.0.2.217:5217"
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if path == "/api/pay/create" || path == "/mapi.php" {
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result["pay_type"] != "jump" {
			t.Fatalf("expected signed cashier material, HTTP=%d", w.Code)
		}
		target, _ := result["url"].(string)
		return target
	}
	if w.Code != http.StatusFound {
		t.Fatalf("expected cashier redirect, HTTP=%d", w.Code)
	}
	return w.Header().Get("Location")
}

func cashierProtocol217Raw(t *testing.T, target string) (map[string]any, *url.URL) {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil || u.Path != "/cashier" {
		t.Fatal("cashier intent location missing")
	}
	raw, err := base64.RawURLEncoding.DecodeString(u.Query().Get("i"))
	var fields map[string]any
	if err != nil || json.Unmarshal(raw, &fields) != nil {
		t.Fatal("cashier payload not decodable")
	}
	return fields, u
}

func cashierProtocol217SignedTarget(t *testing.T, s *Server, fields map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return "/cashier?" + url.Values{"i": {base64.RawURLEncoding.EncodeToString(raw)}, "t": {s.cashierToken(raw)}}.Encode()
}

func TestCashierProtocol217AuthenticatedHTTPEntrySurvivesChoiceAndReplay(t *testing.T) {
	for _, tc := range []struct {
		name, path, kind, version string
		force, rsa                bool
	}{
		{"v2-empty-submit", "/api/pay/submit", "", "v2", false, true},
		{"v2-forced-submit", "/api/pay/submit", "alipay", "v2", true, true},
		{"v2-forced-create", "/api/pay/create", "alipay", "v2", true, true},
		{"v1-md5-empty-submit", "/submit.php", "", "v1", false, false},
		{"v1-rsa-empty-submit", "/submit.php", "", "v1", false, true},
		{"v1-md5-forced-mapi", "/mapi.php", "alipay", "v1", true, false},
		{"v1-rsa-forced-mapi", "/mapi.php", "alipay", "v1", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, merchantKey := paymentDefend206WireFixture(t, tc.rsa)
			if tc.force {
				if _, err := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key','pay_force_cashier_pids','value','1'))`); err != nil {
					t.Fatal(err)
				}
			}
			target := cashierProtocol217Submit(t, f, merchantKey, tc.path, tc.name, tc.kind)
			payload, u := cashierProtocol217Raw(t, target)
			if payload["v"] != tc.version {
				t.Errorf("authenticated cashier intent lost %s protocol: got %v", tc.version, payload["v"])
			}
			if _, err := f.s.Store.Order(t.Context(), 1, tc.name); err == nil {
				t.Fatal("cashier intent created an order before the payer chose")
			}
			for replay := range 2 {
				w := cashierChoose(f, u.RequestURI(), "alipay")
				if w.Code != http.StatusFound || !strings.Contains(w.Header().Get("Location"), "/checkout/") {
					t.Fatalf("cashier choice/replay %d did not continue to checkout: HTTP=%d", replay, w.Code)
				}
			}
			order, err := f.s.Store.Order(t.Context(), 1, tc.name)
			if err != nil {
				t.Fatal(err)
			}
			if order.Version != tc.version {
				t.Errorf("cashier changed authenticated %s request into %s order", tc.version, order.Version)
			}
			var count int
			if err := f.p.QueryRow(t.Context(), `SELECT count(*) FROM orders WHERE merchant_id=1 AND out_trade_no=$1`, tc.name).Scan(&count); err != nil || count != 1 {
				t.Fatal("cashier replay created another order")
			}
			if f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 1 {
				t.Fatal("cashier replay submitted a replacement provider request")
			}
			merchant, err := f.s.Store.Merchant(t.Context(), 1)
			if err != nil {
				t.Fatal(err)
			}
			callback, err := notification.URLWithPolicy(order.NotifyURL, order, merchant, f.s.PlatformKey, time.Now(), false)
			if err != nil {
				t.Fatal(err)
			}
			address, err := url.Parse(callback)
			if err != nil {
				t.Fatal(err)
			}
			params, err := compat.Parse(address.Query())
			if err != nil || params["out_trade_no"] != tc.name || params["money"] != "1.23" {
				t.Fatal("callback changed original cashier facts")
			}
			if tc.version == "v2" {
				if params["sign_type"] != "RSA" || params["timestamp"] == "" || !compat.VerifyRSAWithProfile(params, params["sign"], &f.s.PlatformKey.PublicKey, compat.ProfileSDKV2) {
					t.Fatal("V2 cashier order did not produce a verifiable RSA notification")
				}
			} else if params["sign_type"] != "MD5" || params["timestamp"] != "" || !compat.VerifyMD5(params, "test-secret", params["sign"]) {
				t.Fatal("legacy cashier order callback changed from MD5")
			}
		})
	}
}

func TestCashierProtocol217OldSignedTokenRemainsV1(t *testing.T) {
	f := channel168Local(t)
	// This map intentionally reproduces an issued pre-217 payload with no v key.
	old := map[string]any{"m": 1, "o": "pre217-signed-token", "n": "Synthetic old cashier", "a": "1.00", "nu": "https://merchant.example.invalid/notify", "ru": "https://merchant.example.invalid/return", "p": "", "b": "", "ip": "192.0.2.217", "e": time.Now().Add(time.Minute).Unix()}
	target := cashierProtocol217SignedTarget(t, f.s, old)
	decoded, ok := f.s.decodeCashierIntent(httptest.NewRequest(http.MethodGet, target, nil))
	if !ok {
		t.Fatal("issued old signed cashier token was rejected")
	}
	raw, err := json.Marshal(decoded)
	var normalized map[string]any
	if err != nil || json.Unmarshal(raw, &normalized) != nil || normalized["v"] != "v1" {
		t.Error("historical missing-version token must normalize to v1")
	}
	if w := cashierChoose(f, target, "alipay"); w.Code != http.StatusFound {
		t.Fatal("old signed token did not continue to checkout")
	}
	order, err := f.s.Store.Order(t.Context(), 1, "pre217-signed-token")
	if err != nil || order.Version != "v1" {
		t.Fatal("historical token no longer creates the original v1 order")
	}
	merchant, err := f.s.Store.Merchant(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	callback, err := notification.URLWithPolicy(order.NotifyURL, order, merchant, nil, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(callback)
	params, err := compat.Parse(u.Query())
	if err != nil || params["sign_type"] != "MD5" || params["timestamp"] != "" || !compat.VerifyMD5(params, "test-secret", params["sign"]) {
		t.Fatal("historical token no longer yields its original MD5 callback")
	}
}

func TestCashierProtocol217RejectsSignedUnknownVersionsAndTampering(t *testing.T) {
	s := &Server{CheckoutKey: bytes.Repeat([]byte{217}, 32)}
	fields := map[string]any{"m": 1, "o": "version217", "a": "1.00", "e": time.Now().Add(time.Minute).Unix()}
	for _, version := range []any{"v3", "V2", "v1 ", "0", "MD5", "", nil, 2} {
		fields["v"] = version
		if _, ok := s.decodeCashierIntent(httptest.NewRequest(http.MethodGet, cashierProtocol217SignedTarget(t, s, fields), nil)); ok {
			t.Errorf("signed unsupported version %#v accepted", version)
		}
	}
	fields["v"] = "v2"
	valid := cashierProtocol217SignedTarget(t, s, fields)
	u, _ := url.Parse(valid)
	q := u.Query()
	fields["v"] = "v1"
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	q.Set("i", base64.RawURLEncoding.EncodeToString(raw))
	u.RawQuery = q.Encode()
	if _, ok := s.decodeCashierIntent(httptest.NewRequest(http.MethodGet, u.RequestURI(), nil)); ok {
		t.Fatal("tampering authenticated v2 into v1 was accepted")
	}
}
