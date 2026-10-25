package contract_test

import (
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestIncomeHoldProjectsLegacyStatusThreeWithoutChangingPaidEvidence(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "income-status", Name: "item", Amount: 123, Type: "alipay"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "hold-projection-paid"); e != nil {
		t.Fatal(e)
	}
	h, e := s.Store.FreezeOrderIncome(ctx, 1, o.TradeNo, "hold-status", payment.IncomeHoldActor{ID: 1, Name: "admin", Class: "super"})
	if e != nil {
		t.Fatal(e)
	}
	_, v := call(t, s.Handler(), "GET", "/api.php", url.Values{"act": {"order"}, "pid": {"1"}, "key": {"contract-secret"}, "trade_no": {o.TradeNo}})
	if v["code"] != float64(1) || v["status"] != float64(3) {
		t.Fatalf("held income not projected as status3: %v", v["status"])
	}
	for _, status := range []string{"1", "3"} {
		_, v = call(t, s.Handler(), "GET", "/api.php", url.Values{"act": {"orders"}, "pid": {"1"}, "key": {"contract-secret"}, "status": {status}})
		want := 0
		if status == "3" {
			want = 1
		}
		if v["code"] != float64(1) || len(v["data"].([]any)) != want {
			t.Fatalf("hold list filter %s %v", status, v["code"])
		}
	}
	token, _, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	checkConsole := func(state string, total int) {
		r := httptest.NewRequest("GET", "/console/v1/merchant/orders?income_status="+state, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out struct {
			Data struct {
				Total int              `json:"total"`
				Items []map[string]any `json:"items"`
			} `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		if w.Code != 200 || out.Data.Total != total {
			t.Fatalf("console income status %s ignored: %d count%d", state, w.Code, out.Data.Total)
		}
		if total == 1 && out.Data.Items[0]["income_status"] != state {
			t.Fatal("hold list projection missing")
		}
	}
	checkConsole("held", 1)
	checkConsole("released", 0)
	checkConsole("none", 0)
	var state string
	if e = pool.QueryRow(ctx, "SELECT status FROM orders WHERE id=$1", o.ID).Scan(&state); e != nil || state != "paid" {
		t.Fatal("hold changed payment truth", state, e)
	}
	if _, e = s.Store.UnfreezeOrderIncome(ctx, 1, o.TradeNo, h.ID, payment.IncomeHoldActor{ID: 1, Name: "admin", Class: "super"}); e != nil {
		t.Fatal(e)
	}
	_, v = call(t, s.Handler(), "GET", "/api.php", url.Values{"act": {"order"}, "pid": {"1"}, "key": {"contract-secret"}, "trade_no": {o.TradeNo}})
	if v["status"] != float64(1) {
		t.Fatal("released income status did not restore", v["status"])
	}
	checkConsole("held", 0)
	checkConsole("released", 1)
}
