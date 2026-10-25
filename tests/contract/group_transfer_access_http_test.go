package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOriginalGroupTransferAccessHTTPCapabilitiesSeparateBusinessAndExecution(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	var gid int64
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"本地权限144","user_transfer":"1"}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{group_id}',to_jsonb($1::bigint::text))||'{"transfer_api_enabled":true}'::jsonb WHERE id=1`, gid); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"false"}')`); e != nil {
		t.Fatal(e)
	}
	token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/console/v1/merchant/transfers/capabilities", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var out struct{ Data map[string]any }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("capabilities unavailable %d %s", w.Code, w.Body.String())
	}
	d := out.Data
	if d["platform_enabled"] != false || d["effective_enabled"] != true || d["permission_source"] != "group" || d["effective_group_id"] != fmt.Sprint(gid) || d["writes_enabled"] != false || d["merchant_enabled"] != true {
		t.Fatalf("group confused ordinary permission with runtime execution %+v", d)
	}
	if _, e = pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','false') WHERE id=$1`, gid); e != nil {
		t.Fatal(e)
	}
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("disabled group capability unavailable %d %s", w.Code, w.Body.String())
	}
	if out.Data["effective_enabled"] != false || out.Data["permission_source"] != "group_disabled" || out.Data["platform_enabled"] != false || out.Data["writes_enabled"] != false {
		t.Fatalf("disabled group still advertised ordinary permission %+v", out.Data)
	}
	var n int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM transfers)+(SELECT count(*) FROM backoffice_audit)`).Scan(&n); e != nil || n != 0 {
		t.Fatalf("capability read wrote business state %d %v", n, e)
	}
}
