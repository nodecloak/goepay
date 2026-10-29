package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func profile198Get(t *testing.T, f *email197Fixture, path string) map[string]any {
	t.Helper()
	r := httptest.NewRequest("GET", "/console/v1/merchant/account/profile"+path, nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return email197Data(t, w)
}
func TestProfile198LargeUIDRemainsExactAndReadonlyFieldsCannotBeWritten(t *testing.T) {
	f := email197Local(t)
	const mid int64 = 9007199254740993
	if e := f.s.Store.CreateMerchant(t.Context(), payment.Merchant{ID: mid, Name: "Big profile198", APIKey: "synthetic198-account-api-key-never-live", Enabled: true}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) SELECT 'big198','merchant',$1,'Big profile198',password_hash FROM console_users WHERE username='email197' AND role='merchant'`, mid); e != nil {
		t.Fatal(e)
	}
	var e error
	f.token, f.csrf, e = f.s.Sessions.Create(identity.User{ID: mid, Role: "merchant", Username: "big198", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	v := profile198Get(t, f, "")
	if v["uid"] != "9007199254740993" {
		t.Fatal("profile identity rounded", v["uid"])
	}
	r := httptest.NewRequest("PATCH", "/console/v1/merchant/account/profile", strings.NewReader(`{"name":"tampered198","email":"","uid":"1","addtime":"2000-01-01 00:00:00","registration_time_known":false}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 400 && w.Code != 422 {
		t.Fatal("readonly profile fields accepted", w.Code)
	}
	after := profile198Get(t, f, "")
	if after["uid"] != v["uid"] || after["addtime"] != v["addtime"] || after["name"] != "Big profile198" {
		t.Fatal("rejected readonly request changed account facts", after)
	}
}
func TestProfile198OriginalReadonlyUIDAndRegistrationTimeUseAuthenticatedMerchant(t *testing.T) {
	f := email197Local(t)
	var born time.Time
	if e := f.p.QueryRow(t.Context(), `SELECT created_at FROM merchants WHERE id=1`).Scan(&born); e != nil {
		t.Fatal(e)
	}
	v := profile198Get(t, f, "?uid=9223372036854775807")
	if v["uid"] != "1" || v["registration_time_known"] != true {
		t.Fatal("original UID/registration fields missing, invented, or selected by caller", v)
	}
	text, ok := v["addtime"].(string)
	if !ok {
		t.Fatal("original registration timestamp missing", v)
	}
	reported, e := time.ParseInLocation("2006-01-02 15:04:05", text, time.FixedZone("UTC+8", 8*3600))
	if e != nil || reported.Unix() != born.Unix() {
		t.Fatal("displayed registration time differs from immutable merchant birth", text, e)
	}
	if v["name"] == nil || v["email"] == nil || v["pay"] == nil {
		t.Fatal("old profile contract lost", v)
	}
}

// a7 F2: the original editinfo.php carries a merchant contact phone. The
// merchant's own profile must read it back and accept a plain edit held to the
// same contact-phone shape the admin merchant directory enforces, rejecting a
// malformed value without disturbing the rest of the profile.
func profile198Patch(t *testing.T, f *email197Fixture, body string) int {
	t.Helper()
	r := httptest.NewRequest("PATCH", "/console/v1/merchant/account/profile", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w.Code
}
func TestProfile198ContactPhoneRoundTripsAndRejectsMalformed(t *testing.T) {
	f := email197Local(t)
	if v := profile198Get(t, f, ""); v["phone"] != "" {
		t.Fatal("new profile contract missing empty phone field", v)
	}
	if code := profile198Patch(t, f, `{"name":"Email197","email":"","phone":"13800138000"}`); code != 200 {
		t.Fatal("valid contact phone rejected", code)
	}
	after := profile198Get(t, f, "")
	if after["phone"] != "13800138000" || after["name"] != "Email197" {
		t.Fatal("contact phone did not round-trip", after)
	}
	if code := profile198Patch(t, f, `{"name":"Email197","email":"","phone":"not-a-phone"}`); code != 422 {
		t.Fatal("malformed contact phone accepted", code)
	}
	unchanged := profile198Get(t, f, "")
	if unchanged["phone"] != "13800138000" {
		t.Fatal("rejected phone edit mutated stored contact phone", unchanged)
	}
	if code := profile198Patch(t, f, `{"name":"Email197","email":"","phone":""}`); code != 200 {
		t.Fatal("clearing contact phone rejected", code)
	}
	if cleared := profile198Get(t, f, ""); cleared["phone"] != "" {
		t.Fatal("contact phone was not cleared", cleared)
	}
}
