package contract_test

import (
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminMerchantLoginLogUXVerifiedTargetAndReadOnlyRoleBoundary(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO console_login_audit(username,role,ip,success,subject_id,auth_version,source_transaction) VALUES
('same-name','merchant','192.0.2.1',true,1,1,pg_current_xact_id()),
('same-name','merchant','192.0.2.2',true,2,1,pg_current_xact_id()),
('same-name','merchant','192.0.2.3',true,NULL,NULL,NULL),
('same-name','merchant','192.0.2.4',false,NULL,NULL,NULL),
('same-name','admin','192.0.2.5',true,NULL,NULL,NULL);
INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) SELECT 'operator-logs','admin',2,'operator',password_hash,'operator' FROM console_users WHERE username='admin' AND role='admin'`)
	if err != nil {
		t.Fatal(err)
	}
	admin, csrf, err := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if err != nil {
		t.Fatal(err)
	}
	operator, _, err := s.Sessions.Create(identity.User{ID: 2, Username: "operator-logs", Role: "admin", AdminClass: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	merchant, _, err := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	get := func(method, path, role, token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/console/v1/"+role+path, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	for _, token := range []string{admin, operator} {
		for _, target := range []struct{ id, ip string }{{"1", "192.0.2.1"}, {"2", "192.0.2.2"}} {
			w := get("GET", "/merchants/"+target.id+"/login-log?limit=1", "admin", token)
			var out struct {
				Data struct {
					Items []struct{ IP, Type, City string }
					Total int
				}
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.Total != 1 || len(out.Data.Items) != 1 || out.Data.Items[0].IP != target.ip || out.Data.Items[0].Type != "" || out.Data.Items[0].City != "" {
				t.Fatalf("admin target log %s %d %s", target.id, w.Code, w.Body.String())
			}
		}
	}
	for _, path := range []string{"/merchants/01/login-log", "/merchants/0/login-log", "/merchants/9223372036854775808/login-log", "/merchants/1/login-log?merchant_id=2", "/merchants/1/login-log?uid=2", "/merchants/1/login-log?limit=1&limit=2", "/merchants/1/login-log?page=01"} {
		if w := get("GET", path, "admin", admin); w.Code != 400 {
			t.Fatalf("ambiguous target/query %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	if w := get("GET", "/merchants/999999/login-log", "admin", admin); w.Code != 404 {
		t.Fatalf("nonexistent merchant %d", w.Code)
	}
	if w := get("POST", "/merchants/1/login-log", "admin", admin); w.Code != 405 {
		t.Fatalf("write method accepted %d", w.Code)
	}
	if w := get("GET", "/merchants/2/login-log", "merchant", merchant); w.Code != 403 {
		t.Fatalf("merchant crossed admin log scope %d", w.Code)
	}
	var audits, journals, events int
	if err = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM backoffice_audit),(SELECT count(*) FROM journals),(SELECT count(*) FROM console_login_audit)").Scan(&audits, &journals, &events); err != nil || audits+journals != 0 || events != 5 {
		t.Fatalf("read mutated facts %d %d %d %v", audits, journals, events, err)
	}
}
