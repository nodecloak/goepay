package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMerchantPaymentStatisticsUXExactSevenDaysAndTenantIsolation(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Now().In(zone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	for i, in := range []struct {
		mid, amount int64
		kind        string
		paid        bool
		at          time.Time
	}{
		{1, 9007199254740993, "alipay", true, today},
		{1, 1, "wxpay", true, today.Add(time.Hour)},
		{1, 99, "wxpay", false, today.Add(time.Hour)},
		{1, 100, "alipay", true, today.Add(-time.Nanosecond)},
		{1, 999, "alipay", false, today.Add(-time.Hour)},
		{1, 7, "sandbox", true, today.AddDate(0, 0, -6)},
		{1, 999999, "alipay", true, today.AddDate(0, 0, -6).Add(-time.Nanosecond)},
		{1, 999999, "alipay", true, today.AddDate(0, 0, 1)},
		{2, 999999, "alipay", true, today.Add(time.Hour)},
	} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: in.mid, OutTradeNo: fmt.Sprintf("merchant-stat147-%d", i), Name: "本地统计", Type: in.kind, Amount: in.amount})
		if e != nil {
			t.Fatal(e)
		}
		if in.paid {
			if e = s.Store.MarkPaid(ctx, o.TradeNo, "merchant-stat147:"+o.TradeNo); e != nil {
				t.Fatal(e)
			}
		}
		if _, e = pool.Exec(ctx, `UPDATE orders SET created_at=$2 WHERE id=$1`, o.ID, in.at); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := pool.Exec(ctx, `UPDATE merchants SET enabled=false WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	fingerprint := func() string {
		t.Helper()
		var v string
		if e := pool.QueryRow(ctx, `SELECT md5((SELECT jsonb_agg(to_jsonb(a) ORDER BY id)::text FROM accounts a)||(SELECT jsonb_agg(to_jsonb(o) ORDER BY id)::text FROM orders o)||(SELECT jsonb_agg(to_jsonb(m) ORDER BY id)::text FROM merchants m))`).Scan(&v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	before := fingerprint()
	var auditsBefore int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit`).Scan(&auditsBefore); e != nil {
		t.Fatal(e)
	}
	get := func(id string) map[string]any {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/admin/reports/merchant-payments?uid="+id, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var v struct{ Data map[string]any }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatalf("merchant statistics missing: %d %s", w.Code, w.Body.String())
		}
		return v.Data
	}
	data := get("1")
	if data["merchant_id"] != "1" || data["timezone"] != "Asia/Shanghai" || data["cohort"] != "merchant_orders_created" {
		t.Fatalf("wrong context %+v", data)
	}
	trend := data["trend"].([]any)
	if len(trend) != 7 {
		t.Fatalf("seven days missing %+v", trend)
	}
	for i, item := range trend {
		d := item.(map[string]any)
		if d["date"] != today.AddDate(0, 0, i-6).Format("2006-01-02") || len(d["items"].([]any)) != 3 {
			t.Fatalf("date/method gap %+v", d)
		}
		if i > 0 && i < 5 && d["summary"].(map[string]any)["money"] != "0.00" {
			t.Fatalf("empty day not zero %+v", d)
		}
	}
	if sum := trend[0].(map[string]any)["summary"].(map[string]any); sum["money"] != "0.07" || sum["count"] != "1" {
		t.Fatalf("lower boundary %+v", sum)
	}
	for _, in := range []struct{ key, money, count, rate string }{{"today", "90071992547409.94", "3", "66.67"}, {"yesterday", "1.00", "2", "50.00"}} {
		d := data["days"].(map[string]any)[in.key].(map[string]any)
		sum := d["summary"].(map[string]any)
		if sum["money"] != in.money || sum["count"] != in.count || sum["rate"] != in.rate {
			t.Fatalf("exact weighted %s %+v", in.key, sum)
		}
	}
	other := get("2")["days"].(map[string]any)["today"].(map[string]any)["summary"].(map[string]any)
	if other["money"] != "9999.99" || other["count"] != "1" {
		t.Fatalf("tenant mixed %+v", other)
	}
	if fingerprint() != before {
		t.Fatal("read-only report mutated source rows")
	}
	var audits int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit`).Scan(&audits); e != nil || audits != auditsBefore {
		t.Fatalf("read wrote audit %d %v", audits, e)
	}
}

func TestMerchantPaymentStatisticsUXStrictScopeAndEmptyHistory(t *testing.T) {
	s, pool, _ := fixture(t)
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, path, cookie, tok string) *httptest.ResponseRecorder {
		parts := strings.SplitN(path, "?", 2)
		r := httptest.NewRequest(method, parts[0], nil)
		if len(parts) == 2 {
			r.URL.RawQuery = parts[1]
		}
		r.AddCookie(&http.Cookie{Name: cookie, Value: tok})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	base := "/console/v1/admin/reports/merchant-payments"
	for _, q := range []string{"", "?uid=", "?uid=0", "?uid=-1", "?uid=01", "?uid=+1", "?uid=1&uid=2", "?uid=9223372036854775808", "?uid=1&gid=1", "?uid=1&format=csv", "?uid=1%0a", "?uid=%zz"} {
		if w := call("GET", base+q, "goepay_admin", token); w.Code != 400 {
			t.Fatalf("invalid query %q: %d %s", q, w.Code, w.Body.String())
		}
	}
	if w := call("GET", base+"?uid=9223372036854775807", "goepay_admin", token); w.Code != 404 {
		t.Fatalf("missing merchant %d", w.Code)
	}
	if w := call("POST", base+"?uid=1", "goepay_admin", token); w.Code != 405 {
		t.Fatalf("write accepted %d", w.Code)
	}
	if w := call("GET", base+"?uid=1", "goepay_admin", ""); w.Code != 401 {
		t.Fatalf("anonymous %d", w.Code)
	}
	merchant, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	if w := call("GET", "/console/v1/merchant/reports/merchant-payments?uid=2", "goepay_merchant", merchant); w.Code != 403 {
		t.Fatalf("merchant cross-read %d", w.Code)
	}
	if _, e = pool.Exec(context.Background(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) SELECT 'merchant147operator','admin',2,'Operator',password_hash,'operator' FROM console_users WHERE username='admin' AND role='admin'`); e != nil {
		t.Fatal(e)
	}
	token, _, e = s.Sessions.Create(identity.User{ID: 2, Username: "merchant147operator", Role: "admin", AdminClass: "operator"})
	if e != nil {
		t.Fatal(e)
	}
	w := call("GET", base+"?uid=1", "goepay_admin", token)
	if w.Code != 200 {
		t.Fatalf("operator empty history %d %s", w.Code, w.Body.String())
	}
	var data struct {
		Data struct {
			Trend []struct {
				Summary struct{ Money, Count, Rate string }
			}
		}
	}
	if e = json.Unmarshal(w.Body.Bytes(), &data); e != nil || len(data.Data.Trend) != 7 {
		t.Fatalf("empty seven days %v", e)
	}
	for _, d := range data.Data.Trend {
		if d.Summary.Money != "0.00" || d.Summary.Count != "0" || d.Summary.Rate != "0.00" {
			t.Fatalf("empty money %+v", d)
		}
	}
}
