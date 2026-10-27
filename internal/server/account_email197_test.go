package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"goepay/internal/enrollment"
	"goepay/internal/identity"
)

type email197Fixture struct {
	channel168Fixture
	service     *enrollment.Service
	token, csrf string
	mu          sync.Mutex
	messages    []struct{ email, purpose, code string }
}

func email197Local(t *testing.T) *email197Fixture {
	t.Helper()
	f := &email197Fixture{channel168Fixture: channel168Local(t)}
	hash, e := identity.HashPassword("local-email197-password")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('email197','merchant',1,'Email197',$1)`, hash); e != nil {
		t.Fatal(e)
	}
	f.token, f.csrf, e = f.s.Sessions.Create(identity.User{ID: 1, Username: "email197", Role: "merchant", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	service, e := enrollment.New(f.p, f.s.Store, make([]byte, 32), enrollment.SenderFunc(func(_ context.Context, email, purpose, code string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.messages = append(f.messages, struct{ email, purpose, code string }{email, purpose, code})
		return nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	f.service = service
	f.s.Enrollment = enrollment.NewHandler(service, "http://127.0.0.1:8187")
	return f
}
func (f *email197Fixture) request(method, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "/console/v1/merchant/account/email"+path, strings.NewReader(string(raw)))
	r.RemoteAddr = "192.0.2.197:12345"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func email197Data(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("email binding HTTP", w.Code, w.Body.String())
	}
	var v struct {
		Code int
		Data map[string]any
	}
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil || v.Code != 0 || v.Data == nil {
		t.Fatal("email binding envelope", e)
	}
	return v.Data
}
func (f *email197Fixture) start(t *testing.T, id string) map[string]any {
	t.Helper()
	v := email197Data(t, f.request("GET", "", nil))
	return email197Data(t, f.request("POST", "", map[string]any{"flow_id": id, "expected_fingerprint": v["fingerprint"], "current_password": "local-email197-password"}))
}
func TestEmail197BindingMetadataRequiresVerifiedSource(t *testing.T) {
	f := email197Local(t)
	if _, e := f.p.Exec(t.Context(), `UPDATE merchants SET profile='{"email":"unverified197@example.test"}' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	v := email197Data(t, f.request("GET", "", nil))
	if v["email"] != "" || v["verified"] != false || v["email_enabled"] != true || v["requires"] != "password" {
		t.Fatal("unverified profile became recovery identity", v)
	}
}
func TestEmail197FirstBindingVerifiesTargetAndRevokesSessionsWithoutFunds(t *testing.T) {
	f := email197Local(t)
	id := strings.Repeat("a", 43)
	v := f.start(t, id)
	if v["phase"] != "new" {
		t.Fatal("first binding did not require password then new target", v)
	}
	v = email197Data(t, f.request("POST", "/"+id+"/new-code", map[string]any{"email": "New197@Example.test"}))
	if v["phase"] != "new" || v["delivery"] != "sent" {
		t.Fatal("target challenge absent", v)
	}
	f.mu.Lock()
	msg := f.messages[len(f.messages)-1]
	n := len(f.messages)
	f.mu.Unlock()
	if msg.email != "new197@example.test" || msg.purpose != "contact-new" || len(msg.code) != 7 {
		t.Fatal("wrong verification envelope")
	}
	email197Data(t, f.request("POST", "/"+id+"/new-code", map[string]any{"email": "New197@Example.test"}))
	f.mu.Lock()
	if len(f.messages) != n {
		t.Fatal("same flow retried SMTP")
	}
	f.mu.Unlock()
	if w := f.request("POST", "/"+id+"/complete", map[string]any{"code": "0000000"}); w.Code != 422 {
		t.Fatal("invalid target OTP accepted", w.Code)
	}
	v = email197Data(t, f.request("POST", "/"+id+"/complete", map[string]any{"code": msg.code}))
	if v["phase"] != "completed" || v["reauthenticate"] != true {
		t.Fatal("binding completion absent", v)
	}
	var email string
	var sessions, effects int
	if e := f.p.QueryRow(t.Context(), `SELECT email FROM enrollment_emails WHERE merchant_id=1`).Scan(&email); e != nil || email != "new197@example.test" {
		t.Fatal("verified recovery claim missing", email, e)
	}
	if e := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM console_sessions WHERE role='merchant' AND subject_id=1),(SELECT count(*)FROM orders)+(SELECT count(*)FROM journals)+(SELECT count(*)FROM jobs)`).Scan(&sessions, &effects); e != nil || sessions != 0 || effects != 0 {
		t.Fatal("binding retained session or changed funds", sessions, effects, e)
	}
}
func TestEmail197CannotBeginWithWrongPasswordOrDelegatedSession(t *testing.T) {
	f := email197Local(t)
	v := email197Data(t, f.request("GET", "", nil))
	w := f.request("POST", "", map[string]any{"flow_id": strings.Repeat("b", 43), "expected_fingerprint": v["fingerprint"], "current_password": "incorrect"})
	if w.Code != 422 {
		t.Fatal("first binding skipped password", w.Code)
	}
	email197Data(t, f.request("GET", "", nil))
}
func (f *email197Fixture) legacy(t *testing.T) (int64, string) {
	t.Helper()
	if _, e := f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=body||'{"value":"true"}' WHERE resource='settings' AND body->>'key'='registration_open'; INSERT INTO backoffice_records(resource,body) SELECT 'settings','{"key":"registration_open","value":"true"}' WHERE NOT EXISTS(SELECT 1 FROM backoffice_records WHERE resource='settings' AND body->>'key'='registration_open')`); e != nil {
		t.Fatal(e)
	}
	ch, e := f.service.Issue(t.Context(), enrollment.VerificationRequest{Email: "old197@example.test", Purpose: "register", Source: "192.0.2.196"})
	if e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	code := f.messages[len(f.messages)-1].code
	f.mu.Unlock()
	v, e := f.service.Register(t.Context(), enrollment.RegisterInput{Email: "old197@example.test", ChallengeID: ch.ID, Code: code, Source: "192.0.2.196", Username: "legacy197", Name: "Legacy email", Password: "local-email197-password"})
	if e != nil {
		t.Fatal(e)
	}
	f.token, f.csrf, e = f.s.Sessions.Create(identity.User{ID: v.MerchantID, Username: v.Username, Role: "merchant", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(t.Context(), `UPDATE enrollment_challenges SET created_at=created_at-interval '2 minutes'`); e != nil {
		t.Fatal(e)
	}
	return v.MerchantID, v.Username
}
func TestEmail197ExistingBindingRequiresOldCodeAndProfileCannotBypass(t *testing.T) {
	f := email197Local(t)
	mid, _ := f.legacy(t)
	id := strings.Repeat("c", 43)
	v := f.start(t, id)
	if v["phase"] != "old" {
		t.Fatal("existing verified address skipped old proof", v)
	}
	if w := f.request("POST", "/"+id+"/new-code", map[string]any{"email": "replace197@example.test"}); w.Code != 409 {
		t.Fatal("target sent before old proof", w.Code)
	}
	email197Data(t, f.request("POST", "/"+id+"/old-code", map[string]any{}))
	f.mu.Lock()
	msg := f.messages[len(f.messages)-1]
	f.mu.Unlock()
	if msg.email != "old197@example.test" || msg.purpose != "contact-old" {
		t.Fatal("old verification delivered to wrong target")
	}
	if w := f.request("POST", "/"+id+"/old-verify", map[string]any{"code": "0000000"}); w.Code != 422 {
		t.Fatal("old proof skipped", w.Code)
	}
	v = email197Data(t, f.request("POST", "/"+id+"/old-verify", map[string]any{"code": msg.code}))
	if v["phase"] != "new" {
		t.Fatal("old proof did not unlock new dialog", v)
	}
	if e := f.service.Recover(t.Context(), enrollment.RecoverInput{ChallengeID: v["old_challenge_id"].(string), Email: msg.email, Source: "192.0.2.197", Code: msg.code, Password: "different-password197"}); e == nil {
		t.Fatal("contact proof authorized password recovery")
	}
	r := httptest.NewRequest("PATCH", "/console/v1/merchant/account/profile", strings.NewReader(`{"name":"New name","email":"bypass197@example.test"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 409 {
		t.Fatal("profile changed verified binding without proofs", w.Code)
	}
	email197Data(t, f.request("POST", "/"+id+"/new-code", map[string]any{"email": "replace197@example.test"}))
	f.mu.Lock()
	newMsg := f.messages[len(f.messages)-1]
	f.mu.Unlock()
	email197Data(t, f.request("POST", "/"+id+"/complete", map[string]any{"code": newMsg.code}))
	var current string
	if e := f.p.QueryRow(t.Context(), `SELECT email FROM enrollment_emails WHERE merchant_id=$1`, mid).Scan(&current); e != nil || current != "replace197@example.test" {
		t.Fatal("old claim not replaced", e, current)
	}
	var immutable bool
	if e := f.p.QueryRow(t.Context(), `SELECT count(*)>0 FROM email_binding_events197 WHERE kind='complete'`).Scan(&immutable); e != nil || !immutable {
		t.Fatal("verified change lost durable proof", e)
	}
}
func TestEmail197FlowIsBoundToOriginalSessionAndCloseDisablesProof(t *testing.T) {
	f := email197Local(t)
	id := strings.Repeat("d", 43)
	f.start(t, id)
	originalToken, originalCSRF := f.token, f.csrf
	var e error
	f.token, f.csrf, e = f.s.Sessions.Create(identity.User{ID: 1, Username: "email197", Role: "merchant", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	if w := f.request("GET", "/"+id, nil); w.Code != 403 {
		t.Fatal("another session read original flow", w.Code)
	}
	f.token, f.csrf = originalToken, originalCSRF
	email197Data(t, f.request("DELETE", "/"+id, nil))
	if w := f.request("POST", "/"+id+"/new-code", map[string]any{"email": "closed197@example.test"}); w.Code != 409 {
		t.Fatal("closed flow sent email", w.Code)
	}
}
