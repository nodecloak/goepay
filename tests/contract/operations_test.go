package contract_test

import (
	"context"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
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

// TestCleanupPrunesLoginAuditByAge proves the A6-02 remediation: the data-cleanup
// page prunes aged console_login_audit rows (preview counts, commit deletes) and
// writes an audit row, while recent login-audit rows survive.
func TestCleanupPrunesLoginAuditByAge(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	// Two aged rows (100 days) and one recent row.
	if _, err := pool.Exec(ctx, "INSERT INTO console_login_audit(username,role,ip,success,created_at) VALUES('old1','admin','127.0.0.1',true,now()-interval '100 days'),('old2','merchant','127.0.0.1',false,now()-interval '100 days'),('fresh','admin','127.0.0.1',true,now())"); err != nil {
		t.Fatal(err)
	}
	token, csrf, _ := s.Sessions.Create(identity.User{Username: "admin", ID: 1, Role: "admin"})
	cutoff := time.Now().AddDate(0, 0, -30).UTC().Format(time.RFC3339)
	call := func(preview bool) string {
		body := `{"preview":` + strconv.FormatBool(preview) + `,"cutoff":"` + cutoff + `"}`
		r := httptest.NewRequest("POST", "/console/v1/admin/operations/cleanup", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("cleanup status %d body %s", w.Code, w.Body)
		}
		return w.Body.String()
	}
	// Preview counts the two aged rows and deletes nothing.
	if body := call(true); !strings.Contains(body, "\"expired_login_audit_rows\":2") {
		t.Fatalf("preview must count 2 aged rows, got %s", body)
	}
	var before int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM console_login_audit").Scan(&before); err != nil || before != 3 {
		t.Fatalf("preview must not delete: count=%d err=%v", before, err)
	}
	// Commit deletes the two aged rows; the recent row survives.
	if body := call(false); !strings.Contains(body, "\"expired_login_audit_rows\":2") {
		t.Fatalf("commit must report 2 deleted, got %s", body)
	}
	var remaining int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM console_login_audit").Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("only the recent row must remain: count=%d err=%v", remaining, err)
	}
	var fresh bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM console_login_audit WHERE username='fresh')").Scan(&fresh); err != nil || !fresh {
		t.Fatal("recent login-audit row must survive")
	}
	var audits int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE resource='cleanup' AND operation='expired-cache'").Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("cleanup must write one audit row: count=%d err=%v", audits, err)
	}
}
