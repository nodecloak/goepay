package contract_test

import (
	"context"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
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
