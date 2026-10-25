package contract_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
)

func TestApplicationAuthorizationHTTPRequiresOriginalSuper(t *testing.T) {
	s, p, _ := fixture(t)
	s.Sandbox = false
	s.Sessions = identity.NewPersistentSessions(p, true)
	token, csrf, e := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	merchant, merchantCSRF, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	h := s.Handler()
	base := "/console/v1/admin/application-authorizations"
	if w := lookupHTTP(t, h, "GET", "/console/v1/merchant/application-authorizations/capabilities?merchant_id=1", "", merchant, "merchant", merchantCSRF); w.Code != 403 {
		t.Fatal("merchant application authority", w.Code, w.Body.String())
	}
	for _, query := range []string{"", "?merchant_id=01", "?merchant_id=1&merchant_id=1", "?merchant_id=1&issuer_app_id=forged"} {
		if w := lookupHTTP(t, h, "GET", base+"/capabilities"+query, "", token, "admin", csrf); w.Code != 400 {
			t.Fatal("ambiguous capabilities", query, w.Code)
		}
	}
	r := httptest.NewRequest("GET", base+"/capabilities?merchant_id=1", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("duplicate owner cookie accepted", w.Code)
	}
	if w := lookupHTTP(t, h, "POST", base, `{"merchant_id":"1","profile_id":"1","profile_revision":"1"}`, token, "admin", ""); w.Code != 403 {
		t.Fatal("missing CSRF", w.Code)
	}
	ctx := context.Background()
	tx, e := p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	delegated, delegatedCSRF, _, e := s.Sessions.CreateDelegated(ctx, tx, identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1}, identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}, token)
	if e != nil {
		tx.Rollback(ctx)
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if w := lookupHTTP(t, h, "GET", "/console/v1/merchant/application-authorizations/capabilities?merchant_id=1", "", delegated, "merchant", delegatedCSRF); w.Code != 403 {
		t.Fatal("delegated application authority", w.Code, w.Body.String())
	}
	if _, e = p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) SELECT 'application-auth-backup','admin',12,'Synthetic backup',password_hash,'super' FROM console_users WHERE username='admin';UPDATE console_users SET admin_class='operator' WHERE username='admin'`); e != nil {
		t.Fatal(e)
	}
	if w := lookupHTTP(t, h, "GET", base+"/capabilities?merchant_id=1", "", token, "admin", csrf); w.Code != 401 {
		t.Fatal("demotion did not revoke original super session", w.Code, w.Body.String())
	}
	var operatorVersion int64
	if e = p.QueryRow(ctx, `SELECT auth_version FROM console_users WHERE username='admin'`).Scan(&operatorVersion); e != nil {
		t.Fatal(e)
	}
	operator, operatorCSRF, e := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: operatorVersion, AdminClass: "operator"})
	if e != nil {
		t.Fatal(e)
	}
	if w := lookupHTTP(t, h, "GET", base+"/capabilities?merchant_id=1", "", operator, "admin", operatorCSRF); w.Code != 403 {
		t.Fatal("fresh operator read application profiles", w.Code, w.Body.String())
	}
	var n int
	if e = p.QueryRow(ctx, `SELECT count(*) FROM application_authorization_requests`).Scan(&n); e != nil || n != 0 {
		t.Fatal("policy failure created request", n, e)
	}
	if strings.Contains(w.Body.String(), "token") {
		t.Fatal("cookie error leaked secret")
	}
}
