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
)

func TestSystemLogUXOriginalSearchAndHistoricalSubjectsWithoutInference(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(token string, q url.Values) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/admin/operations/system-log?"+q.Encode(), nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := call(token, url.Values{})
	if w.Code != 200 {
		t.Fatalf("original operation log source missing: %d %s", w.Code, w.Body.String())
	}
	if _, e = pool.Exec(ctx, `SELECT setval(pg_get_serial_sequence('console_login_audit','id'),9007199254740992,true);
 INSERT INTO console_login_audit(username,role,ip,success,subject_id,auth_version,source_transaction) VALUES
 ('merchant','merchant','192.0.2.1',true,1,1,pg_current_xact_id()),
 ('merchant','merchant','192.0.2.9',false,NULL,NULL,NULL),
 ('merchant-two','merchant','192.0.2.2',true,2,1,pg_current_xact_id()),
 ('admin','admin','192.0.2.8',true,NULL,NULL,NULL);
 INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) SELECT 'system-log-operator','admin',2,'operator',password_hash,'operator' FROM console_users WHERE username='admin' AND role='admin'`); e != nil {
		t.Fatal(e)
	}
	var record int64
	if e = pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,owner_id,body) VALUES('domains',2,'{"merchant_id":"2","domain":"later-owner.test"}') RETURNING id`).Scan(&record); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES
 ('admin',1,'admin','merchants',1,'update','{}'),
 ('admin',1,'admin','domains',$1,'update','{"merchant_id":"1"}'),
 ('merchant',1,'merchant','unknown-self',0,'update','{"merchant_id":"2"}'),
 ('admin',1,'admin','groups',1,'update','{"merchant_id":"1","password":"secret-log-sentinel"}'),
 ('admin',1,'admin','unknown-resource',1,'update','{"merchant_id":"1"}'),
 ('merchant',2,'merchant-two','unknown-self',0,'update','{"merchant_id":"1"}')`, record); e != nil {
		t.Fatal(e)
	}
	fingerprint := func() string {
		t.Helper()
		var hash string
		if err := pool.QueryRow(ctx, `SELECT md5((SELECT COALESCE(string_agg(row_to_json(t)::text,'' ORDER BY id),'') FROM console_login_audit t)||(SELECT COALESCE(string_agg(row_to_json(t)::text,'' ORDER BY id),'') FROM backoffice_audit t))`).Scan(&hash); err != nil {
			t.Fatal(err)
		}
		return hash
	}
	before := fingerprint()
	read := func(q url.Values) ([]map[string]any, int) {
		t.Helper()
		w := call(token, q)
		var out struct {
			Data struct {
				Items []map[string]any
				Total int
			}
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || strings.Contains(w.Body.String(), "secret-log-sentinel") {
			t.Fatalf("unsafe original system logs %d %s", w.Code, w.Body.String())
		}
		return out.Data.Items, out.Data.Total
	}
	rows, total := read(url.Values{"column": {"uid"}, "value": {"1"}})
	if total != 4 || len(rows) != 4 {
		t.Fatalf("historical subject count: %+v total=%d", rows, total)
	}
	for _, row := range rows {
		if row["uid"] != "1" || row["subject_kind"] != "merchant" {
			t.Fatalf("wrong historical owner: %+v", row)
		}
	}
	rows, total = read(url.Values{"column": {"uid"}, "value": {"0"}})
	if total != 2 {
		t.Fatalf("admin/global source confused with merchant: %+v total=%d", rows, total)
	}
	rows, total = read(url.Values{"column": {"ip"}, "value": {"192.0.2.9"}})
	if total != 1 || len(rows) != 1 || rows[0]["uid"] != nil || rows[0]["type"] != "登录失败" {
		t.Fatalf("failed claimed username inferred merchant: %+v", rows)
	}
	rows, total = read(url.Values{"column": {"ip"}, "value": {"192.0.2.1"}})
	if total != 1 || rows[0]["id"] != "9007199254740993" || rows[0]["source"] != "login" {
		t.Fatalf("physical large log ID lost: %+v", rows)
	}
	rows, total = read(url.Values{"uid": {"1"}, "page": {"2"}, "limit": {"1"}})
	if total != 4 || len(rows) != 1 {
		t.Fatalf("original uid link/paging: %+v %d", rows, total)
	}
	for _, q := range []url.Values{{"column": {"other"}}, {"column": {"uid"}, "value": {"01"}}, {"uid": {"1"}, "column": {"uid"}, "value": {"1"}}, {"column": {"uid", "ip"}}, {"column": {"uid"}, "value": {"9223372036854775808"}}, {"page": {"01"}}, {"path": {"../../logs"}}} {
		if w = call(token, q); w.Code != 400 {
			t.Fatalf("invalid operation log query %s accepted %d", q.Encode(), w.Code)
		}
	}
	operator, _, e := s.Sessions.Create(identity.User{ID: 2, Username: "system-log-operator", Role: "admin", AdminClass: "operator"})
	if e != nil {
		t.Fatal(e)
	}
	if w = call(operator, url.Values{}); w.Code != 200 {
		t.Fatalf("original ordinary administrator log read %d", w.Code)
	}
	merchant, _, err := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/console/v1/merchant/operations/system-log", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: merchant})
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("merchant global operation log privacy %d", w.Code)
	}
	if after := fingerprint(); after != before {
		t.Fatalf("read/filter changed existing log facts: %s != %s", before, after)
	}
}

