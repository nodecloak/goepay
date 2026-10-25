package contract_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBuyerStatisticsUXOriginalPaidDimensionsFiltersAndReadonly(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	var latest string
	for _, tc := range []struct {
		mid                  int64
		key, kind, buyer, ip string
		money                int64
		paid                 bool
	}{
		{1, "buyer-first", "alipay", "same-account", "192.0.2.10", 123, true},
		{2, "buyer-second", "wxpay", "same-account", "192.0.2.11", 200, true},
		{1, "buyer-unpaid", "alipay", "same-account", "192.0.2.10", 999, false},
		{1, "buyer-empty", "alipay", "", "", 500, true},
	} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: tc.mid, OutTradeNo: tc.key, Name: "用户统计", Type: tc.kind, Amount: tc.money})
		if e != nil {
			t.Fatal(e)
		}
		if tc.paid {
			if e = s.Store.MarkPaid(ctx, o.TradeNo, "buyer-stat:"+o.TradeNo); e != nil {
				t.Fatal(e)
			}
		}
		if _, e = pool.Exec(ctx, `INSERT INTO order_reporting(order_id,buyer,client_ip) VALUES($1,$2,$3) ON CONFLICT(order_id) DO UPDATE SET buyer=$2,client_ip=$3`, o.ID, tc.buyer, tc.ip); e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `UPDATE orders SET created_at='2026-10-24T16:01:00Z' WHERE id=$1`, o.ID); e != nil {
			t.Fatal(e)
		}
		if tc.paid && tc.buyer != "" && o.TradeNo > latest {
			latest = o.TradeNo
		}
	}
	if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES
 ('blacklist','{"kind":"buyer","value":"same-account","enabled":true,"expires_at":"2026-01-01T00:00:00Z"}'),
 ('blacklist','{"kind":"buyer","value":"same-account","enabled":false}'),
 ('blacklist','{"kind":"ip","value":"192.0.2.0/24","enabled":true}')`); e != nil {
		t.Fatal(e)
	}
	admin, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	merchant, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	var auditsBefore int64
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit").Scan(&auditsBefore); e != nil {
		t.Fatal(e)
	}
	get := func(role, token string, q url.Values) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/reports/buyers?"+q.Encode(), nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	query := func(method string) url.Values {
		return url.Values{"view": {"original"}, "from": {"2026-10-25"}, "to": {"2026-10-25"}, "method": {method}}
	}
	read := func(q url.Values) []map[string]any {
		t.Helper()
		w := get("admin", admin, q)
		var out struct {
			Data struct {
				Items []map[string]any
				Total int
			}
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("original buyer view %d %s", w.Code, w.Body.String())
		}
		return out.Data.Items
	}
	rows := read(query("0"))
	if len(rows) != 1 || rows[0]["user"] != "same-account" || rows[0]["order_count"] != "2" || rows[0]["pay_money"] != "3.23" || rows[0]["trade_no"] != latest || rows[0]["is_black"] != false {
		t.Fatalf("paid account source/filter/expiry/count/money: %+v", rows)
	}
	// A legacy malformed expiry is active in payment admission. PostgreSQL's
	// permissive date parser must not turn it into an expired rule in the UI.
	var malformedRule int64
	if e = pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('blacklist','{"kind":"buyer","value":"same-account","enabled":true,"expires_at":"2000-01-01"}') RETURNING id`).Scan(&malformedRule); e != nil {
		t.Fatal(e)
	}
	rows = read(query("0"))
	if len(rows) != 1 || rows[0]["is_black"] != true {
		t.Fatalf("malformed legacy expiry weakened blacklist display: %+v", rows)
	}
	if _, e = pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','false') WHERE id=$1`, malformedRule); e != nil {
		t.Fatal(e)
	}
	rows = read(query("1"))
	if len(rows) != 2 || rows[0]["is_black"] != true || rows[1]["is_black"] != true {
		t.Fatalf("original IP/network blacklist: %+v", rows)
	}
	q := query("0")
	q.Set("type", "alipay")
	rows = read(q)
	if len(rows) != 1 || rows[0]["order_count"] != "1" || rows[0]["pay_money"] != "1.23" {
		t.Fatalf("pay type filter: %+v", rows)
	}
	q = query("0")
	q.Set("user", "not-this-account")
	if rows = read(q); len(rows) != 0 {
		t.Fatalf("explicit money refresh must scope exact user: %+v", rows)
	}
	q = query("0")
	q.Set("from", "2026-10-24")
	q.Set("to", "2026-10-24")
	if rows = read(q); len(rows) != 0 {
		t.Fatalf("Beijing business date: %+v", rows)
	}
	w := get("admin", admin, query("2"))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"available":false`) {
		t.Fatalf("unrecorded phones must be explicit unavailable: %d %s", w.Code, w.Body.String())
	}
	for _, q := range []url.Values{{"view": {"original"}, "method": {"3"}}, {"view": {"original"}, "method": {"0", "1"}}, {"view": {"original"}, "type": {"xx' OR TRUE"}}, {"view": {"original"}, "sort": {"o.id"}}, {"view": {"original"}, "merchant_id": {"1"}}} {
		if w = get("admin", admin, q); w.Code != 400 {
			t.Fatalf("invalid original buyer query accepted %s: %d", q.Encode(), w.Code)
		}
	}
	if w = get("merchant", merchant, query("0")); w.Code != 403 {
		t.Fatalf("merchant buyer privacy %d", w.Code)
	}
	var auditsAfter int64
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit").Scan(&auditsAfter); e != nil || auditsAfter != auditsBefore {
		t.Fatalf("read added audit %v %d -> %d", e, auditsBefore, auditsAfter)
	}
	q = query("0")
	q.Set("format", "csv")
	w = get("admin", admin, q)
	csvRows, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
	if w.Code != 200 || e != nil || len(csvRows) != 2 || csvRows[0][0] != "user" || csvRows[1][0] != "same-account" || csvRows[1][1] != "2" || csvRows[1][2] != "3.23" {
		t.Fatalf("same-scope original buyer CSV %d %v %+v", w.Code, e, csvRows)
	}
}
