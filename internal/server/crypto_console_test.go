package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/cryptopay"
	"goepay/internal/identity"
)

func TestCryptoMerchantScopeNeverAllowsAnEmptyMerchantPrincipal(t *testing.T) {
	if _, e := cryptoMerchant("merchant", identity.Session{}, ""); e == nil {
		t.Fatal("empty merchant principal acquired the global read scope")
	}
	if _, e := cryptoMerchant("merchant", identity.Session{User: identity.User{ID: 1}}, "2"); e == nil {
		t.Fatal("merchant selected another merchant")
	}
	if v, e := cryptoMerchant("merchant", identity.Session{User: identity.User{ID: 1}}, ""); e != nil || v != 1 {
		t.Fatal(v, e)
	}
}

func TestCryptoConsoleMerchantScopeCheckoutAndSensitiveWrites(t *testing.T) {
	f := email197Local(t)
	svc := &cryptopay.Service{Pool: f.p, TokenKey: make([]byte, 32), Networks: map[string]cryptopay.NetworkConfig{"tron": {Enabled: true, FeeBPS: 50}, "ethereum": {Enabled: true, FeeBPS: 80}}}
	f.s.Crypto = svc
	const address = "TJRabPrwbZy45sbavfcjinPJC18kjpRTv8"
	if e := svc.AddAddress(t.Context(), "tron", address, "deposit"); e != nil {
		t.Fatal(e)
	}
	req := func(method, path, body, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/merchant/crypto"+path, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.197:12345"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		path, body, csrf string
		status           int
	}{
		{"/invoices", `{"out_trade_no":"forged","network":"tron","amount":"10","merchant_id":"2"}`, f.csrf, 403},
		{"/invoices", `{"out_trade_no":"fee","network":"tron","amount":"10","fee_bps":0}`, f.csrf, 400},
		{"/invoices", `{"out_trade_no":"csrf","network":"tron","amount":"10"}`, "incorrect", 403},
		{"/addresses", `{"network":"tron","address":"` + address + `","role":"deposit"}`, f.csrf, 404},
		{"/withdrawals/other/approve", `{"current_password":"local-email197-password"}`, f.csrf, 404},
	} {
		w := req("POST", tc.path, tc.body, tc.csrf)
		if w.Code != tc.status {
			t.Fatalf("%s HTTP=%d expected%d body=%s", tc.path, w.Code, tc.status, w.Body.String())
		}
	}
	w := req("POST", "/invoices", `{"out_trade_no":"own-invoice","network":"tron","amount":"10.123456","notify_url":"https://merchant.example/notify"}`, f.csrf)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		Data struct {
			ID, CheckoutURL, Address string
			MerchantID               string `json:"merchant_id"`
			Checkout                 string `json:"checkout_url"`
		}
	}
	if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if result.Data.MerchantID != "1" || result.Data.Checkout == "" || strings.Contains(w.Body.String(), "checkout_token") {
		t.Fatal("invoice scope or token envelope", w.Body.String())
	}
	for _, path := range []string{"/invoices?merchant_id=2", "/balances?merchant_id=2", "/withdrawals?merchant_id=2"} {
		if w = req("GET", path, "", f.csrf); w.Code != 403 {
			t.Fatal("cross-merchant read", path, w.Code)
		}
	}
	for _, suffix := range []string{"", "/status", "/qr"} {
		checkout, e := svc.GetInvoice(t.Context(), 1, result.Data.ID)
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest("GET", "/crypto/checkout/"+checkout.ID+suffix+"?token="+checkout.CheckoutToken, nil)
		w = httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("token checkout", suffix, w.Code)
		}
		if strings.Contains(w.Body.String(), "merchant.example/notify") {
			t.Fatal("checkout disclosed callback")
		}
		if suffix == "" && (!strings.Contains(w.Body.String(), "margin:20px auto") || !strings.Contains(w.Body.String(), "nonce=")) {
			t.Fatal("uncentered or unprotected checkout")
		}
		r = httptest.NewRequest("GET", "/crypto/checkout/"+checkout.ID+suffix+"?token="+strings.Repeat("x", 43), nil)
		w = httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatal("token bypass", w.Code)
		}
	}
	paidInvoice, e := svc.GetInvoice(t.Context(), 1, result.Data.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e := svc.RecordDeposit(t.Context(), cryptopay.Deposit{Network: "tron", TxID: "synthetic-console", BlockHash: "local-block", BlockHeight: 1, Address: address, Amount: "10.123456", OccurredAt: paidInvoice.ExpiresAt.Add(-time.Minute)}); e != nil {
		t.Fatal(e)
	}
	const recipientAddress = "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE"
	w = req("POST", "/recipients", `{"network":"tron","address":"`+recipientAddress+`","current_password":"local-email197-password"}`, f.csrf)
	if w.Code != 200 {
		t.Fatal("request recipient", w.Code, w.Body.String())
	}
	var recipient struct{ Data cryptopay.Recipient }
	if e := json.Unmarshal(w.Body.Bytes(), &recipient); e != nil {
		t.Fatal(e)
	}
	if _, e := svc.ApproveRecipient(t.Context(), recipient.Data.ID, 168); e != nil {
		t.Fatal(e)
	}
	base := `{"request_id":"withdraw-local","network":"tron","address":"` + recipientAddress + `","amount":"1","current_password":"`
	if w = req("POST", "/withdrawals", base+`incorrect"}`, f.csrf); w.Code != 422 {
		t.Fatal("wrong password accepted", w.Code, w.Body.String())
	}
	if w = req("POST", "/withdrawals", base+`local-email197-password"}`, f.csrf); w.Code != 409 {
		t.Fatal("recipient cooling bypass", w.Code, w.Body.String())
	}
	if _, e := f.p.Exec(t.Context(), "UPDATE crypto_recipients SET usable_at=clock_timestamp()-interval '1 second' WHERE id=$1", recipient.Data.ID); e != nil {
		t.Fatal(e)
	}
	if w = req("POST", "/withdrawals", base+`local-email197-password"}`, f.csrf); w.Code != 200 {
		t.Fatal("withdraw request", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "password") {
		t.Fatal("password reflected")
	}
	var count int
	if e := f.p.QueryRow(t.Context(), "SELECT count(*) FROM crypto_withdrawals WHERE state='requested' AND merchant_id=1").Scan(&count); e != nil || count != 1 {
		t.Fatal("unapproved withdrawal state", count, e)
	}
	var outgoing struct{ Data cryptopay.Withdrawal }
	if e := json.Unmarshal(w.Body.Bytes(), &outgoing); e != nil {
		t.Fatal(e)
	}
	w = req("POST", "/withdrawals/"+outgoing.Data.ID+"/cancel", `{"current_password":"local-email197-password"}`, f.csrf)
	if w.Code != 200 {
		t.Fatal("cancel request", w.Code, w.Body.String())
	}
	if e := svc.AddAddress(t.Context(), "tron", recipientAddress, "deposit"); e != nil {
		t.Fatal(e)
	}
	heldInvoice, e := svc.CreateInvoice(t.Context(), cryptopay.CreateInvoiceInput{MerchantID: 1, OutTradeNo: "underpaid-held", Network: "tron", Amount: "10"})
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.RecordDeposit(t.Context(), cryptopay.Deposit{Network: "tron", TxID: "synthetic-held", BlockHash: "held-block", BlockHeight: 2, Address: recipientAddress, Amount: "4", OccurredAt: heldInvoice.ExpiresAt.Add(-time.Minute)}); e != nil {
		t.Fatal(e)
	}
	w = req("POST", "/held-refunds", `{"request_id":"held-refund","invoice_id":"`+heldInvoice.ID+`","address":"`+recipientAddress+`","amount":"2","current_password":"local-email197-password"}`, f.csrf)
	if w.Code != 200 {
		t.Fatal("held-fund return request", w.Code, w.Body.String())
	}
	var held struct{ Data cryptopay.Withdrawal }
	if e = json.Unmarshal(w.Body.Bytes(), &held); e != nil || held.Data.Kind != "exception_refund" {
		t.Fatal("held-fund scope", held, e)
	}
	w = req("POST", "/withdrawals/"+held.Data.ID+"/cancel", `{"current_password":"local-email197-password"}`, f.csrf)
	if w.Code != 200 {
		t.Fatal("cancel held return", w.Code, w.Body.String())
	}
	w = req("POST", "/recipients/"+recipient.Data.ID+"/revoke", `{"current_password":"local-email197-password"}`, f.csrf)
	if w.Code != 200 {
		t.Fatal("revoke recipient", w.Code, w.Body.String())
	}
	b, e := svc.Balances(t.Context(), 1)
	if e != nil || len(b) != 1 || b[0].Reserved != "0" {
		t.Fatal("cancel did not release reserve", b, e)
	}
}
