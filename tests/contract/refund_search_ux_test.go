package contract_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestRefundSearchUXOriginalIdentifiersAcrossListSummaryAndCSV(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, "SELECT setval(pg_get_serial_sequence('refunds','id'),9007199254740992,true)"); e != nil {
		t.Fatal(e)
	}
	var first payment.Refund
	var firstTrade string
	for _, mid := range []int64{1, 2} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: mid, OutTradeNo: "refund-search-original", Name: "退款编号", Type: "alipay", Amount: 1000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, fmt.Sprintf("refund-search-paid-%d", mid)); e != nil {
			t.Fatal(e)
		}
		r, e := s.Store.ReserveRefund(ctx, mid, o.OutTradeNo, " shared-refund-request ", 123)
		if e != nil {
			t.Fatal(e)
		}
		if mid == 1 {
			first = r
			firstTrade = o.TradeNo
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
	get := func(role, token, suffix string, query url.Values) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/refunds"+suffix+"?"+query.Encode(), nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	platform := strconv.FormatInt(first.ID, 10)
	for _, tc := range []struct {
		field, value, role, token string
		total                     int
	}{{"refund_no", platform, "admin", admin, 1}, {"out_refund_no", " shared-refund-request ", "admin", admin, 2}, {"out_refund_no", " shared-refund-request ", "merchant", merchant, 1}, {"trade_no", firstTrade, "admin", admin, 1}} {
		q := url.Values{"search_field": {tc.field}, "q": {tc.value}}
		w := get(tc.role, tc.token, "", q)
		var out struct {
			Data struct {
				Total int
				Items []map[string]any
			}
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.Total != tc.total || len(out.Data.Items) != tc.total {
			t.Fatalf("original refund selector %s: %d %s", tc.field, w.Code, w.Body.String())
		}
		for _, row := range out.Data.Items {
			if row["refund_no"] != " shared-refund-request " || row["out_refund_no"] != " shared-refund-request " || row["platform_refund_no"] != row["id"] {
				t.Fatalf("old console key changed or original identifier aliases missing: %+v", row)
			}
		}
		w = get(tc.role, tc.token, "/summary", q)
		var stats struct{ Data map[string]string }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &stats) != nil || stats.Data["totalCount"] != strconv.Itoa(tc.total) || stats.Data["pendingCount"] != strconv.Itoa(tc.total) {
			t.Fatalf("summary scope differs %d %s", w.Code, w.Body.String())
		}
	}
	q := url.Values{"search_field": {"refund_no"}, "q": {platform}, "format": {"csv"}}
	w := get("admin", admin, "", q)
	rows, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
	if w.Code != 200 || e != nil || len(rows) != 2 {
		t.Fatalf("identifier CSV %d %v", w.Code, e)
	}
	for _, key := range []string{"platform_refund_no", "out_refund_no"} {
		at := -1
		for i, k := range rows[0] {
			if k == key {
				at = i
			}
		}
		if at < 0 {
			t.Fatalf("CSV missing identifier %s", key)
		}
		want := platform
		if key == "out_refund_no" {
			want = " shared-refund-request "
		}
		if rows[1][at] != want {
			t.Fatalf("CSV identifier %s: %q", key, rows[1][at])
		}
	}
	for _, q := range []url.Values{{"search_field": {"refund_no"}, "q": {"01"}}, {"search_field": {"refund_no"}, "q": {"9223372036854775808"}}, {"search_field": {"f.refund_no"}, "q": {"x"}}, {"search_field": {"refund_no", "out_refund_no"}, "q": {platform}}} {
		for _, suffix := range []string{"", "/summary"} {
			if w = get("admin", admin, suffix, q); w.Code != 400 {
				t.Fatalf("invalid refund selector %s %d", q.Encode(), w.Code)
			}
		}
	}
	if w = get("merchant", merchant, "", url.Values{"search_field": {"refund_no"}, "q": {strconv.FormatInt(first.ID+1, 10)}}); w.Code != 200 || !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatalf("foreign platform id crossed tenant %d %s", w.Code, w.Body.String())
	}
}
