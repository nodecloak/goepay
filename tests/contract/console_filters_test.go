package contract_test

import (
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConsoleListFiltersAndTenantBoundary(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	var wanted payment.Order
	for i, spec := range []struct {
		mid           int64
		no, typ, date string
		paid          bool
	}{
		{1, "wanted", "sandbox", "2026-10-22T16:00:00Z", true},
		{1, "yesterday", "alipay", "2026-10-22T15:59:59Z", true},
		{2, "other-tenant", "sandbox", "2026-10-22T16:00:00Z", true},
		{1, "unpaid", "sandbox", "2026-10-23T00:00:00Z", false},
	} {
		// Import a genuinely historical birth at its original timestamp. Current
		// order births authenticate created_at and must never be backdated.
		var trade string
		if e := pool.QueryRow(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount,platform_fee,fee_policy,refund_fee_payer,created_at)VALUES(nextval('trade_no_seq')::text,$1,$2,$2,$3,123,123,0,'none','platform',$4::timestamptz)RETURNING trade_no`, spec.mid, spec.no, spec.typ, spec.date).Scan(&trade); e != nil {
			t.Fatal(e)
		}
		o, e := s.Store.OrderByTradeNo(ctx, trade)
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			if _, _, e = s.Store.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); e != nil {
				t.Fatal(e)
			}
			wanted = o
		}
		if spec.paid {
			if e = s.Store.MarkPaid(ctx, o.TradeNo, "list-"+o.TradeNo); e != nil {
				t.Fatal(e)
			}
		}
	}
	tokens := map[string]string{}
	for _, role := range []string{"admin", "merchant"} {
		var e error
		tokens[role], _, e = s.Sessions.Create(identity.User{Username: role, ID: 1, Role: role})
		if e != nil {
			t.Fatal(e)
		}
	}
	get := func(role, endpoint string) (int, map[string]any) {
		r := httptest.NewRequest("GET", "/console/v1/"+role+endpoint, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: tokens[role]})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var v map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
			t.Fatal(e)
		}
		return w.Code, v
	}
	status, v := get("admin", "/orders?merchant_id=1&type=sandbox&status=paid&channel_id=7&from=2026-10-23&to=2026-10-23")
	if status != 200 {
		t.Fatalf("filtered status=%d body=%v", status, v)
	}
	data := v["data"].(map[string]any)
	items := data["items"].([]any)
	if data["total"] != float64(1) || len(items) != 1 || items[0].(map[string]any)["trade_no"] != wanted.TradeNo {
		t.Fatalf("filters ignored: %v", v)
	}
	if status, _ = get("merchant", "/orders?merchant_id=2"); status != 403 {
		t.Fatalf("merchant selected another owner: %d", status)
	}
	for _, q := range []string{"status=anything", "status=paid%2Cclosed", "type=invalid", "channel_id=-1", "merchant_id=0", "from=2026-02-30", "from=2026-10-24&to=2026-10-23", "status=paid&status=created"} {
		if status, _ = get("admin", "/orders?"+q); status != 400 {
			t.Errorf("invalid filter %s accepted: %d", q, status)
		}
	}
	if status, v = get("merchant", "/orders?status=paid"); status != 200 || v["data"].(map[string]any)["total"] != float64(2) {
		t.Fatalf("tenant filter result=%d %v", status, v)
	}
	// Payment metadata filters must use trusted columns and bound values.
	if _, e := pool.Exec(ctx, "INSERT INTO order_reporting(order_id,buyer,client_ip) VALUES($1,'trusted-buyer','2001:db8::1')", wanted.ID); e != nil {
		t.Fatal(e)
	}
	if status, v = get("admin", "/orders?buyer=trusted-buyer&client_ip=2001%3Adb8%3A%3A1"); status != 200 || v["data"].(map[string]any)["total"] != float64(1) {
		t.Fatalf("metadata filtering: %d %v", status, v)
	}
	if status, v = get("admin", "/orders?buyer=%27%20OR%201%3D1--"); status != 200 || v["data"].(map[string]any)["total"] != float64(0) {
		t.Fatalf("filter not parameterized: %d %v", status, v)
	}
}
