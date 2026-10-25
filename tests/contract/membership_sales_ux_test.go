package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestMembershipSalesUXConfigurationAndCatalog(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	var id int64
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"sale config","enabled":true,"price_cents":"100","duration_days":"30"}') RETURNING id`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) SELECT 'operator','admin',2,'Operator',password_hash,'operator' FROM console_users WHERE username='admin' AND role='admin'`); e != nil {
		t.Fatal(e)
	}
	super, session, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	operator, op, e := s.Sessions.Create(identity.User{ID: 2, Username: "operator", Role: "admin", AdminClass: "operator"})
	if e != nil {
		t.Fatal(e)
	}
	merchant, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	request := func(role, token, csrf, method string, body any, paths ...string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		path := "/membership-sales"
		if len(paths) > 0 {
			path = paths[0]
		}
		r := httptest.NewRequest(method, "/console/v1/"+role+path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := request("admin", super, session, "GET", nil)
	if w.Code != 200 {
		t.Fatalf("configuration page %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Data struct {
			Revision string
			Enabled  bool
			Groups   []map[string]any
		}
	}
	if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil || result.Data.Revision == "" || len(result.Data.Groups) != 1 {
		t.Fatalf("configuration %+v %v", result, e)
	}
	body := map[string]any{"expected_revision": result.Data.Revision, "enabled": true, "groups": []any{map[string]any{"id": strconv.FormatInt(id, 10), "sale_enabled": true, "price_cents": "299", "duration_months": 0, "sort": 2, "visible": "0", "description": "永久组"}}}
	if w = request("admin", operator, op, "POST", body); w.Code != 403 {
		t.Fatalf("operator modified sale policy: %d", w.Code)
	}
	if w = request("admin", super, "wrong", "POST", body); w.Code != 403 {
		t.Fatalf("missing CSRF accepted: %d", w.Code)
	}
	w = request("admin", super, session, "POST", body)
	if w.Code != 200 {
		t.Fatalf("save %d %s", w.Code, w.Body.String())
	}
	if w = request("admin", super, session, "POST", body); w.Code != 409 {
		t.Fatalf("stale config accepted: %d", w.Code)
	}
	w = request("merchant", merchant, "", "GET", nil)
	if w.Code != 200 {
		t.Fatalf("catalog %d %s", w.Code, w.Body.String())
	}
	var catalog struct {
		Data struct{ Items []map[string]any }
	}
	json.Unmarshal(w.Body.Bytes(), &catalog)
	if len(catalog.Data.Items) != 1 || catalog.Data.Items[0]["price_cents"] != "299" || catalog.Data.Items[0]["permanent"] != true {
		t.Fatalf("catalog did not consume config: %s", w.Body.String())
	}
	var changed map[string]any
	if e = pool.QueryRow(ctx, "SELECT body FROM backoffice_records WHERE id=$1", id).Scan(&changed); e != nil {
		t.Fatal(e)
	}
	if changed["name"] != "sale config" || changed["duration_days"] != "30" {
		t.Fatalf("unrelated fields lost: %v", changed)
	}
	var audit int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE resource='membership-sales' AND operation='configure'").Scan(&audit); e != nil || audit != 1 {
		t.Fatalf("audit %d %v", audit, e)
	}
	var globalID int64
	if e = pool.QueryRow(ctx, "SELECT id FROM backoffice_records WHERE resource='settings' AND body->>'key'='group_buy'").Scan(&globalID); e != nil {
		t.Fatal(e)
	}
	globalPath := "/resources/settings/" + strconv.FormatInt(globalID, 10)
	if w = request("admin", super, session, "PUT", map[string]any{"value": "false"}, globalPath); w.Code != 403 {
		t.Fatalf("generic settings bypassed sales lock/version: %d %s", w.Code, w.Body.String())
	}
	if w = request("admin", super, session, "DELETE", nil, globalPath); w.Code != 403 && w.Code != 404 {
		t.Fatalf("generic delete bypassed sales policy: %d", w.Code)
	}
}
