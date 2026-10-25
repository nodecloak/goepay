package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAuditedSSOGrantIsBoundSingleUseAndIssuerRevoked(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(pool, false)
	admin := identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := s.Sessions.Create(admin)
	if e != nil {
		t.Fatal(e)
	}
	invoke := func(method, path, body, cookie, proof string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if cookie != "" {
			r.Header.Set("Cookie", cookie)
		}
		r.Header.Set("X-CSRF-Token", proof)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	adminCookie := "goepay_admin=" + token
	data := func(w *httptest.ResponseRecorder) map[string]any {
		var out struct {
			Data map[string]any `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		return out.Data
	}
	issue := func() string {
		w := invoke("POST", "/console/v1/admin/sso/grants", `{"merchant_id":"1"}`, adminCookie, csrf)
		if w.Code != 201 {
			t.Fatalf("SSO issue status %d", w.Code)
		}
		return data(w)["grant"].(string)
	}
	grant := issue()
	if w := invoke("POST", "/console/v1/admin/sso/consume", fmt.Sprintf(`{"grant":%q}`, grant), adminCookie, "invalid"); w.Code != 403 {
		t.Fatalf("SSO CSRF %d", w.Code)
	}
	// Another valid session for the same administrator cannot consume this grant.
	other, otherCS, e := s.Sessions.Create(admin)
	if e != nil {
		t.Fatal(e)
	}
	if w := invoke("POST", "/console/v1/admin/sso/consume", fmt.Sprintf(`{"grant":%q}`, grant), "goepay_admin="+other, otherCS); w.Code != 409 {
		t.Fatalf("grant moved to another session %d", w.Code)
	}
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- invoke("POST", "/console/v1/admin/sso/consume", fmt.Sprintf(`{"grant":%q}`, grant), adminCookie, csrf)
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	merchantCookie := ""
	merchantCSRF := ""
	for w := range results {
		if w.Code == 200 {
			winners++
			merchantCSRF = data(w)["csrf_token"].(string)
			for _, c := range w.Result().Cookies() {
				if c.Name == "goepay_merchant" {
					merchantCookie = c.Name + "=" + c.Value
					if c.MaxAge > 900 || !c.HttpOnly || c.Path != "/console/v1/merchant" {
						t.Fatal("unsafe delegated cookie")
					}
				}
			}
		} else if w.Code != 409 {
			t.Fatalf("SSO race status %d", w.Code)
		}
	}
	if winners != 1 || merchantCookie == "" {
		t.Fatal("grant did not create one session", winners)
	}
	w := invoke("GET", "/console/v1/merchant/session", "", merchantCookie, "")
	if w.Code != 200 || data(w)["delegated_by"] == nil {
		t.Fatalf("delegated identity absent %d", w.Code)
	}
	// Every delegated write has a separate audit of the actual administrator.
	w = invoke("POST", "/console/v1/merchant/account/profile", `{}`, merchantCookie, merchantCSRF)
	var audits int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE actor_role='admin' AND actor_id=1 AND operation='delegated-request'").Scan(&audits); e != nil || audits != 1 {
		t.Fatal("actual issuer not audited", audits, e)
	}
	if _, e = pool.Exec(ctx, "ALTER TABLE backoffice_audit RENAME TO sso_audit_hidden"); e != nil {
		t.Fatal(e)
	}
	if w = invoke("POST", "/console/v1/merchant/account/profile", `{}`, merchantCookie, merchantCSRF); w.Code != 503 {
		t.Fatalf("delegated write bypassed audit failure %d", w.Code)
	}
	if _, e = pool.Exec(ctx, "ALTER TABLE sso_audit_hidden RENAME TO backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	s.Sessions.Delete(token)
	if w = invoke("GET", "/console/v1/merchant/session", "", merchantCookie, ""); w.Code != 401 {
		t.Fatalf("issuer logout left delegated authority %d", w.Code)
	}
}

func TestSSOExpiryIsRecheckedAfterGrantRowLockWait(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(pool, false)
	token, csrf, e := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	invoke := func(action, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/console/v1/admin/sso/"+action, strings.NewReader(body))
		r.Header.Set("Cookie", "goepay_admin="+token)
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := invoke("grants", `{"merchant_id":"1"}`)
	if w.Code != 201 {
		t.Fatalf("issue %d", w.Code)
	}
	var out struct {
		Data struct {
			Grant string `json:"grant"`
		} `json:"data"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	var expires time.Time
	if e = pool.QueryRow(ctx, "UPDATE console_sso_grants SET expires_at=clock_timestamp()+interval '1 second' RETURNING expires_at").Scan(&expires); e != nil {
		t.Fatal(e)
	}
	locker, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer locker.Rollback(ctx)
	var lockerPID int
	if e = locker.QueryRow(ctx, "SELECT pg_backend_pid() FROM console_sso_grants FOR UPDATE").Scan(&lockerPID); e != nil {
		t.Fatal(e)
	}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- invoke("consume", fmt.Sprintf(`{"grant":%q}`, out.Data.Grant)) }()
	blocked := false
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if e = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid)))", lockerPID).Scan(&blocked); e != nil {
			t.Fatal(e)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("consume never waited for held grant row")
	}
	expired := false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		if e = locker.QueryRow(ctx, "SELECT clock_timestamp()>=expires_at FROM console_sso_grants").Scan(&expired); e != nil {
			t.Fatal(e)
		}
		if expired {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !expired {
		t.Fatal("database did not observe grant expiry while locked")
	}
	if e = locker.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case w = <-result:
	case <-time.After(3 * time.Second):
		t.Fatal("consume did not finish after row unlock")
	}
	if w.Code != 409 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("expired grant survived row wait: %d", w.Code)
	}
}

