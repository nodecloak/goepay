package server

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"goepay/internal/identity"
)

func adminSecurityCall(f channel168Fixture, token, csrf string, id int64, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/console/v1/admin/admin-users/"+strconv.FormatInt(id, 10)+"/security-reset", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func TestAdminUserSecurityResetRoute(t *testing.T) {
	f := channel168Local(t)
	// backup169 (admin169, super) is the reset target. Seed its TOTP.
	if _, e := f.p.Exec(t.Context(), `INSERT INTO console_mfa(username,role,secret,last_counter) VALUES('backup168','admin','\x1234'::bytea,3)`); e != nil {
		t.Fatal(e)
	}
	var targetID int64
	if e := f.p.QueryRow(t.Context(), "SELECT id FROM console_users WHERE username='backup168' AND role='admin'").Scan(&targetID); e != nil {
		t.Fatal(e)
	}
	// Operator is denied.
	if _, e := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('op168','admin',1680,'Op','unused-synthetic','operator')`); e != nil {
		t.Fatal(e)
	}
	opToken, opCSRF, e := f.s.Sessions.Create(identity.User{ID: 1680, Role: "admin", Username: "op168", AdminClass: "operator", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	if w := adminSecurityCall(f, opToken, opCSRF, targetID, `{"disable_totp":true}`); w.Code != 403 {
		t.Fatal("operator must be forbidden", w.Code, w.Body.String())
	}
	// Super with no fields -> 422.
	if w := adminSecurityCall(f, f.token, f.csrf, targetID, `{}`); w.Code != 422 {
		t.Fatal("empty reset must be 422", w.Code, w.Body.String())
	}
	// Super clears the target's TOTP.
	if w := adminSecurityCall(f, f.token, f.csrf, targetID, `{"disable_totp":true}`); w.Code != 200 {
		t.Fatal("super reset must succeed", w.Code, w.Body.String())
	}
	var rows int
	if e := f.p.QueryRow(t.Context(), "SELECT count(*) FROM console_mfa WHERE username='backup168'").Scan(&rows); e != nil || rows != 0 {
		t.Fatal("target TOTP not cleared", rows, e)
	}
	// No CSRF -> 403.
	if w := adminSecurityCall(f, f.token, "", targetID, `{"disable_totp":true}`); w.Code != 403 {
		t.Fatal("missing CSRF must be 403", w.Code)
	}
}
