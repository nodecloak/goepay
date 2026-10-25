package contract_test

import (
	"context"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestOperationsRetryIsAuditedAndCannotReplayRunningJob(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	var id int64
	if err := pool.QueryRow(ctx, "INSERT INTO jobs(event_key,kind,payload,state,attempts) VALUES('ops-test','payment.reconcile','{}','dead',12) RETURNING id").Scan(&id); err != nil {
		t.Fatal(err)
	}
	token, csrf, _ := s.Sessions.Create(identity.User{Username: "admin", ID: 1, Role: "admin"})
	call := func(path, body string) int {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	path := "/console/v1/admin/operations/jobs/" + strconv.FormatInt(id, 10) + "/retry"
	if code := call(path, "{}"); code != 200 {
		t.Fatalf("retry %d", code)
	}
	if code := call(path, "{}"); code != 409 {
		t.Fatalf("repeat %d", code)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE resource='jobs' AND operation='retry'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit %d %v", n, err)
	}
	if code := call("/console/v1/admin/operations/cleanup", `{"preview":false,"cutoff":"2099-01-01T00:00:00Z"}`); code != 400 {
		t.Fatalf("futurecleanup %d", code)
	}
}
