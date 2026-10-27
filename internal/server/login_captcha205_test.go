package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
)

const loginCaptcha205Password = "synthetic-login205-password"

// These regressions use only the existing persistent login, settings and 115
// credential vault. They compile before any login-CAPTCHA consumer is added.
// The missing-proof cases never need a remote verifier or a TestPay intent.
func loginCaptcha205Local(t *testing.T, enabled string) channel168Fixture {
	t.Helper()
	f := channel168Local(t)
	hash, err := identity.HashPassword(loginCaptcha205Password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('captcha205','merchant',1,'Synthetic CAPTCHA205',$1)`, hash); err != nil {
		t.Fatal(err)
	}
	service, err := NewPublicTestPayService203ForMaster(f.s, nil, bytes.Repeat([]byte{205}, 32), "https://pay.example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	f.s.PublicTestPay203 = service
	for key, value := range map[string]string{
		"captcha_open_login": enabled,
		"captcha_id":         strings.Repeat("a", 32),
		"captcha_version":    "1",
	} {
		site191Save(t, f, key, value)
	}
	if view := captchaSetKey203(t, f, strings.Repeat("b", 32)); !view.Configured {
		t.Fatal("synthetic sealed CAPTCHA credential was not configured")
	}
	return f
}

func loginCaptcha205Request(f channel168Fixture, method, path string, body any) *httptest.ResponseRecorder {
	var input *strings.Reader
	if body == nil {
		input = strings.NewReader("")
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			panic(err)
		}
		input = strings.NewReader(string(raw))
	}
	r := httptest.NewRequest(method, path, input)
	r.RemoteAddr = "192.0.2.205:5205"
	r.Header.Set("Origin", "https://pay.example.invalid")
	if method != http.MethodGet {
		r.Header.Set("Content-Type", "application/json")
	}
	out := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(out, r)
	return out
}

func loginCaptcha205Sessions(t *testing.T, f channel168Fixture, role, username string) int {
	t.Helper()
	var count int
	if err := f.p.QueryRow(t.Context(), `SELECT count(*) FROM console_sessions WHERE role=$1 AND username=$2`, role, username).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func loginCaptcha205NoPublicPaymentIntent(t *testing.T, f channel168Fixture) {
	t.Helper()
	var intents, challenges, consumed int
	if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM public_testpay203_intents),(SELECT count(*) FROM captcha_challenges203),(SELECT count(*) FROM captcha_used_proofs203)`).Scan(&intents, &challenges, &consumed); err != nil {
		t.Fatal(err)
	}
	if intents != 0 || challenges != 0 || consumed != 0 {
		t.Fatal("login metadata or missing proof borrowed a TestPay intent or consumed a CAPTCHA proof")
	}
}

func loginCaptcha205OriginalSession(t *testing.T, f channel168Fixture, out *httptest.ResponseRecorder, role, username string) {
	t.Helper()
	if out.Code != http.StatusOK {
		t.Fatalf("original %s password login must remain available: HTTP=%d", role, out.Code)
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			CSRF string `json:"csrf_token"`
		} `json:"data"`
	}
	if json.Unmarshal(out.Body.Bytes(), &envelope) != nil || envelope.Code != 0 || envelope.Data.CSRF == "" {
		t.Fatal("original password login did not return a usable console session")
	}
	var cookie *http.Cookie
	for _, candidate := range out.Result().Cookies() {
		if candidate.Name == "goepay_"+role {
			cookie = candidate
		}
	}
	if cookie == nil || cookie.Value == "" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("original password login did not return its protected role cookie")
	}
	session, ok := f.s.Sessions.Get(cookie.Value, role)
	if !ok || session.User.Username != username || session.User.Role != role || session.CSRF != envelope.Data.CSRF || session.Issuer != nil {
		t.Fatal("password login returned a wrong or delegated persistent session")
	}
}

func TestLoginCaptcha205ActualHTTPEnabledMerchantPasswordRejectsMissingProofWithoutSession(t *testing.T) {
	f := loginCaptcha205Local(t, "1")
	before := loginCaptcha205Sessions(t, f, "merchant", "captcha205")
	effects := publicTestPay203Effects(t, f)
	out := loginCaptcha205Request(f, "POST", "/console/v1/merchant/login", map[string]string{"username": "captcha205", "password": loginCaptcha205Password, "code": ""})
	if out.Code != http.StatusUnprocessableEntity && out.Code != http.StatusServiceUnavailable {
		t.Errorf("enabled merchant CAPTCHA accepted credentials without a proof: HTTP=%d, want422/503", out.Code)
	}
	for _, cookie := range out.Result().Cookies() {
		if cookie.Name == "goepay_merchant" && cookie.Value != "" {
			t.Error("missing CAPTCHA proof issued an authenticated merchant cookie")
		}
	}
	if loginCaptcha205Sessions(t, f, "merchant", "captcha205") != before {
		t.Error("missing CAPTCHA proof created a persistent merchant session")
	}
	if publicTestPay203Effects(t, f) != effects {
		t.Error("missing CAPTCHA proof created payment work")
	}
	loginCaptcha205NoPublicPaymentIntent(t, f)
}

