package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"goepay/internal/backoffice"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The blacklist management page gains Pro's bulk-add dialog: 拉黑类型, a 拉黑内容
// textarea (one entry per line, batched), 有效期 天 (0 = 永久) and 备注. This
// router-level test drives POST /console/v1/admin/resources/blacklist/batch-create
// through s.Handler() so the real CSRF, admin-class and transaction behaviour is
// exercised against real tables.
func TestBlacklistBatchCreateThroughHandler(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	token, csrf, e := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1, AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	post := func(body any, anti, cookie string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/console/v1/admin/resources/blacklist/batch-create", bytes.NewReader(raw))
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: cookie})
		}
		r.Header.Set("Content-Type", "application/json")
		if anti != "" {
			r.Header.Set("X-CSRF-Token", anti)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	count := func(kind string) int {
		var n int
		if e := pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_records WHERE resource='blacklist' AND body->>'kind'=$1`, kind).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}

	// Unauthenticated: no cookie -> 401.
	if w := post(map[string]any{"kind": "ip", "lines": []string{"10.0.0.1"}, "days": 0}, csrf, ""); w.Code != 401 {
		t.Fatalf("no session = %d", w.Code)
	}
	// CSRF: authenticated but missing token -> 403.
	if w := post(map[string]any{"kind": "ip", "lines": []string{"10.0.0.1"}, "days": 0}, "", token); w.Code != 403 {
		t.Fatalf("missing CSRF = %d", w.Code)
	}

	// (1) Valid batch with days -> expires_at ~ now+days, three IP rows created.
	body := map[string]any{"kind": "ip", "lines": []string{"10.0.0.1", "10.0.0.2", "10.0.0.0/24"}, "days": 2, "remark": "批量风控"}
	w := post(body, csrf, token)
	if w.Code != 201 {
		t.Fatalf("valid batch = %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			Created   []string `json:"created"`
			Skipped   int      `json:"skipped"`
			ExpiresAt string   `json:"expires_at"`
		}
	}
	if e = json.Unmarshal(w.Body.Bytes(), &created); e != nil {
		t.Fatal(e)
	}
	if len(created.Data.Created) != 3 || created.Data.Skipped != 0 {
		t.Fatalf("created=%v skipped=%d", created.Data.Created, created.Data.Skipped)
	}
	expiry, e := time.Parse(time.RFC3339, created.Data.ExpiresAt)
	if e != nil || time.Until(expiry) < 47*time.Hour || time.Until(expiry) > 49*time.Hour {
		t.Fatalf("days->expires_at wrong: %s %v", created.Data.ExpiresAt, e)
	}
	if count("ip") != 3 {
		t.Fatalf("ip rows=%d want 3", count("ip"))
	}
	// Stored rows are enabled and carry the expiry and remark the single create would.
	var n int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_records WHERE resource='blacklist' AND body->>'enabled'='true' AND body->>'expires_at'=$1 AND body->>'reason'='批量风控'`, created.Data.ExpiresAt).Scan(&n); e != nil || n != 3 {
		t.Fatalf("stored rows=%d want 3 (%v)", n, e)
	}

	// (2) days 0 -> permanent (empty expires_at).
	w = post(map[string]any{"kind": "buyer", "lines": []string{"buyer-x", "buyer-y"}, "days": 0}, csrf, token)
	if w.Code != 201 {
		t.Fatalf("permanent batch = %d %s", w.Code, w.Body.String())
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_records WHERE resource='blacklist' AND body->>'kind'='buyer' AND body->>'expires_at'=''`).Scan(&n); e != nil || n != 2 {
		t.Fatalf("permanent rows=%d want 2 (%v)", n, e)
	}

	// (3) One invalid line rejects the whole request (no partial writes) and the
	// message names the bad line. The first two IPs are valid, the third is not.
	w = post(map[string]any{"kind": "ip", "lines": []string{"10.9.9.1", "10.9.9.2", "not-an-ip"}, "days": 0}, csrf, token)
	if w.Code != 422 {
		t.Fatalf("invalid line = %d %s", w.Code, w.Body.String())
	}
	var fail struct {
		Message string `json:"message"`
	}
	json.Unmarshal(w.Body.Bytes(), &fail)
	if fail.Message == "" || !bytes.Contains([]byte(fail.Message), []byte("第 3 行")) {
		t.Fatalf("message does not name bad line: %q", fail.Message)
	}
	if count("ip") != 3 { // still only the 3 from step (1) — the batch rolled back
		t.Fatalf("partial write on invalid line: ip rows=%d want 3", count("ip"))
	}

	// (4) Duplicates: within-request repeats collapse, and a line already stored
	// as an enabled rule is skipped (not duplicated). 10.0.0.1 exists from (1);
	// 10.5.5.5 appears twice in the request.
	w = post(map[string]any{"kind": "ip", "lines": []string{"10.0.0.1", "10.5.5.5", "10.5.5.5"}, "days": 0}, csrf, token)
	if w.Code != 201 {
		t.Fatalf("dedupe batch = %d %s", w.Code, w.Body.String())
	}
	var dup struct {
		Data struct {
			Created []string `json:"created"`
			Skipped int      `json:"skipped"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &dup)
	if len(dup.Data.Created) != 1 || dup.Data.Skipped != 1 {
		t.Fatalf("dedupe: created=%v skipped=%d (want 1 created, 1 skipped)", dup.Data.Created, dup.Data.Skipped)
	}
	if count("ip") != 4 {
		t.Fatalf("after dedupe ip rows=%d want 4", count("ip"))
	}

	// (5) Every line already present -> 409 conflict, nothing created.
	if w = post(map[string]any{"kind": "ip", "lines": []string{"10.0.0.1"}, "days": 0}, csrf, token); w.Code != 409 {
		t.Fatalf("all-duplicate batch = %d %s", w.Code, w.Body.String())
	}

	// (6) Service-level guards mirror the single create / order-level path.
	svc := backoffice.New(pool, s.Store)
	if _, e = svc.BatchCreateBlacklist(ctx, backoffice.Principal{Role: "merchant", SubjectID: 1}, "ip", []string{"10.1.1.1"}, 0, ""); e != backoffice.ErrForbidden {
		t.Fatalf("merchant batch = %v want forbidden", e)
	}
	if _, e = svc.BatchCreateBlacklist(ctx, backoffice.Principal{Role: "admin", SubjectID: 1, AdminClass: "viewer"}, "ip", []string{"10.1.1.1"}, 0, ""); e != backoffice.ErrForbidden {
		t.Fatalf("non-privileged admin batch = %v want forbidden", e)
	}
	if _, e = svc.BatchCreateBlacklist(ctx, backoffice.Principal{Role: "admin", SubjectID: 1, AdminClass: "super"}, "ip", []string{"   ", ""}, 0, ""); e != backoffice.ErrInvalid {
		t.Fatalf("all-blank batch = %v want invalid", e)
	}
	if _, e = svc.BatchCreateBlacklist(ctx, backoffice.Principal{Role: "admin", SubjectID: 1, AdminClass: "super"}, "ip", []string{"10.2.2.2"}, 40000, ""); e != backoffice.ErrInvalid {
		t.Fatalf("over-long days = %v want invalid", e)
	}
}
