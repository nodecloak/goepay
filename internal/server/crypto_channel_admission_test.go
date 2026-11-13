package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/compat"
	"goepay/internal/cryptopay"
)

func cryptoAdmissionFixture(t *testing.T) *email197Fixture {
	t.Helper()
	f := email197Local(t)
	f.s.Crypto = &cryptopay.Service{Pool: f.p, TokenKey: make([]byte, 32), Networks: map[string]cryptopay.NetworkConfig{"tron": {Enabled: true}}}
	f.s.CryptoCNYPerUSDT, f.s.CryptoDefaultNetwork, f.s.CryptoPublicOrigin = "7.2", "tron", "https://payments.example"
	if err := f.s.Crypto.AddAddress(t.Context(), "tron", "TJRabPrwbZy45sbavfcjinPJC18kjpRTv8", "deposit"); err != nil {
		t.Fatal(err)
	}
	return f
}

func cryptoAdmissionChannels(t *testing.T, f *email197Fixture) (cryptopay.Channel, cryptopay.Channel) {
	t.Helper()
	f.s.Crypto.RequireChannels = true
	f.s.Crypto.Networks["ethereum"] = cryptopay.NetworkConfig{Enabled: true}
	if _, err := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('pay-types','{"type":"usdt","enabled":true}'); UPDATE backoffice_records SET body=jsonb_set(body,'{info}','{"usdt":{"rate":"99.5","channel":"-5","type":"channel"}}') WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	ctx := cryptopay.WithAuthority(t.Context(), func(context.Context, pgx.Tx) error { return nil })
	makeChannel := func(network, key string) cryptopay.Channel {
		v, err := f.s.Crypto.SaveChannel(ctx, cryptopay.SaveChannelInput{ActorID: 168, RequestID: strings.Repeat(key, 32), Name: "USDT " + network, Network: network, Enabled: true, FeeBPS: 100, MinAmount: "0", MaxAmount: "100", DayLimit: "1000", ReservedIDs: []int64{7, 8}})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	return makeChannel("tron", "a"), makeChannel("ethereum", "b")
}

func TestCryptoUnifiedChannelAcrossConsoleAPIAndCashier(t *testing.T) {
	f := cryptoAdmissionFixture(t)
	tron, ethereum := cryptoAdmissionChannels(t, f)
	for _, a := range []string{"0x1111111111111111111111111111111111111111", "0x2222222222222222222222222222222222222222"} {
		if err := f.s.Crypto.AddAddress(t.Context(), "ethereum", a, "deposit"); err != nil {
			t.Fatal(err)
		}
	}
	w := cryptoAdmissionConsole(f, "GET", "/channels", "")
	var choices struct {
		Data struct {
			Items []struct {
				ID     string
				FeeBPS int64 `json:"fee_bps"`
			}
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &choices); err != nil || w.Code != 200 || len(choices.Data.Items) != 2 {
		t.Fatalf("choices HTTP %d %s %v", w.Code, w.Body.String(), err)
	}
	if strings.Contains(w.Body.String(), "merchant_ids") || choices.Data.Items[0].FeeBPS != 50 {
		t.Fatal("choices leaked grants or ignored effective group fee", w.Body.String())
	}
	w = cryptoAdmissionConsole(f, "GET", "/channels?merchant_id=2", "")
	if w.Code != 403 {
		t.Fatal("channel choices crossed merchant scope", w.Code)
	}
	w = cryptoAdmissionConsole(f, "POST", "/invoices", `{"out_trade_no":"console-chan","channel_id":"`+strconv.FormatInt(ethereum.ID, 10)+`","network":"tron","amount":"10"}`)
	if w.Code < 400 {
		t.Fatal("channel-network mismatch accepted")
	}
	w = cryptoAdmissionConsole(f, "POST", "/invoices", `{"out_trade_no":"console-chan","channel_id":"`+strconv.FormatInt(tron.ID, 10)+`","amount":"10"}`)
	if w.Code != 200 {
		t.Fatal("console channel collection", w.Code, w.Body.String())
	}
	invoice, err := f.s.Crypto.FindInvoice(t.Context(), 1, "", "console-chan")
	if err != nil || invoice.ChannelID != tron.ID || invoice.Fee != "0.05" || invoice.FeeSource != "group" {
		t.Fatal("console route", invoice, err)
	}
	p := compat.Params{"pid": "1", "type": "usdt", "channel_id": strconv.FormatInt(ethereum.ID, 10), "out_trade_no": "api-chan", "name": "channel test", "money": "72.00", "sign_type": "MD5"}
	p["sign"] = compat.SignMD5(p, "test-secret")
	values := url.Values{}
	for k, v := range p {
		values.Set(k, v)
	}
	r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var wire struct{ Code int }
	if err = json.Unmarshal(w.Body.Bytes(), &wire); err != nil || wire.Code != 1 {
		t.Fatal("compat channel", w.Body.String(), err)
	}
	apiInvoice, err := f.s.Crypto.FindInvoice(t.Context(), 1, "", "api-chan")
	if err != nil || apiInvoice.Network != "ethereum" || apiInvoice.ChannelID != ethereum.ID || apiInvoice.Fee != "0.05" {
		t.Fatal("API ignored selected channel", apiInvoice, err)
	}
	// The group's explicit channel outranks a different deployment default.
	f.s.CryptoDefaultNetwork = "tron"
	if _, err = f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=jsonb_set(body,'{info}',jsonb_build_object('usdt',jsonb_build_object('rate','99.5','channel',$1::text,'type','channel'))) WHERE resource='groups' AND id=0`, strconv.FormatInt(ethereum.ID, 10)); err != nil {
		t.Fatal(err)
	}
	intent := cashierIntent{Version: "v1", MerchantID: 1, OutTradeNo: "cashier-chan", Name: "cashier test", Money: "72.00", ClientIP: "192.0.2.4", Expires: time.Now().Add(time.Minute).Unix()}
	endpoint := f.s.cashierURL(intent)
	r = httptest.NewRequest("POST", endpoint, strings.NewReader("type=usdt"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 302 || !strings.Contains(w.Header().Get("Location"), "/crypto/checkout/") {
		t.Fatal("cashier didn't dispatch USDT", w.Code, w.Body.String())
	}
	cashierInvoice, err := f.s.Crypto.FindInvoice(t.Context(), 1, "", "cashier-chan")
	if err != nil || cashierInvoice.ChannelID != ethereum.ID || cashierInvoice.Fee != "0.05" {
		t.Fatal("cashier route", cashierInvoice, err)
	}
	w = cryptoAdmissionConsole(f, "GET", "/invoices?channel_id="+strconv.FormatInt(tron.ID, 10), "")
	var records struct{ Data []cryptopay.Invoice }
	if err = json.Unmarshal(w.Body.Bytes(), &records); err != nil || len(records.Data) != 1 || records.Data[0].ID != invoice.ID {
		t.Fatal("channel order filter", w.Body.String(), err)
	}
	if _, err = f.p.Exec(t.Context(), `UPDATE crypto_channels SET enabled=false; UPDATE backoffice_records SET body='{"type":"usdt","enabled":false}' WHERE resource='pay-types' AND body->>'type'='usdt'`); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("POST", endpoint, strings.NewReader("type=usdt"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 302 || !strings.Contains(w.Header().Get("Location"), cashierInvoice.ID) {
		t.Fatal("cashier replay lost original", w.Code, w.Body.String())
	}
	var cnyOrders int
	if err = f.p.QueryRow(t.Context(), "SELECT count(*) FROM orders").Scan(&cnyOrders); err != nil || cnyOrders != 0 {
		t.Fatal("USDT entered CNY orders", cnyOrders, err)
	}
}

func cryptoAdmissionConsole(f *email197Fixture, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/merchant/crypto"+path, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.197:12345"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func TestCryptoCompatibilityDoesNotIgnoreExplicitChannel(t *testing.T) {
	f := cryptoAdmissionFixture(t)
	for _, extra := range []compat.Params{{"channel_id": "999999"}, {"channel_id": "01"}, {"subchannel_id": "1"}} {
		p := compat.Params{"pid": "1", "type": "usdt", "out_trade_no": "explicit-channel", "name": "test", "money": "72.00", "sign_type": "MD5"}
		for k, v := range extra {
			p[k] = v
		}
		p["sign"] = compat.SignMD5(p, "test-secret")
		values := url.Values{}
		for k, v := range p {
			values.Set(k, v)
		}
		r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		var out struct{ Code int }
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.Code != -1 {
			t.Errorf("explicit channel was silently ignored: %v", extra)
		}
	}
	var invoices, used int
	if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM crypto_invoices),(SELECT count(*) FROM crypto_addresses WHERE used)`).Scan(&invoices, &used); err != nil {
		t.Fatal(err)
	}
	if invoices != 0 || used != 0 {
		t.Fatalf("rejected requests allocated funds state: invoices=%d used=%d", invoices, used)
	}
}

func TestCryptoConsoleInvoiceHonorsMerchantPaymentRestriction(t *testing.T) {
	f := cryptoAdmissionFixture(t)
	if _, err := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,owner_id,body) VALUES('merchant-risk',1,'{"status":"restricted"}')`); err != nil {
		t.Fatal(err)
	}
	w := cryptoAdmissionConsole(f, "POST", "/invoices", `{"out_trade_no":"risk-blocked","network":"tron","amount":"10"}`)
	if w.Code < 400 {
		t.Fatalf("restricted merchant created a console invoice: HTTP %d", w.Code)
	}
	var count int
	if err := f.p.QueryRow(t.Context(), `SELECT count(*) FROM crypto_invoices`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("restricted invoice persisted")
	}
}

func TestCryptoConsoleClosedChannelChoicesAreEmpty(t *testing.T) {
	f := email197Local(t)
	w := cryptoAdmissionConsole(f, "GET", "/channels", "")
	var out struct {
		Code int
		Data struct{ Items []any }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || out.Code != 0 || out.Data.Items == nil || len(out.Data.Items) != 0 {
		t.Fatalf("closed choices should be an empty list: HTTP %d %s %v", w.Code, w.Body.String(), err)
	}
}

func TestCryptoCollectionRejectsRuntimeChannelIdentityCollision(t *testing.T) {
	f := cryptoAdmissionFixture(t)
	tron, _ := cryptoAdmissionChannels(t, f)
	for _, provider := range f.s.ProviderFlow.Providers {
		f.s.ProviderFlow.Providers[tron.ID] = provider
		break
	}
	w := cryptoAdmissionConsole(f, "POST", "/invoices", `{"out_trade_no":"identity-conflict","channel_id":"`+strconv.FormatInt(tron.ID, 10)+`","amount":"10"}`)
	if w.Code < 400 {
		t.Fatal("ambiguous channel ID admitted USDT")
	}
	var count int
	if err := f.p.QueryRow(t.Context(), "SELECT count(*) FROM crypto_invoices").Scan(&count); err != nil || count != 0 {
		t.Fatal("collision allocated invoice", count, err)
	}
}