func TestSystemLogUXDeletedTenantUsesLastImmutableAuditedOwner(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	token, _, err := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if err != nil {
		t.Fatal(err)
	}
	// The current record is gone. Its last recorded owner, rather than its
	// original owner or an unrelated current record, owns the deletion event.
	if _, err = pool.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES
 ('admin',1,'admin','domains',991,'create','{"merchant_id":"1"}'),
 ('admin',1,'admin','domains',991,'update','{"merchant_id":"2"}'),
 ('admin',1,'admin','domains',991,'delete','{}'),
 ('merchant',1,'merchant','domains',992,'create','{"merchant_id":"2"}'),
 ('admin',1,'admin','domains',992,'archive','{}'),
 ('admin',1,'admin','domains',993,'create','{"merchant_id":"1"}'),
 ('admin',1,'admin','domains',993,'update','{}'),
 ('admin',1,'admin','domains',993,'delete','{}'),
 ('admin',1,'admin','domains',994,'delete','{}')`); err != nil {
		t.Fatal(err)
	}
	read := func(uid string) []map[string]any {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/admin/operations/system-log?uid="+uid, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out struct {
			Data struct{ Items []map[string]any }
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("historical deletion log %d %s", w.Code, w.Body.String())
		}
		return out.Data.Items
	}
	rows := read("2")
	if len(rows) != 2 {
		t.Fatalf("last owner must retain update and deletion after record removal: %+v", rows)
	}
	foundDelete := false
	for _, row := range rows {
		foundDelete = foundDelete || row["type"] == "删除 · 授权域名"
	}
	if !foundDelete {
		t.Fatalf("last owner deletion missing: %+v", rows)
	}
	rows = read("1")
	if len(rows) != 4 {
		t.Fatalf("merchant actor provenance or historical create lost: %+v", rows)
	}
	for _, row := range rows {
		if row["type"] == "删除 · 授权域名" {
			t.Fatalf("stale owner incorrectly inherited deletion: %+v", rows)
		}
	}
	rows = read("")
	unknownDeletes := 0
	for _, row := range rows {
		if row["uid"] == nil && row["type"] == "删除 · 授权域名" {
			unknownDeletes++
		}
	}
	if unknownDeletes != 2 {
		t.Fatalf("missing or invalid last snapshot must remain unknown: %+v", rows)
	}
}
