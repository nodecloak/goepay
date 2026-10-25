package enrollment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func publicRequest(t *testing.T, h http.Handler, method, path, body, origin string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://localhost/console/v1/public/"+path, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.70:12345"
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestPublicEnrollmentHTTPStrictJSONNoSecretsAndNoAutomaticLogin(t *testing.T) {
	s, f := enrollmentFixture(t)
	h := NewHandler(s, "http://localhost")
	w := publicRequest(t, h, "GET", "capabilities", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"registration_open":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, body := range []string{`{"purpose":"register","email":"http@example.test","source":"1.1.1.1"}`, `{"purpose":"register","email":"http@example.test"} {}`} {
		w = publicRequest(t, h, "POST", "verification-code", body, "")
		if w.Code != 400 {
			t.Fatal("non-strict input", w.Code, w.Body.String())
		}
	}
	w = publicRequest(t, h, "POST", "verification-code", `{"purpose":"register","email":"http@example.test"}`, "https://evil.example")
	if w.Code != 403 {
		t.Fatal("cross-origin request accepted", w.Code)
	}
	w = publicRequest(t, h, "POST", "verification-code", `{"purpose":"register","email":"http@example.test"}`, "http://localhost")
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		Data Challenge `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	code := f.last(t).code
	if strings.Contains(w.Body.String(), code) || strings.Contains(w.Body.String(), "http@example.test") {
		t.Fatal("send response exposes private information", w.Body.String())
	}
	body := `{"challenge_id":"` + out.Data.ID + `","code":"` + code + `","email":"http@example.test","username":"http-user","name":"HTTP","password":"correct-test-password"}`
	w = publicRequest(t, h, "POST", "register", body, "http://localhost")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), "api_key") || strings.Contains(w.Body.String(), code) || strings.Contains(w.Body.String(), "password") {
		t.Fatal("registration exposed credential/session", w.Body.String())
	}
	var source []byte
	if e := s.pool.QueryRow(context.Background(), "SELECT source_hash FROM enrollment_challenges WHERE id=$1", out.Data.ID).Scan(&source); e != nil || string(source) != string(s.digest("source", "192.0.2.70")) {
		t.Fatal("connection source not bound", e)
	}
}
func TestPublicRecoveryResponsesAndDisabledSender(t *testing.T) {
	s, f := enrollmentFixture(t)
	h := NewHandler(s, "http://localhost")
	known := registerVerified(t, s, f, "known@example.test", "known-user", "192.0.2.70")
	_ = known
	ageSendingCooldown(t, s)
	for _, email := range []string{"known@example.test", "unknown@example.test"} {
		w := publicRequest(t, h, "POST", "verification-code", `{"purpose":"recover","email":"`+email+`"}`, "")
		if w.Code != 202 {
			t.Fatal(email, w.Code, w.Body.String())
		}
		var out struct {
			Data Challenge `json:"data"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		body := `{"challenge_id":"` + out.Data.ID + `","code":"` + f.last(t).code + `","email":"` + email + `","new_password":"replacement-password"}`
		w = publicRequest(t, h, "POST", "recover-password", body, "")
		if w.Code != 200 || w.Body.String() != "{\"code\":0,\"message\":\"request completed; sign in normally\",\"data\":null}\n" {
			t.Fatal("enumerating recovery response", email, w.Code, w.Body.String())
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("recovery logged user in")
		}
	}
	nilService, e := New(s.pool, s.store, make([]byte, 32), nil)
	if e != nil {
		t.Fatal(e)
	}
	w := publicRequest(t, NewHandler(nilService, "http://localhost"), "POST", "verification-code", `{"purpose":"recover","email":"fresh@example.test"}`, "")
	if w.Code != 503 {
		t.Fatal("unconfigured transport reported send", w.Code)
	}
}
