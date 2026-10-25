package contract_test

import (
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRuntimeLogUXOriginalDateColumnsPermissionsAndStableCleanup(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, token, csrf, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/console/v1/admin/operations/runtime-log"+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Now().In(zone)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	date := day.Format("2006-01-02")
	w := call("GET", token, "", "?date="+date, "")
	if w.Code != 200 {
		t.Fatalf("runtime page must have its own event source: %d %s", w.Code, w.Body.String())
	}
	for _, at := range []time.Time{day.Add(-time.Second), day.Add(time.Hour), day.Add(23 * time.Hour), day.AddDate(0, 0, 1)} {
		if _, e = pool.Exec(ctx, `INSERT INTO runtime_events(created_at,level,content,error_code) VALUES($1,'error','本地运行记录','INTERNAL')`, at); e != nil {
			t.Fatal(e)
		}
	}
	w = call("GET", token, "", "?date="+date, "")
	var list struct {
		Data struct {
			Items []map[string]any
			Total int
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || list.Data.Total != 2 || len(list.Data.Items) != 2 || list.Data.Items[0]["type"] != "error" || list.Data.Items[0]["content"] != "本地运行记录" || list.Data.Items[0]["time"] == nil {
		t.Fatalf("original date/three columns %d %s", w.Code, w.Body.String())
	}
	if _, e = pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) SELECT 'runtime-operator','admin',2,'operator',password_hash,'operator' FROM console_users WHERE username='admin' AND role='admin'`); e != nil {
		t.Fatal(e)
	}
	operator, _, e := s.Sessions.Create(identity.User{ID: 2, Username: "runtime-operator", Role: "admin", AdminClass: "operator"})
	if e != nil {
		t.Fatal(e)
	}
	if w = call("GET", operator, "", "?date="+date, ""); w.Code != 403 {
		t.Fatalf("super-only runtime read %d", w.Code)
	}
	for _, q := range []string{"?date=2026-02-30", "?date=" + date + "&date=" + date, "?date=" + date + "&page=01", "?date=" + date + "&path=../../runtime"} {
		if w = call("GET", token, "", q, ""); w.Code != 400 {
			t.Fatalf("invalid log filter %q %d", q, w.Code)
		}
	}
	cutoff := day.AddDate(0, 0, -30)
	var old int64
	if e = pool.QueryRow(ctx, `INSERT INTO runtime_events(created_at,level,content,error_code) VALUES($1,'info','旧运行记录','INTERNAL') RETURNING id`, cutoff.Add(-time.Second)).Scan(&old); e != nil {
		t.Fatal(e)
	}
	w = call("GET", token, "", "/cleanup?days=30", "")
	var preview struct {
		Data struct {
			Cutoff string
			Count  string
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || preview.Data.Count != "1" {
		t.Fatalf("cleanup preview %d %s", w.Code, w.Body.String())
	}
	bodyBytes, _ := json.Marshal(map[string]any{"days": 30, "cutoff": preview.Data.Cutoff, "external_no": "runtime-cleanup-132"})
	body := string(bodyBytes)
	if w = call("POST", token, "", "/cleanup", body); w.Code != 403 {
		t.Fatalf("cleanup missing CSRF %d", w.Code)
	}
	if w = call("POST", token, csrf, "/cleanup", body); w.Code != 200 || !strings.Contains(w.Body.String(), `"count":"1"`) {
		t.Fatalf("cleanup %d %s", w.Code, w.Body.String())
	}
	var late int64
	if e = pool.QueryRow(ctx, `INSERT INTO runtime_events(created_at,level,content,error_code) VALUES($1,'info','迟到旧记录','INTERNAL') RETURNING id`, cutoff.Add(-time.Second)).Scan(&late); e != nil {
		t.Fatal(e)
	}
	if w = call("POST", token, csrf, "/cleanup", body); w.Code != 200 || !strings.Contains(w.Body.String(), `"replayed":true`) {
		t.Fatalf("cleanup replay %d %s", w.Code, w.Body.String())
	}
	var oldArchived, lateArchived bool
	if e = pool.QueryRow(ctx, `SELECT (SELECT archived_at IS NOT NULL FROM runtime_events WHERE id=$1),(SELECT archived_at IS NOT NULL FROM runtime_events WHERE id=$2)`, old, late).Scan(&oldArchived, &lateArchived); e != nil || !oldArchived || lateArchived {
		t.Fatalf("cleanup replay expanded %v old=%v late=%v", e, oldArchived, lateArchived)
	}
	if w = call("POST", token, csrf, "/cleanup", strings.Replace(body, `"days":30`, `"days":31`, 1)); w.Code != 409 {
		t.Fatalf("cleanup changed request %d %s", w.Code, w.Body.String())
	}
	var n int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='runtime-log' AND operation='archive'`).Scan(&n); e != nil || n != 1 {
		t.Fatalf("cleanup audit %d %v", n, e)
	}
	// A real local SQL failure is recorded without exposing the SQL statement or
	// original database detail. No provider or funds operation is involved.
	if _, e = pool.Exec(ctx, `DROP VIEW provider_refund_result_invariants`); e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/console/v1/admin/refunds?"+url.Values{"limit": {"1"}}.Encode(), nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 500 {
		t.Fatalf("test failure source %d", w.Code)
	}
	var content, code string
	if e = pool.QueryRow(ctx, `SELECT content,error_code FROM runtime_events WHERE error_code='42P01' ORDER BY id DESC LIMIT 1`).Scan(&content, &code); e != nil || strings.Contains(content, "provider_refund_result_invariants") || strings.Contains(content, "SELECT") {
		t.Fatalf("unsafe/unrecorded SQL failure %q %q %v", content, code, e)
	}
}
