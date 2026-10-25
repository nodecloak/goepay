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

func TestProOrderSearchHTTPFieldsNotificationCSVAndTenant(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	var wanted payment.Order
	for _, mid := range []int64{1, 2} {
		o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: mid, OutTradeNo: "exact-search", Name: "A%_literal 商品", Type: "sandbox", Amount: 123, Param: "Case%_Sensitive"})
		if e != nil {
			t.Fatal(e)
		}
		if _, _, e = s.Store.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, "UPDATE provider_attempts SET provider_trade_no=$2 WHERE order_id=$1", o.ID, "LOCAL_"+o.TradeNo); e != nil {
			t.Fatal(e)
		}
		if e = s.Store.SetOrderClientIP(ctx, o.ID, "2001:db8::1"); e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, "UPDATE order_reporting SET buyer='original-buyer' WHERE order_id=$1", o.ID); e != nil {
			t.Fatal(e)
		}
		if e = s.Store.MarkPaid(ctx, o.TradeNo, "pro-search:"+o.TradeNo); e != nil {
			t.Fatal(e)
		}
		if mid == 1 {
			wanted = o
		}
	}
	// The old successful job must not defeat a newer failed notification.
	if _, e := pool.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload,state,created_at) VALUES('pro-search-old','payment.notify',jsonb_build_object('trade_no',$1::text),'succeeded',statement_timestamp()+interval '1 second'),('pro-search-new','payment.notify',jsonb_build_object('trade_no',$1::text),'dead',statement_timestamp()+interval '2 seconds')`, wanted.TradeNo); e != nil {
		t.Fatal(e)
	}
	tokens := map[string]string{}
	for _, role := range []string{"admin", "merchant"} {
		var e error
		tokens[role], _, e = s.Sessions.Create(identity.User{Username: role, ID: 1, Role: role})
		if e != nil {
			t.Fatal(e)
		}
	}
	request := func(role, resource, raw string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/"+resource+"?"+raw, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: tokens[role]})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	check := func(role string, q url.Values, total int) {
		t.Helper()
		w := request(role, "orders", q.Encode())
		var body struct {
			Data struct {
				Total int
				Items []map[string]any
			}
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Data.Total != total || len(body.Data.Items) != total {
			t.Fatalf("%s query=%s status=%d body=%s", role, q.Encode(), w.Code, w.Body.String())
		}
		for _, item := range body.Data.Items {
			if role == "merchant" && item["merchant_id"] != "1" {
				t.Fatal("field search bypassed authenticated owner")
			}
		}
	}
	for _, tc := range []struct{ field, value string }{
		{"trade_no", wanted.TradeNo}, {"out_trade_no", "exact-search"}, {"api_trade_no", "LOCAL_" + wanted.TradeNo},
		{"name", "%_literal"}, {"param", "case%_sensitive"}, {"money", "1.23"}, {"realmoney", "1.23"}, {"getmoney", "1.23"},
		{"buyer", "original-buyer"}, {"ip", "2001:0db8:0:0:0:0:0:1"},
	} {
		check("merchant", url.Values{"search_field": {tc.field}, "q": {tc.value}}, 1)
	}
	check("admin", url.Values{"search_field": {"out_trade_no"}, "q": {"exact-search"}}, 2)
	for _, tc := range []struct{ field, value string }{{"out_trade_no", "exact"}, {"trade_no", "' OR 1=1--"}, {"name", "%_not-a-wildcard"}, {"money", "92233720368547758.07"}} {
		check("merchant", url.Values{"search_field": {tc.field}, "q": {tc.value}}, 0)
	}
	check("merchant", url.Values{"search_field": {"out_trade_no"}}, 1)
	check("merchant", url.Values{"notification_state": {"dead"}}, 1)
	check("merchant", url.Values{"notification_state": {"succeeded"}}, 0)
	for _, raw := range []string{"search_field=money&q=1e2", "search_field=money&q=1.001", "search_field=money&q=92233720368547758.08", "search_field=ip&q=bad", "search_field=mobile&q=123", "search_field=name&search_field=param&q=x", "q=x&q=y", "notification_state=paid", "notification_state=dead&notification_state=pending", "q=%GG", "q=abc;status=paid"} {
		if w := request("merchant", "orders", raw); w.Code != 400 {
			t.Errorf("invalid query=%s accepted: %d", raw, w.Code)
		}
	}
	if w := request("merchant", "orders", "search_field=money&q=1.23&merchant_id=2"); w.Code != 403 {
		t.Fatalf("owner override accepted: %d", w.Code)
	}
	if w := request("merchant", "refunds", "search_field=name&q=x"); w.Code != 400 {
		t.Fatalf("order field accepted in refunds: %d", w.Code)
	}
	q := url.Values{"search_field": {"api_trade_no"}, "q": {"LOCAL_" + wanted.TradeNo}, "notification_state": {"dead"}, "format": {"csv"}}
	w := request("merchant", "orders", q.Encode())
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("CSV filter status=%d body=%s", w.Code, w.Body.String())
	}
	rows, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
	if e != nil || len(rows) != 2 || !strings.Contains(strings.Join(rows[1], "|"), wanted.TradeNo) {
		t.Fatalf("CSV not same scoped selection: rows=%v error=%v", rows, e)
	}
}
