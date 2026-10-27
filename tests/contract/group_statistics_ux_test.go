package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGroupStatisticsUXExactAmountsCurrentMembershipAndWeightedDays(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	var gid, other int64
	for _, v := range []struct {
		name string
		id   *int64
	}{{"统计143组", &gid}, {"其它组", &other}} {
		if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups',jsonb_build_object('name',$1::text,'enabled',true)) RETURNING id`, v.name).Scan(v.id); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{group_id}',to_jsonb($1::bigint::text)) WHERE id=1`, other); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(1,$1,now()+interval '1 day')`, gid); e != nil {
		t.Fatal(e)
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Now().In(zone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	for i, in := range []struct {
		mid, amount int64
		kind        string
		paid        bool
		at          time.Time
	}{
		{1, 9007199254740993, "alipay", true, today}, {1, 1, "wxpay", true, today.Add(time.Hour)}, {1, 99, "wxpay", false, today.Add(time.Hour)},
		{1, 100, "alipay", true, today.Add(-time.Nanosecond)}, {1, 999, "alipay", false, today.Add(-time.Hour)},
		{1, 999999, "alipay", true, today.AddDate(0, 0, -1).Add(-time.Nanosecond)},
		{2, 999999, "alipay", true, today.Add(time.Hour)},
	} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: in.mid, OutTradeNo: fmt.Sprintf("group143-%d", i), Name: "本地统计", Type: in.kind, Amount: in.amount})
		if e != nil {
			t.Fatal(e)
		}
		if in.paid {
			if e = s.Store.MarkPaid(ctx, o.TradeNo, "group143:"+o.TradeNo); e != nil {
				t.Fatal(e)
			}
		}
		if _, e = pool.Exec(ctx, `UPDATE orders SET created_at=$2 WHERE id=$1`, o.ID, in.at); e != nil {
			t.Fatal(e)
		}
	}
	token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	get := func(id int64) map[string]any {
		t.Helper()
		r := httptest.NewRequest("GET", fmt.Sprintf("/console/v1/admin/reports/groups?gid=%d", id), nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var v struct{ Data map[string]any }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatalf("group statistics missing: %d %s", w.Code, w.Body.String())
		}
		return v.Data
	}
	before := func() string {
		var v string
		if e := pool.QueryRow(ctx, `SELECT md5((SELECT jsonb_agg(to_jsonb(a) ORDER BY id)::text FROM accounts a)||(SELECT jsonb_agg(to_jsonb(o) ORDER BY id)::text FROM orders o))`).Scan(&v); e != nil {
			t.Fatal(e)
		}
		return v
	}()
	var auditsBefore int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit`).Scan(&auditsBefore); e != nil {
		t.Fatal(e)
	}
	data := get(gid)
	if data["group_id"] != fmt.Sprint(gid) || data["group_name"] != "统计143组" || data["cohort"] != "current_effective_group" {
		t.Fatalf("wrong group context %+v", data)
	}
	days := data["days"].(map[string]any)
	for _, in := range []struct{ key, money, count, rate string }{{"today", "90071992547409.94", "3", "66.67"}, {"yesterday", "1.00", "2", "50.00"}} {
		day := days[in.key].(map[string]any)
		sum := day["summary"].(map[string]any)
		if sum["money"] != in.money || sum["count"] != in.count || sum["rate"] != in.rate {
			t.Fatalf("exact weighted %s %+v", in.key, sum)
		}
		if len(day["items"].([]any)) != 3 {
			t.Fatalf("stable methods %+v", day)
		}
	}
	if sum := get(other)["days"].(map[string]any)["today"].(map[string]any)["summary"].(map[string]any); sum["count"] != "0" || sum["money"] != "0.00" || sum["rate"] != "0.00" {
		t.Fatalf("base group leaked active membership %+v", sum)
	}
	if _, e = pool.Exec(ctx, `UPDATE memberships SET expires_at=now()-interval '1 second' WHERE merchant_id=1`); e != nil {
		t.Fatal(e)
	}
	if sum := get(other)["days"].(map[string]any)["today"].(map[string]any)["summary"].(map[string]any); sum["count"] != "3" {
		t.Fatalf("expired membership did not restore baseline %+v", sum)
	}
	if _, e = pool.Exec(ctx, `UPDATE merchants SET group_baseline_unresolved=true WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	if sum := get(other)["days"].(map[string]any)["today"].(map[string]any)["summary"].(map[string]any); sum["count"] != "0" {
		t.Fatalf("unresolved baseline leaked %+v", sum)
	}
	var after string
	if e = pool.QueryRow(ctx, `SELECT md5((SELECT jsonb_agg(to_jsonb(a) ORDER BY id)::text FROM accounts a)||(SELECT jsonb_agg(to_jsonb(o) ORDER BY id)::text FROM orders o))`).Scan(&after); e != nil || after != before {
		t.Fatalf("report mutated financial rows %v", e)
	}
	var audits int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit`).Scan(&audits); e != nil || audits != auditsBefore {
		t.Fatalf("report wrote audit %d %v", audits, e)
	}
}

func TestGroupStatisticsUXStrictReadOnlyScopeAndDefaultGroup(t *testing.T) {
	s, pool, _ := fixture(t)
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, path, cookie, tok string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.AddCookie(&http.Cookie{Name: cookie, Value: tok})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	base := "/console/v1/admin/reports/groups"
	for _, q := range []string{"", "?gid=", "?gid=-1", "?gid=01", "?gid=1&gid=2", "?gid=9223372036854775808", "?gid=1&uid=2", "?gid=1&format=csv", "?gid=1%0a"} {
		if w := call("GET", base+q, "goepay_admin", token); w.Code != 400 {
			t.Fatalf("invalid query %q returned %d %s", q, w.Code, w.Body.String())
		}
	}
	if w := call("GET", base+"?gid=9223372036854775807", "goepay_admin", token); w.Code != 404 {
		t.Fatalf("missing group %d", w.Code)
	}
	if w := call("GET", base+"?gid=0", "goepay_admin", token); w.Code != 200 {
		t.Fatalf("default group missing %d %s", w.Code, w.Body.String())
	}
	if w := call("POST", base+"?gid=0", "goepay_admin", token); w.Code != 405 {
		t.Fatalf("write accepted %d", w.Code)
	}
	merchant, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	if w := call("GET", "/console/v1/merchant/reports/groups?gid=0", "goepay_merchant", merchant); w.Code != 403 {
		t.Fatalf("merchant got group statistics %d", w.Code)
	}
	if _, e = pool.Exec(context.Background(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) SELECT 'group143operator','admin',2,'Operator',password_hash,'operator' FROM console_users WHERE username='admin' AND role='admin'`); e != nil {
		t.Fatal(e)
	}
	token, _, e = s.Sessions.Create(identity.User{ID: 2, Username: "group143operator", Role: "admin", AdminClass: "operator"})
	if e != nil {
		t.Fatal(e)
	}
	if w := call("GET", base+"?gid=0", "goepay_admin", token); w.Code != 200 {
		t.Fatalf("original admin reader refused %d %s", w.Code, w.Body.String())
	}
}
