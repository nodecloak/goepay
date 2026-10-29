package contract_test

import (
	"context"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRestrictedAdministratorCannotReachSuperOperations(t *testing.T) {
	s, pool, _ := fixture(t)
	if _, err := pool.Exec(context.Background(), "INSERT INTO console_users(username,role,subject_id,display_name,password_hash) SELECT 'operator','admin',7,'Operator',password_hash FROM console_users WHERE username='admin' AND role='admin'"); err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Sessions.Create(identity.User{Username: "operator", Role: "admin", ID: 7})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{"dashboard": 200, "resources/admin-users": 403, "resources/settings": 403, "operations/jobs": 403, "operations/cleanup": 403, "operations/login-log": 200} {
		r := httptest.NewRequest("GET", "/console/v1/admin/"+path, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("operator %s status%d want%d body%s", path, w.Code, want, w.Body)
		}
	}
}

// TestRestrictedOperatorEnforcedByGrantSet proves the per-menu permission model:
// an operator with an explicit grant set reaches granted pages (200) and is
// refused ungranted business pages (403), while still reaching its own session.
func TestRestrictedOperatorEnforcedByGrantSet(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class,permissions) SELECT 'restricted','admin',8,'Restricted',password_hash,'operator','[\"OrderList\",\"SystemLog\"]'::jsonb FROM console_users WHERE username='admin' AND role='admin'"); err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Sessions.Create(identity.User{Username: "restricted", Role: "admin", ID: 8})
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{
		"orders":                200, // granted OrderList
		"operations/login-log":  200, // granted SystemLog
		"session":               200, // self-service always allowed
		"reports/channels":      403, // ungranted ChannelStat
		"resources/groups":      403, // ungranted GroupList
		"resources/merchants":   403, // ungranted MerchantList
		"resources/admin-users": 403, // super-only, never grantable
	} {
		r := httptest.NewRequest("GET", "/console/v1/admin/"+path, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("restricted %s status%d want%d body%s", path, w.Code, want, w.Body)
		}
	}
	// The session response advertises the granted keys so the web hides menus.
	r := httptest.NewRequest("GET", "/console/v1/admin/session", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	body := w.Body.String()
	if !strings.Contains(body, "OrderList") || !strings.Contains(body, "SystemLog") {
		t.Fatalf("session must expose granted permissions, got %s", body)
	}
}

// TestAdminUserEditorPersistsPermissionGrant proves the Pro admin-user editor
// equivalent: creating an operator with a permission set stores it, an unknown
// key is rejected, and pinning a grant set onto a super row is refused.
func TestAdminUserEditorPersistsPermissionGrant(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	token, csrf, _ := s.Sessions.Create(identity.User{Username: "admin", ID: 1, Role: "admin"})
	call := func(path, body string) (int, string) {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	// Valid grant set stored.
	if code, body := call("/console/v1/admin/resources/admin-users", `{"username":"ops1","display_name":"Ops One","password":"operator-pass-1234","admin_class":"operator","permissions":["OrderList","DailyStat"]}`); code != 200 && code != 201 {
		t.Fatalf("create operator %d %s", code, body)
	}
	var stored []byte
	if err := pool.QueryRow(ctx, "SELECT permissions FROM console_users WHERE username='ops1' AND role='admin'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored), "OrderList") || !strings.Contains(string(stored), "DailyStat") {
		t.Fatalf("grant set not stored: %s", stored)
	}
	// Unknown key rejected.
	if code, _ := call("/console/v1/admin/resources/admin-users", `{"username":"ops2","display_name":"Ops Two","password":"operator-pass-1234","admin_class":"operator","permissions":["NotAMenu"]}`); code != 422 {
		t.Fatalf("unknown key must be rejected, got %d", code)
	}
	// Pinning a grant set onto a super row is refused.
	if code, _ := call("/console/v1/admin/resources/admin-users", `{"username":"ops3","display_name":"Ops Three","password":"operator-pass-1234","admin_class":"super","permissions":["OrderList"]}`); code != 422 {
		t.Fatalf("super with permissions must be rejected, got %d", code)
	}
}