func TestSSORequiresSuperCurrentTargetAndAtomicAudit(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(pool, false)
	admin := identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := s.Sessions.Create(admin)
	if e != nil {
		t.Fatal(e)
	}
	invoke := func(action, body, token, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/console/v1/admin/sso/"+action, strings.NewReader(body))
		r.Header.Set("Cookie", "goepay_admin="+token)
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := invoke("grants", `{"merchant_id":"1"}`, token, ""); w.Code != 403 {
		t.Fatalf("grant CSRF ignored %d", w.Code)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('sso-operator','admin',42,'operator','unused','operator')"); e != nil {
		t.Fatal(e)
	}
	op, opCS, e := s.Sessions.Create(identity.User{Username: "sso-operator", Role: "admin", ID: 42, AuthVersion: 1, AdminClass: "operator"})
	if e != nil {
		t.Fatal(e)
	}
	if w := invoke("grants", `{"merchant_id":"1"}`, op, opCS); w.Code != 403 {
		t.Fatalf("operator delegated authority %d", w.Code)
	}
	issue := func() string {
		w := invoke("grants", `{"merchant_id":"1"}`, token, csrf)
		if w.Code != 201 {
			t.Fatalf("issue %d", w.Code)
		}
		var out struct {
			Data struct {
				Grant string `json:"grant"`
			} `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		return out.Data.Grant
	}
	grant := issue()
	if _, e = pool.Exec(ctx, "UPDATE console_users SET password_hash=password_hash||'changed' WHERE role='merchant' AND subject_id=1"); e != nil {
		t.Fatal(e)
	}
	if w := invoke("consume", fmt.Sprintf(`{"grant":%q}`, grant), token, csrf); w.Code != 409 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("stale target grant minted session %d", w.Code)
	}
	grant = issue()
	if _, e = pool.Exec(ctx, "UPDATE console_sso_grants SET created_at=statement_timestamp()-interval '2 minutes',expires_at=statement_timestamp()-interval '1 minute'"); e != nil {
		t.Fatal(e)
	}
	if w := invoke("consume", fmt.Sprintf(`{"grant":%q}`, grant), token, csrf); w.Code != 409 {
		t.Fatalf("expired grant consumed %d", w.Code)
	}
	grant = issue()
	var before, after int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM console_sso_grants").Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "ALTER TABLE backoffice_audit RENAME TO sso_issue_audit_hidden"); e != nil {
		t.Fatal(e)
	}
	if w := invoke("grants", `{"merchant_id":"1"}`, token, csrf); w.Code != 500 {
		t.Fatalf("grant bypassed failed audit %d", w.Code)
	}
	if w := invoke("consume", fmt.Sprintf(`{"grant":%q}`, grant), token, csrf); w.Code != 500 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("consume leaked session without audit %d", w.Code)
	}
	var sessions int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM console_sessions WHERE role='merchant'").Scan(&sessions); e != nil || sessions != 0 {
		t.Fatal("failed consume left delegated session", sessions, e)
	}
	if _, e = pool.Exec(ctx, "ALTER TABLE sso_issue_audit_hidden RENAME TO backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM console_sso_grants").Scan(&after); e != nil || before != after {
		t.Fatal("failed audit leaked grant", before, after, e)
	}
	w := invoke("consume", fmt.Sprintf(`{"grant":%q}`, grant), token, csrf)
	if w.Code != 200 {
		t.Fatalf("audit failure consumed grant prematurely %d", w.Code)
	}
	delegatedCookie := ""
	for _, c := range w.Result().Cookies() {
		if c.Name == "goepay_merchant" {
			delegatedCookie = c.Name + "=" + c.Value
		}
	}
	ordinary, _, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 2})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('sso-backup','admin',43,'backup','unused','super')"); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "UPDATE console_users SET admin_class='operator' WHERE username='admin' AND role='admin'"); e != nil {
		t.Fatal(e)
	}
	get := func(cookie string) int {
		r := httptest.NewRequest("GET", "/console/v1/merchant/session", nil)
		r.Header.Set("Cookie", cookie)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if status := get(delegatedCookie); status != 401 {
		t.Fatalf("issuer demotion left delegated access %d", status)
	}
	if status := get("goepay_merchant=" + ordinary); status != 200 {
		t.Fatalf("issuer demotion revoked ordinary merchant login %d", status)
	}
}