func TestLoginCaptcha205ActualHTTPDisabledPreservesOriginalMerchantPasswordLogin(t *testing.T) {
	f := loginCaptcha205Local(t, "0")
	before := loginCaptcha205Sessions(t, f, "merchant", "captcha205")
	out := loginCaptcha205Request(f, "POST", "/console/v1/merchant/login", map[string]string{"username": "captcha205", "password": loginCaptcha205Password, "code": ""})
	loginCaptcha205OriginalSession(t, f, out, "merchant", "captcha205")
	if loginCaptcha205Sessions(t, f, "merchant", "captcha205") != before+1 {
		t.Fatal("disabled CAPTCHA changed original persistent session creation")
	}
	loginCaptcha205NoPublicPaymentIntent(t, f)
}

func TestLoginCaptcha205ActualHTTPMerchantSwitchDoesNotGateIndependentAdminPasswordLogin(t *testing.T) {
	// Pro's administrator page uses ArtDragVerify locally, then sends only
	// username/password/enc. It does not consume captcha_open_login or Geetest.
	// This preserves that switch boundary without treating its local slider as
	// a server-side authentication proof.
	f := loginCaptcha205Local(t, "1")
	// Set this after the vault's authenticated admin setup: the password change
	// rotates auth_version and deliberately invalidates the setup session.
	if _, err := f.p.Exec(t.Context(), `UPDATE console_users SET password_hash=(SELECT password_hash FROM console_users WHERE username='captcha205' AND role='merchant') WHERE username='admin168' AND role='admin'`); err != nil {
		t.Fatal(err)
	}
	before := loginCaptcha205Sessions(t, f, "admin", "admin168")
	out := loginCaptcha205Request(f, "POST", "/console/v1/admin/login", map[string]string{"username": "admin168", "password": loginCaptcha205Password, "code": ""})
	loginCaptcha205OriginalSession(t, f, out, "admin", "admin168")
	if loginCaptcha205Sessions(t, f, "admin", "admin168") != before+1 {
		t.Fatal("merchant CAPTCHA switch changed independent admin session creation")
	}
	loginCaptcha205NoPublicPaymentIntent(t, f)
}

func TestLoginCaptcha205ActualHTTPPublicMetadataReportsMerchantPasswordPolicyWithoutTestPayIntent(t *testing.T) {
	for _, tc := range []struct {
		name, setting string
		enabled       bool
	}{{"enabled", "1", true}, {"disabled", "0", false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := loginCaptcha205Local(t, tc.setting)
			before := loginCaptcha205Sessions(t, f, "merchant", "captcha205")
			effects := publicTestPay203Effects(t, f)
			out := loginCaptcha205Request(f, "GET", "/console/v1/public/login-captcha?role=merchant&mode=password", nil)
			if out.Code != http.StatusOK {
				t.Errorf("anonymous login CAPTCHA metadata is unavailable: HTTP=%d, want200", out.Code)
			} else {
				var envelope struct {
					Code int                        `json:"code"`
					Data map[string]json.RawMessage `json:"data"`
				}
				if json.Unmarshal(out.Body.Bytes(), &envelope) != nil || envelope.Code != 0 {
					t.Error("login CAPTCHA metadata returned an invalid JSON envelope")
				} else {
					var enabled bool
					value, found := envelope.Data["enabled"]
					if !found || string(value) == "null" || json.Unmarshal(value, &enabled) != nil || enabled != tc.enabled {
						t.Error("login CAPTCHA metadata lost the server's boolean enabled policy")
					}
				}
				if out.Header().Get("Cache-Control") != "no-store" {
					t.Error("login CAPTCHA metadata was cacheable")
				}
			}
			for _, secret := range []string{strings.Repeat("b", 32), loginCaptcha205Password, f.token, f.csrf, "captcha_key", "test_pay_uid"} {
				if strings.Contains(out.Body.String(), secret) {
					t.Error("anonymous login CAPTCHA metadata exposed private settings or console authority")
				}
			}
			for _, cookie := range out.Result().Cookies() {
				if (cookie.Name == "goepay_admin" || cookie.Name == "goepay_merchant") && cookie.Value != "" {
					t.Error("login metadata created an authenticated console cookie")
				}
			}
			if loginCaptcha205Sessions(t, f, "merchant", "captcha205") != before || publicTestPay203Effects(t, f) != effects {
				t.Error("reading login metadata created a session or payment work")
			}
			loginCaptcha205NoPublicPaymentIntent(t, f)
		})
	}
}
