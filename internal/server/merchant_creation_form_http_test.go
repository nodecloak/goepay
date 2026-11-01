package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// These requests use an isolated synthetic database and the real administrator
// resource and merchant login handlers. No provider operation is requested.
func merchantCreationFormRequest(t *testing.T, f channel168Fixture, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/console/v1/admin/resources/merchants", strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://pay.example.invalid")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	out := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(out, r)
	return out
}

func TestMerchantCreationFormActualHTTPContactLoginAndSession(t *testing.T) {
	f := channel168Local(t)
	const password = "synthetic-create-contact-password"
	for _, tc := range []struct{ name, phone, email, username string }{
		{"phone", "13000012211", "", "13000012211"},
		{"email", "", "create12212@example.test", "create12212@example.test"},
		{"international phone", "+651300012213", "", "+651300012213"},
		{"tagged email", "", "create+12214@example.test", "create+12214@example.test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"creation_mode": "account", "phone": tc.phone, "email": tc.email, "login_password": password, "group_id": "0"}
			created := merchantCreationFormRequest(t, f, body)
			if created.Code != http.StatusCreated {
				t.Fatalf("contact creation HTTP=%d, want201: %s", created.Code, created.Body.String())
			}
			var creation struct {
				Code int `json:"code"`
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if err := json.Unmarshal(created.Body.Bytes(), &creation); err != nil || creation.Code != 0 {
				t.Fatal("invalid merchant creation envelope", err)
			}
			id, err := strconv.ParseInt(creation.Data.ID, 10, 64)
			if err != nil || id <= 1 {
				t.Fatal("creation did not allocate a fresh merchant identity", err)
			}
			for _, secret := range []string{password, "login_password", "api_key", "creation_mode"} {
				if strings.Contains(created.Body.String(), secret) {
					t.Fatal("creation response exposed private setup field", secret)
				}
			}
			login := loginCaptcha205Request(f, http.MethodPost, "/console/v1/merchant/login", map[string]string{"username": tc.username, "password": password})
			loginCaptcha205OriginalSession(t, f, login, "merchant", tc.username)
			var merchantCookie *http.Cookie
			for _, cookie := range login.Result().Cookies() {
				if cookie.Name == "goepay_merchant" {
					merchantCookie = cookie
				}
			}
			if merchantCookie == nil || merchantCookie.Path != "/console/v1/merchant" {
				t.Fatal("new merchant login did not scope its cookie to the merchant console")
			}
			session, ok := f.s.Sessions.Get(merchantCookie.Value, "merchant")
			if !ok || session.User.ID != id || session.User.Username != tc.username || session.Issuer != nil {
				t.Fatal("new account login session bound to a different merchant")
			}
			r := httptest.NewRequest(http.MethodGet, "/console/v1/merchant/session", nil)
			r.AddCookie(merchantCookie)
			readback := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(readback, r)
			var current struct {
				Code int `json:"code"`
				Data struct {
					CSRF string `json:"csrf_token"`
					User struct {
						ID   string `json:"id"`
						Role string `json:"role"`
					} `json:"user"`
				} `json:"data"`
			}
			if readback.Code != http.StatusOK || json.Unmarshal(readback.Body.Bytes(), &current) != nil || current.Code != 0 || current.Data.User.ID != creation.Data.ID || current.Data.User.Role != "merchant" || current.Data.CSRF != session.CSRF {
				t.Fatalf("new merchant cookie failed actual session readback: HTTP=%d", readback.Code)
			}
			if loginCaptcha205Sessions(t, f, "merchant", tc.username) != 1 {
				t.Fatal("login did not persist exactly one merchant session")
			}
			var accounts, funded int
			if err := f.p.QueryRow(t.Context(), `SELECT count(*),count(*) FILTER(WHERE balance<>0) FROM accounts WHERE owner_id=$1`, id).Scan(&accounts, &funded); err != nil || accounts != 2 || funded != 0 {
				t.Fatal("contact creation must initialize only zero-balance accounts", accounts, funded, err)
			}
		})
	}
	var payments, attempts int
	if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM provider_attempts)`).Scan(&payments, &attempts); err != nil || payments != 0 || attempts != 0 {
		t.Fatal("contact creation/login unexpectedly created a payment intent", payments, attempts, err)
	}
}

func TestMerchantCreationFormActualHTTPInvalidOrDuplicateRollsBack(t *testing.T) {
	f := channel168Local(t)
	counts := func() [3]int {
		t.Helper()
		var out [3]int
		if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM merchants),(SELECT count(*) FROM console_users WHERE role='merchant'),(SELECT count(*) FROM accounts WHERE owner_id>0)`).Scan(&out[0], &out[1], &out[2]); err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := counts()
	for _, body := range []map[string]any{
		{"creation_mode": "account", "login_password": "synthetic-create-contact-password"},
		{"creation_mode": "account", "phone": "13000012215"},
		{"creation_mode": "account", "phone": "13000012215", "login_password": "short"},
		{"creation_mode": "account", "email": "invalid email", "login_password": "synthetic-create-contact-password"},
	} {
		out := merchantCreationFormRequest(t, f, body)
		if out.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid contact draft HTTP=%d, want422", out.Code)
		}
		if counts() != before {
			t.Fatal("rejected draft left merchant, login or money accounts")
		}
	}
	body := map[string]any{"creation_mode": "account", "phone": "13000012215", "login_password": "synthetic-create-contact-password"}
	if out := merchantCreationFormRequest(t, f, body); out.Code != http.StatusCreated {
		t.Fatalf("valid creation HTTP=%d, want201", out.Code)
	}
	committed := counts()
	if out := merchantCreationFormRequest(t, f, body); out.Code != http.StatusConflict {
		t.Fatalf("same contact repeated HTTP=%d, want409", out.Code)
	}
	if counts() != committed {
		t.Fatal("duplicate contact left an orphan merchant or accounts after rollback")
	}
}
