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

func TestReportsScopeMoneyAndValidation(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	for _, in := range []payment.CreateOrderInput{
		{MerchantID: 1, OutTradeNo: "report-a", Name: "A", Type: "sandbox", Amount: 12345},
		{MerchantID: 2, OutTradeNo: "report-b", Name: "B", Type: "sandbox", Amount: 99999},
	} {
		o, err := s.Store.CreateOrder(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Store.MarkPaid(ctx, o.TradeNo, "test:"+o.TradeNo); err != nil {
			t.Fatal(err)
		}
	}
	token, _, err := s.Sessions.Create(identity.User{Username: "merchant", ID: 1, Role: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) (int, map[string]any) {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return w.Code, out
	}
	status, out := get("/console/v1/merchant/reports/daily")
	if status != 200 {
		t.Fatalf("status=%d body=%v", status, out)
	}
	data := out["data"].(map[string]any)
	items := data["items"].([]any)
	if data["total"] != float64(1) || len(items) != 1 {
		t.Fatal(out)
	}
	row := items[0].(map[string]any)
	if row["paid_money"] != "123.45" || row["orders"] != "1" {
		t.Fatalf("scope or decimal error: %v", row)
	}
	if row["net_money"] != "123.45" || row["fee_money"] != "0.00" {
		t.Fatalf("fee reporting not exact: %v", row)
	}
	for _, q := range []string{"from=2026-02-30", "from=2026-10-02&to=2026-10-01", "limit=999999", "page=-1"} {
		status, _ = get("/console/v1/merchant/reports/daily?" + q)
		if status != 400 {
			t.Fatalf("accepted %s: %d", q, status)
		}
	}
	status, _ = get("/console/v1/merchant/reports/merchants")
	if status != 403 {
		t.Fatalf("merchant admin report: %d", status)
	}
	status, out = get("/console/v1/merchant/reports/provinces")
	if status != 200 {
		t.Fatal(out)
	}
	row = out["data"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if row["dimension"] != "未知" {
		t.Fatal(row)
	}
}
