package server

import (
	"encoding/json"
	"goepay/internal/compat"
	"goepay/internal/cryptopay"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestCryptoCheckoutReturnIsSignedOnlyAfterVerifiedPayment(t *testing.T) {
	f := email197Local(t)
	s := &cryptopay.Service{Pool: f.p, TokenKey: make([]byte, 32), Networks: map[string]cryptopay.NetworkConfig{"tron": {Enabled: true}}}
	f.s.Crypto = s
	if e := s.AddAddress(t.Context(), "tron", "TJRabPrwbZy45sbavfcjinPJC18kjpRTv8", "deposit"); e != nil {
		t.Fatal(e)
	}
	v, e := s.CreateInvoice(t.Context(), cryptopay.CreateInvoiceInput{MerchantID: 1, OutTradeNo: "signed-return", Network: "tron", Amount: "10", CNYAmount: 7200, Version: "v1", ReturnURL: "https://merchant.example/return?custom=1"})
	if e != nil {
		t.Fatal(e)
	}
	status := func() map[string]any {
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/crypto/checkout/"+v.ID+"/status?token="+url.QueryEscape(v.CheckoutToken), nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var r struct{ Data map[string]any }
		if e = json.Unmarshal(w.Body.Bytes(), &r); e != nil {
			t.Fatal(e)
		}
		return r.Data
	}
	if p := status(); p["return_url"] != nil && p["return_url"] != "" {
		t.Fatal("unpaid return exposed", p)
	}
	if e = s.RecordDeposit(t.Context(), cryptopay.Deposit{Network: "tron", TxID: "signed-return-payment", BlockHeight: 1, BlockHash: "b", Address: v.Address, Amount: "10", OccurredAt: v.ExpiresAt.Add(-time.Minute)}); e != nil {
		t.Fatal(e)
	}
	p := status()
	target, ok := p["return_url"].(string)
	if !ok || target == "" {
		t.Fatal("missing signed return", p)
	}
	u, e := url.Parse(target)
	if e != nil {
		t.Fatal(e)
	}
	params, e := compat.Parse(u.Query())
	if e != nil {
		t.Fatal(e)
	}
	m, e := f.s.Store.Merchant(t.Context(), 1)
	if e != nil {
		t.Fatal(e)
	}
	if params["money"] != "72.00" || params["trade_status"] != "TRADE_SUCCESS" || params["sign"] != compat.SignMD5(params, m.APIKey) || params["network"] != "tron" {
		t.Fatal(params)
	}
}
