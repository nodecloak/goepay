package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"goepay/internal/compat"
)

func cryptoQuoteExpiryRequest(t *testing.T, f *email197Fixture, order string) *httptest.ResponseRecorder {
	t.Helper()
	p := compat.Params{"pid": "1", "type": "usdt", "out_trade_no": order, "name": "Quoted invoice", "money": "72.00", "sign_type": "MD5"}
	p["sign"] = compat.SignMD5(p, "test-secret")
	form := url.Values{}
	for k, v := range p {
		form.Set(k, v)
	}
	r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func TestCryptoQuoteWithoutDeadlineStopsNewSignedOrder(t *testing.T) {
	f := cryptoAdmissionFixture(t)
	f.s.CryptoQuoteValidUntil = time.Time{}
	w := cryptoQuoteExpiryRequest(t, f, "quote-missing-expiry")
	var reply struct{ Code int }
	if err := json.Unmarshal(w.Body.Bytes(), &reply); w.Code != http.StatusOK || err != nil || reply.Code != -1 {
		t.Fatalf("a quote with no deadline was accepted: %d %s", w.Code, w.Body.String())
	}
	var used int
	if err := f.p.QueryRow(t.Context(), "SELECT count(*) FROM crypto_addresses WHERE used").Scan(&used); err != nil || used != 0 {
		t.Fatalf("inactive quote consumed an address: %d %v", used, err)
	}
}

func TestCryptoExpiredQuoteStopsCashierAndPreservesOriginal(t *testing.T) {
	f := cryptoAdmissionFixture(t)
	tron, ethereum := cryptoAdmissionChannels(t, f)
	if err := f.s.Crypto.AddAddress(t.Context(), "ethereum", "0x2222222222222222222222222222222222222222", "deposit"); err != nil {
		t.Fatal(err)
	}
	first := cryptoQuoteExpiryRequest(t, f, "quoted-original")
	var original struct {
		Code int
		URL  string
	}
	if err := json.Unmarshal(first.Body.Bytes(), &original); err != nil || original.Code != 1 || original.URL == "" {
		t.Fatalf("valid quote was rejected: %s", first.Body.String())
	}
	invoice, err := f.s.Crypto.FindInvoice(t.Context(), 1, "", "quoted-original")
	if err != nil || invoice.ChannelID != tron.ID || invoice.Amount != "10" || invoice.CNYAmount != 7200 {
		t.Fatalf("unexpected original invoice: %+v %v", invoice, err)
	}
	var snapshot struct {
		ValidUntil time.Time `json:"quote_valid_until"`
	}
	if err := json.Unmarshal(invoice.ChannelSnapshot, &snapshot); err != nil || !snapshot.ValidUntil.Equal(f.s.CryptoQuoteValidUntil) {
		t.Fatalf("quote deadline was not captured with the original invoice: %s", invoice.ChannelSnapshot)
	}
	cashier := func(method, order, name string) *httptest.ResponseRecorder {
		intent := cashierIntent{MerchantID: 1, OutTradeNo: order, Name: name, Money: "72.00", Expires: time.Now().Add(time.Hour).Unix()}
		r := httptest.NewRequest(method, f.s.cashierURL(intent), strings.NewReader("type=usdt"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	before := cashier(http.MethodGet, "quote-cashier-new", "Cashier invoice")
	if before.Code != http.StatusOK || !strings.Contains(before.Body.String(), `value="usdt"`) {
		t.Fatal("cashier omitted a valid USDT quote")
	}
	f.s.CryptoQuoteValidUntil = time.Now().Add(-time.Minute)
	after := cashier(http.MethodGet, "quote-cashier-new", "Cashier invoice")
	if after.Code != http.StatusOK || strings.Contains(after.Body.String(), `value="usdt"`) {
		t.Fatal("cashier still advertises an expired quote")
	}
	stale := cashier(http.MethodPost, "quote-cashier-new", "Cashier invoice")
	if stale.Code != http.StatusBadRequest || !strings.Contains(stale.Body.String(), "usdt_quote_unavailable") || stale.Header().Get("Location") != "" {
		t.Fatalf("stale checkout did not report quote expiry: %d %s", stale.Code, stale.Body.String())
	}
	w := cryptoQuoteExpiryRequest(t, f, "quote-signed-new")
	var refusal struct {
		Code int
		Msg  string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &refusal); err != nil || refusal.Code != -1 || refusal.Msg != cryptoQuoteUnavailable {
		t.Fatalf("expired signed order was not rejected: %s", w.Body.String())
	}
	replay := cashier(http.MethodPost, "quoted-original", "Quoted invoice")
	if replay.Code != http.StatusFound || replay.Header().Get("Location") != original.URL {
		t.Fatalf("expired current quote blocked replay: %d %s", replay.Code, replay.Body.String())
	}
	var invoices, used int
	if err := f.p.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM crypto_invoices),(SELECT count(*) FROM crypto_addresses WHERE used)").Scan(&invoices, &used); err != nil || invoices != 1 || used != 1 {
		t.Fatalf("expired quote changed order or address counts: %d %d %v", invoices, used, err)
	}
	native := cryptoAdmissionConsole(f, "POST", "/invoices", `{"out_trade_no":"native-after-quote-expiry","channel_id":"`+fmt.Sprint(ethereum.ID)+`","amount":"2.5"}`)
	if native.Code != http.StatusOK {
		t.Fatalf("native USDT depends on the CNY quote: %d %s", native.Code, native.Body.String())
	}
}
