package server

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNotice231AccountsBindingAndRotation(t *testing.T) {
	f := channel168Local(t)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/admin/settings/notice-accounts/"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	data := func(w *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var out struct {
			Data map[string]any `json:"data"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("account HTTP %d: %s", w.Code, w.Body.String())
		}
		return out.Data
	}
	body := func(rev, action string, enabled bool) string {
		secret, app := "", ""
		if action == "replace" {
			secret = "synthetic-secret231"
			app = "123456"
		}
		b, _ := json.Marshal(map[string]any{"expected_revision": rev, "enabled": enabled, "credential_action": action, "app_id": app, "secret": secret, "phone": "", "token_source": ""})
		return string(b)
	}
	if data(call("GET", "qqbot", ""))["revision"] != "0" {
		t.Fatal("unexpected initial revision")
	}
	if call("PUT", "qqbot", body("0", "keep", true)).Code != 422 {
		t.Fatal("enabled missing credentials")
	}
	v := data(call("PUT", "qqbot", body("0", "replace", true)))
	if v["revision"] != "1" || v["bound"] != false {
		t.Fatal("pair save failed")
	}
	if call("PUT", "qqbot", body("0", "keep", false)).Code != 409 {
		t.Fatal("stale CAS accepted")
	}
	if call("PUT", "qqbot", `{"expected_revision":"1","enabled":null,"credential_action":"keep","app_id":"","secret":"","phone":"","token_source":""}`).Code != 400 {
		t.Fatal("null accepted")
	}
	if strings.Contains(call("GET", "qqbot", "").Body.String(), "synthetic-secret231") {
		t.Fatal("secret echoed")
	}
	validationBody := fmt.Sprintf(`{"op":13,"d":{"event_ts":%q,"plain_token":"opaque_validation231"}}`, fmt.Sprint(time.Now().Unix()))
	validationRequest := httptest.NewRequest("POST", "/payment/qqbot/webhook", strings.NewReader(validationBody))
	validationReply := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(validationReply, validationRequest)
	if validationReply.Code != 200 {
		t.Fatalf("optional AppID header blocked provider validation: %d", validationReply.Code)
	}
	wrongApp := httptest.NewRequest("POST", "/payment/qqbot/webhook", strings.NewReader(validationBody))
	wrongApp.Header.Set("X-Bot-Appid", "different231")
	wrongReply := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(wrongReply, wrongApp)
	if wrongReply.Code != 400 {
		t.Fatal("mismatched AppID header accepted")
	}
	a := data(call("POST", "qqbot/binding", `{"expected_revision":"1"}`))
	challenge := a["challenge"].(string)
	if call("POST", "qqbot/binding", `{"expected_revision":"1"}`).Code != 409 {
		t.Fatal("parallel active binding accepted")
	}
	seed := []byte(strings.Repeat("synthetic-secret231", 3))[:32]
	key := ed25519.NewKeyFromSeed(seed)
	callback := func(content, recipient string, valid bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"op": 0, "t": "C2C_MESSAGE_CREATE", "id": "local-event231", "d": map[string]any{"content": content, "author": map[string]string{"user_openid": recipient}}})
		stamp := fmt.Sprint(time.Now().Unix())
		sig := hex.EncodeToString(ed25519.Sign(key, append([]byte(stamp), raw...)))
		if !valid {
			sig = "00"
		}
		r := httptest.NewRequest("POST", "/payment/qqbot/webhook", strings.NewReader(string(raw)))
		// The provider AppID header is optional; the raw-body signature always
		// binds the actual bot key and may never be skipped.
		r.Header.Set("X-Signature-Timestamp", stamp)
		r.Header.Set("X-Signature-Ed25519", sig)
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	if callback(challenge, "recipient231", false).Code != 400 {
		t.Fatal("bad signature accepted")
	}
	if callback("hello", "stranger231", true).Code != 200 || data(call("GET", "qqbot", ""))["bound"] != false {
		t.Fatal("unsolicited message bound")
	}
	if callback(challenge, "recipient231", true).Code != 200 {
		t.Fatal("signed binding failed")
	}
	v = data(call("GET", "qqbot", ""))
	if v["bound"] != true || v["revision"] != "2" {
		t.Fatalf("binding missing %#v", v)
	}
	callback(challenge, "other231", true)
	if data(call("GET", "qqbot", ""))["revision"] != "2" {
		t.Fatal("replayed callback rebound")
	}
	v = data(call("PUT", "qqbot", body("2", "replace", true)))
	if v["bound"] != false || v["revision"] != "3" {
		t.Fatal("rotation retained binding")
	}
	callback(challenge, "other231", true)
	if data(call("GET", "qqbot", ""))["bound"] != false {
		t.Fatal("old challenge survived rotation")
	}
	a = data(call("POST", "qqbot/binding", `{"expected_revision":"3"}`))
	id := a["id"].(string)
	challenge = a["challenge"].(string)
	data(call("DELETE", "qqbot/binding/"+id, ""))
	callback(challenge, "other231", true)
	if data(call("GET", "qqbot", ""))["bound"] != false {
		t.Fatal("cancelled binding completed")
	}
	// The cancellation audit is authoritative even if mutable delivery state
	// is rolled back to NULL in a restored database row.
	if _, e := f.p.Exec(t.Context(), "UPDATE qq_binding_attempts231 SET consumed_at=NULL WHERE id=$1", id); e != nil {
		t.Fatal(e)
	}
	callback(challenge, "other231", true)
	if data(call("GET", "qqbot", ""))["bound"] != false {
		t.Fatal("cancelled window resurrected")
	}
	if _, e := f.p.Exec(t.Context(), "UPDATE qq_binding_attempts231 SET consumed_at=clock_timestamp() WHERE id=$1", id); e != nil {
		t.Fatal(e)
	}
	a = data(call("POST", "qqbot/binding", `{"expected_revision":"3"}`))
	challenge = a["challenge"].(string)
	d := sha256.Sum256([]byte(f.token))
	if _, e := f.p.Exec(t.Context(), "DELETE FROM console_sessions WHERE token_hash=$1", d[:]); e != nil {
		t.Fatal(e)
	}
	callback(challenge, "other231", true)
	var boundRev int64
	if e := f.p.QueryRow(t.Context(), "SELECT revision FROM notice_accounts231 WHERE kind='qqbot'").Scan(&boundRev); e != nil || boundRev != 3 {
		t.Fatal("revoked originating session still authorized binding", e)
	}
	var audit string
	if e := f.p.QueryRow(t.Context(), "SELECT COALESCE(string_agg(changes::text,''),'') FROM backoffice_audit WHERE resource='notice-accounts231'").Scan(&audit); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(audit, "synthetic-secret231") || strings.Contains(audit, challenge) || strings.Contains(audit, "recipient231") {
		t.Fatal("private binding data leaked to audit")
	}
}

func TestNotice231RobotPhoneOnlyAndFailClosedVault(t *testing.T) {
	f := channel168Local(t)
	call := func(method, kind, body, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/admin/settings/notice-accounts/"+kind, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	save := `{"expected_revision":"0","enabled":false,"credential_action":"replace","app_id":"","secret":"","phone":"13800138231","token_source":""}`
	if call("PUT", "robot", save, "").Code != 403 {
		t.Fatal("CSRF bypass")
	}
	w := call("PUT", "robot", save, f.csrf)
	if w.Code != 200 || strings.Contains(w.Body.String(), "13800138231") {
		t.Fatal("phone-only target failed or leaked", w.Code)
	}
	if call("PUT", "robot", `{"expected_revision":"1","enabled":true,"credential_action":"keep","app_id":"","secret":"","phone":"","token_source":""}`, f.csrf).Code != 422 {
		t.Fatal("phone-only enabled webhook")
	}
	if call("PUT", "telegram", `{"expected_revision":"0","enabled":true,"credential_action":"replace","app_id":"1234","secret":"","phone":"","token_source":"oauth"}`, f.csrf).Code != 422 {
		t.Fatal("missing shared token enabled")
	}
	var cipher []byte
	if e := f.p.QueryRow(t.Context(), "SELECT credential_cipher FROM notice_accounts231 WHERE kind='robot'").Scan(&cipher); e != nil || strings.Contains(string(cipher), "13800138231") {
		t.Fatal("unencrypted recipient", e)
	}
	if _, e := f.p.Exec(t.Context(), "DELETE FROM notice_accounts231 WHERE kind='robot'"); e != nil {
		t.Fatal(e)
	}
	if call("GET", "robot", "", f.csrf).Code != 409 {
		t.Fatal("missing managed record reverted to legacy")
	}
}
