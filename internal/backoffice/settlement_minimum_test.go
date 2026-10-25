package backoffice

import (
	"context"
	"fmt"
	"goepay/internal/payment"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettlementMinimumMerchantAndAdministratorCannotBypass(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	o, e := s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 101, OutTradeNo: "minimum-funding", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.store.MarkPaid(ctx, o.TradeNo, "minimum-funding"); e != nil {
		t.Fatal(e)
	}
	setting, e := s.Create(ctx, admin, "settings", input(`{"key":"settlement_minimum_cents","value":"101"}`))
	if e != nil {
		t.Fatal(e)
	}
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "merchant"}
	for i, p := range []Principal{merchant, admin} {
		for _, amount := range []string{"1.00", "0.009"} {
			w := httptest.NewRecorder()
			body := fmt.Sprintf(`{"merchant_id":"101","external_no":"below-%d-%s","amount":"%s"}`, i, amount, amount)
			s.ServeWorkflows(w, httptest.NewRequest("POST", "/workflows/settlements", strings.NewReader(body)), p, []string{"settlements"})
			if w.Code != 422 {
				t.Fatalf("%s amount%s: %d %s", p.Role, amount, w.Code, w.Body.String())
			}
		}
	}
	b, _ := s.store.Balance(ctx, 101)
	if b.Available != 1000 || b.Reserved != 0 {
		t.Fatal("invalid submission reserved money", b)
	}
	for i, p := range []Principal{merchant, admin} {
		w := httptest.NewRecorder()
		body := fmt.Sprintf(`{"merchant_id":"101","external_no":"exact-%d","amount":"1.01"}`, i)
		s.ServeWorkflows(w, httptest.NewRequest("POST", "/workflows/settlements", strings.NewReader(body)), p, []string{"settlements"})
		if w.Code != 201 {
			t.Fatalf("%s exact minimum: %d %s", p.Role, w.Code, w.Body.String())
		}
	}
	if _, e = s.Update(ctx, admin, "settings", setting.ID, input(`{"value":"1.01"}`)); e == nil {
		t.Fatal("decimal yuan accepted as integer cents setting")
	}
	b, _ = s.store.Balance(ctx, 101)
	if b.Available != 798 || b.Reserved != 202 {
		t.Fatalf("exact amount not reserved once per request: %+v", b)
	}
}
