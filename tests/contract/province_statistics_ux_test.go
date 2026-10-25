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

func TestProvinceStatisticsUXOriginalTotalsFiltersUnknownAndTenant(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	for _, in := range []struct {
		mid, amount, channel int64
		no, province, at     string
		paid                 bool
	}{
		{1, 123, 7, "province-a", "广东省", "2026-10-24T16:00:00Z", true},
		{2, 200, 8, "province-other", "北京市", "2026-10-24T16:00:01Z", true},
		{1, 300, 7, "province-unknown", "", "2026-10-24T16:00:02Z", true},
		{1, 900, 7, "province-unpaid", "广东省", "2026-10-24T16:00:03Z", false},
		{1, 99, 7, "province-before", "广西壮族自治区", "2026-10-24T15:59:59Z", true},
	} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: in.mid, OutTradeNo: in.no, Name: "local province statistics", Type: "wxpay", Amount: in.amount})
		if e != nil {
			t.Fatal(e)
		}
		if in.paid {
			if e = s.Store.MarkPaid(ctx, o.TradeNo, "province:"+o.TradeNo); e != nil {
				t.Fatal(e)
			}
		}
		if _, e = pool.Exec(ctx, `UPDATE orders SET created_at=$2::timestamptz WHERE id=$1;`, o.ID, in.at); e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `INSERT INTO order_reporting(order_id,province) VALUES($1,$2) ON CONFLICT(order_id) DO UPDATE SET province=$2`, o.ID, in.province); e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `INSERT INTO provider_attempts(order_id,channel_id,method,state) VALUES($1,$2,'native','pending')`, o.ID, in.channel); e != nil {
			t.Fatal(e)
		}
	}
	admin, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	merchant, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	q := url.Values{"view": {"original"}, "from": {"2026-10-25"}, "to": {"2026-10-25"}}
	get := func(role, token string, q url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/reports/provinces?"+q.Encode(), nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	var out struct {
		Data struct {
			Items        []map[string]any
			TotalMoney   string `json:"total_money"`
			TotalCount   string `json:"total_count"`
			UnknownMoney string `json:"unknown_money"`
			UnknownCount string `json:"unknown_count"`
		}
	}
	read := func(role, token string, q url.Values) {
		t.Helper()
		w := get(role, token, q)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("original map statistics missing: %d %s", w.Code, w.Body.String())
		}
	}
	var before int64
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	read("admin", admin, q)
	if out.Data.TotalMoney != "6.23" || out.Data.TotalCount != "3" || out.Data.UnknownMoney != "3.00" || out.Data.UnknownCount != "1" || len(out.Data.Items) != 3 {
		t.Fatalf("paid-only original total or unknown source lost: %+v", out.Data)
	}
	read("merchant", merchant, q)
	if out.Data.TotalMoney != "4.23" || out.Data.TotalCount != "2" || len(out.Data.Items) != 2 {
		t.Fatalf("merchant province leak or missing unknown: %+v", out.Data)
	}
	q.Set("uid", "2")
	read("admin", admin, q)
	if out.Data.TotalMoney != "2.00" || len(out.Data.Items) != 1 || out.Data.Items[0]["name"] != "北京市" {
		t.Fatalf("admin merchant filter: %+v", out.Data)
	}
	if w := get("merchant", merchant, q); w.Code != 400 {
		t.Fatalf("merchant accepted original uid filter: %d", w.Code)
	}
	q.Del("uid")
	q.Set("channel", "7")
	read("admin", admin, q)
	if out.Data.TotalMoney != "4.23" {
		t.Fatalf("recorded channel filter ignored: %+v", out.Data)
	}
	for _, bad := range []url.Values{{"view": {"original"}, "uid": {"1", "2"}}, {"view": {"original"}, "channel": {"01"}}, {"view": {"original"}, "from": {"2026-02-30"}}, {"view": {"original"}, "uid": {"9223372036854775808"}}, {"view": {"original"}, "path": {"../logs"}}} {
		if w := get("admin", admin, bad); w.Code != 400 {
			t.Fatalf("invalid province filters accepted: %s %d", bad.Encode(), w.Code)
		}
	}
	for _, key := range []string{"uid", "channel"} {
		if w := get("merchant", merchant, url.Values{"view": {"original"}, key: {""}}); w.Code != 400 {
			t.Fatalf("merchant accepted empty administrator %s: %d", key, w.Code)
		}
	}
	var after int64
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit`).Scan(&after); e != nil || after != before {
		t.Fatalf("province reads wrote business facts: %d -> %d %v", before, after, e)
	}
}

func TestProvinceStatisticsUXExactLargeUnknownMoneyAndSameScopeCSV(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "province-exact-large", Name: "local exact province", Type: "wxpay", Amount: 9007199254740993})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "province-exact:"+o.TradeNo); e != nil {
		t.Fatal(e)
	}
	token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	get := func(query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/console/v1/merchant/reports/provinces?"+query, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := get("view=original")
	var out struct {
		Data struct {
			TotalMoney   string `json:"total_money"`
			UnknownMoney string `json:"unknown_money"`
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.TotalMoney != "90071992547409.93" || out.Data.UnknownMoney != out.Data.TotalMoney {
		t.Fatalf("unknown large amount lost: %d %s", w.Code, w.Body.String())
	}
	w = get("view=original&format=csv")
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
	if w.Code != 200 || err != nil || len(rows) != 2 || rows[1][0] != "未知" || rows[1][1] != "90071992547409.93" {
		t.Fatalf("same-scope original province CSV %d %v %+v", w.Code, err, rows)
	}
	var n int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='reports/provinces-original' AND operation='export-csv'`).Scan(&n); e != nil || n != 1 {
		t.Fatalf("successful CSV requires one download audit: %d %v", n, e)
	}
	if w = get(""); w.Code != 200 {
		t.Fatalf("legacy merchant province API removed %d %s", w.Code, w.Body.String())
	}
}
