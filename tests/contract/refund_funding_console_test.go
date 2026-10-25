package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestRefundFundingPreviewReadsOwnOriginalHoldWithoutProviderIO(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	o, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "funding-preview", Name: "item", Type: "alipay", Amount: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.MarkPaid(ctx, o.TradeNo, "local-preview-paid"); err != nil {
		t.Fatal(err)
	}
	h, err := s.Store.FreezeOrderIncome(ctx, 1, o.TradeNo, "local-preview-hold", payment.IncomeHoldActor{ID: 1, Name: "admin", Class: "super"})
	if err != nil {
		t.Fatal(err)
	}
	token, csrf, err := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err = p.QueryRow(ctx, "SELECT count(*) FROM journals").Scan(&before); err != nil {
		t.Fatal(err)
	}
	call := func(body string) (int, string) {
		r := httptest.NewRequest("POST", "/console/v1/merchant/orders/"+o.TradeNo+"/preview-refund", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cacheable funding preview")
		}
		return w.Code, w.Body.String()
	}
	status, body := call(`{"amount":"4.00","external_no":"preview-original"}`)
	if status != 200 {
		t.Fatal(status, body)
	}
	var envelope struct {
		Data payment.RefundFundingSnapshot `json:"data"`
	}
	if err = json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatal(err)
	}
	f := envelope.Data
	if f.HoldID != h.ID || f.HeldAmount != 400 || f.AvailableAmount != 0 || f.FeeAmount != 0 || f.HeldRemaining != 1000 {
		t.Fatal(f)
	}
	for _, forbidden := range []string{"SourceHash", "source_hash", "HistoryHash", "RequestHash", "ResultHash"} {
		if strings.Contains(body, forbidden) {
			t.Fatal("internal funding evidence exposed", forbidden)
		}
	}
	for _, invalid := range []string{`{}`, `{"amount":"1.00"}`, `{"amount":"1.00","external_no":"x","hold_id":"` + fmt.Sprint(h.ID) + `"}`, `{"amount":"1.00","amount":"2.00","external_no":"x"}`} {
		if status, _ = call(invalid); status < 400 {
			t.Fatal("ambiguous preview accepted", invalid)
		}
	}
	var after, refunds int
	if err = p.QueryRow(ctx, "SELECT count(*) FROM journals").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err = p.QueryRow(ctx, "SELECT count(*) FROM refunds").Scan(&refunds); err != nil || refunds != 0 || before != after {
		t.Fatal("preview moved funds or created refunds", before, after, refunds, err)
	}
}
