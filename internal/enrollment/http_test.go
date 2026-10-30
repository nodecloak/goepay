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

func TestPublicPhoneEnrollmentHTTPRoutesGatedOnGateway(t *testing.T) {
	s, _ := enrollmentFixture(t)
	h := NewHandler(s, "http://localhost")
	ctx := context.Background()
	phone := "13800130001"

	// Gateway OFF (default): capabilities.sms_enabled false, and the phone routes
	// refuse with 403 (ErrClosed) — not offered.
	w := publicRequest(t, h, "GET", "capabilities", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"sms_enabled":false`) {
		t.Fatal("sms capability off", w.Code, w.Body.String())
	}
	off := `{"code":"246810","phone":"` + phone + `","username":"phone-http","name":"PH","password":"correct-test-password"}`
	if w = publicRequest(t, h, "POST", "register-phone", off, "http://localhost"); w.Code != 403 {
		t.Fatal("register-phone must be closed when gateway off", w.Code, w.Body.String())
	}
	if w = publicRequest(t, h, "POST", "recover-password-phone", `{"code":"246810","phone":"`+phone+`","new_password":"correct-test-password"}`, "http://localhost"); w.Code != 403 {
		t.Fatal("recover-password-phone must be closed when gateway off", w.Code, w.Body.String())
	}

	// Turn the gateway on with an active provider, seed a live code, register.
	seal := make([]byte, 64)
	mac := make([]byte, 32)
	if _, e := s.pool.Exec(ctx, `INSERT INTO sms_providers134(provider,sealed,mac,configured)VALUES('aliyun',$1,$2,true)`, seal, mac); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE sms_gateway_head134 SET active='aliyun',status=1 WHERE singleton`); e != nil {
		t.Fatal(e)
	}
	if w = publicRequest(t, h, "GET", "capabilities", "", ""); !strings.Contains(w.Body.String(), `"sms_enabled":true`) {
		t.Fatal("sms capability not on", w.Body.String())
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO verification_codes134(uid,scene,type,recipient,code,ip)VALUES(0,'reg',1,$1,'246810','192.0.2.70')`, phone); e != nil {
		t.Fatal(e)
	}
	// Non-strict JSON is refused.
	if w = publicRequest(t, h, "POST", "register-phone", off+" {}", "http://localhost"); w.Code != 400 {
		t.Fatal("non-strict phone register accepted", w.Code)
	}
	w = publicRequest(t, h, "POST", "register-phone", off, "http://localhost")
	if w.Code != 201 {
		t.Fatal("phone register", w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), "password") || strings.Contains(w.Body.String(), "246810") {
		t.Fatal("phone registration exposed credential/session", w.Body.String())
	}
	// The phone claim exists and no auto-login happened.
	var claimed int64
	if e := s.pool.QueryRow(ctx, "SELECT merchant_id FROM enrollment_phones134 WHERE phone_hash=$1", s.digest("phone", phone)).Scan(&claimed); e != nil || claimed <= 0 {
		t.Fatal("phone claim not recorded", claimed, e)
	}
}

// verifytype (用户验证方式) restricts which channel a new merchant may register
// with. 0 邮箱验证 permits only the email path; 1 手机验证 permits only the phone
// path. Enforcement is on the server before any code is sent or account made,
// and Capabilities surfaces it as verify_type for the UI. Recovery is never
// restricted, and the default (no row) keeps both paths open.
func TestPublicEnrollmentVerifyTypeRestrictsRegistrationChannel(t *testing.T) {
	s, f := enrollmentFixture(t)
	h := NewHandler(s, "http://localhost")
	ctx := context.Background()
	phone := "13800130009"

	setVerifyType := func(v string) {
		t.Helper()
		if _, e := s.pool.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'='verifytype'`); e != nil {
			t.Fatal(e)
		}
		if v != "" {
			if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key','verifytype','value',$1::text))`, v); e != nil {
				t.Fatal(e)
			}
		}
	}

	// Default: no row — capabilities.verify_type is "" and the email send works.
	w := publicRequest(t, h, "GET", "capabilities", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"verify_type":""`) {
		t.Fatal("default verify_type not empty", w.Code, w.Body.String())
	}

	// verifytype=0 邮箱验证: capabilities say "email"; the email send is accepted,
	// and the phone register is refused (403) before any send.
	setVerifyType("0")
	if w = publicRequest(t, h, "GET", "capabilities", "", ""); !strings.Contains(w.Body.String(), `"verify_type":"email"`) {
		t.Fatal("verify_type email not surfaced", w.Body.String())
	}
	if w = publicRequest(t, h, "POST", "verification-code", `{"purpose":"register","email":"vt-email@example.test"}`, "http://localhost"); w.Code != 202 {
		t.Fatal("email register send refused under verifytype=0", w.Code, w.Body.String())
	}
	// Phone register path is closed. Turn the SMS gateway on so the refusal is
	// the verifytype restriction, not the gateway being off.
	seal := make([]byte, 64)
	mac := make([]byte, 32)
	if _, e := s.pool.Exec(ctx, `INSERT INTO sms_providers134(provider,sealed,mac,configured)VALUES('aliyun',$1,$2,true)`, seal, mac); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE sms_gateway_head134 SET active='aliyun',status=1 WHERE singleton`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO verification_codes134(uid,scene,type,recipient,code,ip)VALUES(0,'reg',1,$1,'246810','192.0.2.70')`, phone); e != nil {
		t.Fatal(e)
	}
	regPhone := `{"code":"246810","phone":"` + phone + `","username":"vt-phone","name":"VT","password":"correct-test-password"}`
	if w = publicRequest(t, h, "POST", "register-phone", regPhone, "http://localhost"); w.Code != 403 {
		t.Fatal("phone register must be refused under verifytype=0", w.Code, w.Body.String())
	}
	var created int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM enrollment_phones134 WHERE phone_hash=$1", s.digest("phone", phone)).Scan(&created); e != nil || created != 0 {
		t.Fatal("refused phone register created an account", created, e)
	}

	// verifytype=1 手机验证: capabilities say "phone"; the phone register works,
	// and the email send is refused (403) before any code is sent.
	setVerifyType("1")
	if w = publicRequest(t, h, "GET", "capabilities", "", ""); !strings.Contains(w.Body.String(), `"verify_type":"phone"`) {
		t.Fatal("verify_type phone not surfaced", w.Body.String())
	}
	if w = publicRequest(t, h, "POST", "verification-code", `{"purpose":"register","email":"vt-blocked@example.test"}`, "http://localhost"); w.Code != 403 {
		t.Fatal("email register send must be refused under verifytype=1", w.Code, w.Body.String())
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO verification_codes134(uid,scene,type,recipient,code,ip)VALUES(0,'reg',1,$1,'135791','192.0.2.70')`, phone); e != nil {
		t.Fatal(e)
	}
	regPhone = `{"code":"135791","phone":"` + phone + `","username":"vt-phone1","name":"VT","password":"correct-test-password"}`
	if w = publicRequest(t, h, "POST", "register-phone", regPhone, "http://localhost"); w.Code != 201 {
		t.Fatal("phone register must succeed under verifytype=1", w.Code, w.Body.String())
	}

	// Recovery is never restricted: a recover email send is accepted even under
	// verifytype=1. (sender is live via the fixture.)
	_ = f
	if w = publicRequest(t, h, "POST", "verification-code", `{"purpose":"recover","email":"vt-recover@example.test"}`, "http://localhost"); w.Code != 202 {
		t.Fatal("recovery must not be restricted by verifytype", w.Code, w.Body.String())
	}
}
