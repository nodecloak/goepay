package printing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"net/http"
	"testing"
)

func TestPrintingOriginalRechargeIsExcludedFromAutomaticOrderPrintingEvenInAllOrdersMode(t *testing.T) {
	s, p := printFixture(t)
	configurePrinting(t, s, 2)
	ctx := context.Background()
	o, e := p.CreateRechargeOrder(ctx, payment.CreateOrderInput{MerchantID: 901, OutTradeNo: "original-recharge-print-exclusion", Name: "普通订单文字不能改变充值来源", Amount: 123, Type: "wxpay"}, "local-only")
	if e != nil {
		t.Fatal(e)
	}
	verifyPrintTestOrder(t, p, o)
	f, e := p.PrintingPaidSource(ctx, s.pool, o.ID)
	if e != nil || f.Origin != "recharge" || !f.Fresh || f.SnapshotState != "captured" || f.Mode != 2 || !f.PlatformEnabled {
		t.Fatal("fixture did not reach an otherwise enabled print source", f, e)
	}
	for i := 0; i < 2; i++ {
		if job, e := s.PrepareAutomatic(ctx, o.ID); e != nil || job != nil {
			t.Fatal("recharge expanded into an ordinary automatic print", job, e)
		}
	}
	flow := payment.ProviderFlow{Store: p, Providers: map[int64]adapter.Provider{7: printPaymentVerifier{}}}
	body := []byte(o.TradeNo + ":123")
	mac := hmac.New(sha256.New, []byte("local-only-payment-signer"))
	mac.Write(body)
	if e = flow.HandleNotification(ctx, 7, o.TradeNo, http.Header{"Local-Signature": {hex.EncodeToString(mac.Sum(nil))}}, body); e != nil {
		t.Fatal(e)
	}
	if job, e := s.PrepareAutomatic(ctx, o.ID); e != nil || job != nil {
		t.Fatal("duplicate recharge callback created an automatic print", job, e)
	}
	var intents int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM printing_intents WHERE order_id=$1`, o.ID).Scan(&intents); e != nil || intents != 0 {
		t.Fatal("recharge acquired a print intent", intents, e)
	}
	balance, e := p.Balance(ctx, 901)
	if e != nil || balance.Available != 123 || balance.Reserved != 0 {
		t.Fatal("printing scope changed original recharge credit", balance, e)
	}
	ordinary, _, _, _ := printPaidOrder(t, p, "original-ordinary-print-positive")
	job, e := s.PrepareAutomatic(ctx, ordinary.ID)
	if e != nil || job == nil || job.OrderID != ordinary.ID {
		t.Fatal("ordinary paid order lost automatic printing", job, e)
	}
}
