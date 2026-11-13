package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"goepay/internal/compat"
	"goepay/internal/cryptopay"
)

func TestCryptoQuoteCNYUsesExactRoundingAtSixDecimals(t *testing.T) {
	for _, tc := range []struct {
		cents      int64
		rate, want string
	}{{7200, "7.2", "10"}, {100, "3", "0.333334"}, {1, "7.2", "0.001389"}, {100, "1", "1"}} {
		got, e := CryptoQuoteCNY(tc.cents, tc.rate)
		if e != nil || got != tc.want {
			t.Fatalf("quote %d at %s = %s want %s: %v", tc.cents, tc.rate, got, tc.want, e)
		}
	}
	for _, rate := range []string{"0", "-1", "NaN", "7.1234567"} {
		if _, e := CryptoQuoteCNY(100, rate); e == nil {
			t.Fatal("invalid trusted quote accepted", rate)
		}
	}
}

func TestCryptoCompatibilityKeepsTheSignedCNYPriceAndNeverUsesTheCNYLedger(t *testing.T) {
	f := email197Local(t)
	f.s.Crypto = &cryptopay.Service{Pool: f.p, TokenKey: make([]byte, 32), Networks: map[string]cryptopay.NetworkConfig{"tron": {Enabled: true}}}
	if e := f.s.Crypto.AddAddress(t.Context(), "tron", "TJRabPrwbZy45sbavfcjinPJC18kjpRTv8", "deposit"); e != nil {
		t.Fatal(e)
	}
	if e := f.p.QueryRow(t.Context(), "SELECT count(*) FROM orders").Scan(new(int)); e != nil {
		t.Fatal(e)
	}
	// Quote settings are supplied by trusted deployment configuration.
	f.s.CryptoCNYPerUSDT = "7.2"
	f.s.CryptoDefaultNetwork = "tron"
	f.s.CryptoPublicOrigin = "https://payments.example"
	p := compat.Params{"pid": "1", "type": "usdt", "out_trade_no": "compat-usdt", "name": "USD invoice", "money": "72.00", "notify_url": "https://merchant.example/notify", "return_url": "https://merchant.example/return", "param": "original-param", "sign_type": "MD5"}
	call := func(path string, p compat.Params) *httptest.ResponseRecorder {
		values := url.Values{}
		for k, v := range p {
			values.Set(k, v)
		}
		r := httptest.NewRequest("POST", path, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	p["sign"] = compat.SignMD5(p, "test-secret")
	w := call("/mapi.php", p)
	var v map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil || v["code"] != float64(1) {
		t.Fatal("signed USDT create", w.Body.String(), e)
	}
	trade, ok := v["trade_no"].(string)
	if !ok || !strings.HasPrefix(trade, "USDT") {
		t.Fatal("distinct crypto trade number", v)
	}
	invoice, e := f.s.Crypto.FindInvoice(t.Context(), 1, trade, "")
	if e != nil || invoice.Amount != "10" || invoice.CNYAmount != 7200 || invoice.Param != "original-param" || invoice.Version != "v1" {
		t.Fatal("CNY quote", invoice, e)
	}
	if !strings.HasPrefix(v["payurl"].(string), "https://payments.example/crypto/checkout/") {
		t.Fatal(v)
	}
	f.s.CryptoCNYPerUSDT = "8"
	f.s.CryptoDefaultNetwork = "ethereum"
	w = call("/mapi.php", p)
	v = nil
	json.Unmarshal(w.Body.Bytes(), &v)
	if v["code"] != float64(1) || v["trade_no"] != trade || v["usdt_amount"] != "10" || v["network"] != "tron" {
		t.Fatal("replayed invoice was repriced or rerouted", w.Body.String())
	}
	p["money"] = "73.00"
	p["sign"] = compat.SignMD5(p, "test-secret")
	w = call("/mapi.php", p)
	v = nil
	json.Unmarshal(w.Body.Bytes(), &v)
	if v["code"] != float64(-1) {
		t.Fatal("replay changed signed CNY price", w.Body.String())
	}
	p["money"] = "72.00"
	p["sign"] = compat.SignMD5(p, "test-secret")
	query := compat.Params{"pid": "1", "key": "test-secret", "act": "order", "trade_no": trade}
	w = call("/api.php", query)
	v = nil
	json.Unmarshal(w.Body.Bytes(), &v)
	if v["money"] != "72.00" || v["usdt_amount"] != "10" || v["type"] != "usdt" {
		t.Fatal("legacy query money semantics", w.Body.String())
	}
	query["act"] = "refund"
	query["money"] = "72.00"
	w = call("/api.php", query)
	v = nil
	json.Unmarshal(w.Body.Bytes(), &v)
	if v["code"] != float64(-1) || !strings.Contains(w.Body.String(), "USDT") {
		t.Fatal("legacy refund fell through", w.Body.String())
	}
	var orders, journals int
	if e = f.p.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM journals)").Scan(&orders, &journals); e != nil || orders != 0 || journals != 0 {
		t.Fatal("USDT touched CNY books", orders, journals, e)
	}
	p["money"] = "144.00"
	w = call("/mapi.php", p)
	v = nil
	json.Unmarshal(w.Body.Bytes(), &v)
	if v["code"] != float64(-1) {
		t.Fatal("unverified price accepted", w.Body.String())
	}
}

func TestCryptoCompatibilityPublicOriginBeforeAllocationAndAbsoluteLinks(t *testing.T) {
	f := email197Local(t)
	f.s.Crypto = &cryptopay.Service{Pool: f.p, TokenKey: make([]byte, 32), Networks: map[string]cryptopay.NetworkConfig{"ethereum": {Enabled: true}}}
	f.s.CryptoCNYPerUSDT = "7.2"
	f.s.CryptoDefaultNetwork = "ethereum"
	for _, address := range []string{"0x1111111111111111111111111111111111111111", "0x2222222222222222222222222222222222222222", "0x3333333333333333333333333333333333333333"} {
		if e := f.s.Crypto.AddAddress(t.Context(), "ethereum", address, "deposit"); e != nil {
			t.Fatal(e)
		}
	}
	request := func(path string, p compat.Params) *httptest.ResponseRecorder {
		values := url.Values{}
		for k, v := range p {
			values.Set(k, v)
		}
		r := httptest.NewRequest("POST", path, strings.NewReader(values.Encode()))
		r.Host = "attacker.example"
		r.Header.Set("X-Forwarded-Host", "attacker-forwarded.example")
		r.Header.Set("Forwarded", "host=attacker-forwarded.example;proto=https")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	params := func(order string) compat.Params {
		p := compat.Params{"pid": "1", "type": "usdt", "out_trade_no": order, "name": "Origin safety", "money": "72.00", "notify_url": "https://merchant.example/notify", "return_url": "https://merchant.example/return", "sign_type": "MD5"}
		p["sign"] = compat.SignMD5(p, "test-secret")
		return p
	}
	for i, origin := range []string{"", "http://payments.example", "https://user%40other.example@payments.example", "https://payments.example/path", "https://payments.example/%2F", "https://payments.example?", "https://payments.example#", "https://payments.example/?q=1"} {
		f.s.CryptoPublicOrigin = origin
		w := request("/mapi.php", params(fmt.Sprintf("bad-origin-%d", i)))
		var result map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil || result["code"] != float64(-1) {
			t.Fatal("invalid origin admitted", origin, w.Body.String(), e)
		}
		var invoices, used int
		if e := f.p.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM crypto_invoices),(SELECT count(*) FROM crypto_addresses WHERE used)").Scan(&invoices, &used); e != nil || invoices != 0 || used != 0 {
			t.Fatal("invalid public origin consumed an address", origin, invoices, used, e)
		}
	}
	f.s.CryptoPublicOrigin = "https://payments.example/"
	legacy := params("legacy-public-origin")
	w := request("/mapi.php", legacy)
	var first map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &first); e != nil || first["code"] != float64(1) {
		t.Fatal("legacy create", w.Body.String(), e)
	}
	assertLink := func(data map[string]any, fields ...string) string {
		t.Helper()
		var first string
		for _, field := range fields {
			raw, ok := data[field].(string)
			if !ok {
				t.Fatal("missing absolute checkout link", field, data)
			}
			u, e := url.Parse(raw)
			if e != nil || u.Scheme != "https" || u.Host != "payments.example" || !strings.HasPrefix(u.Path, "/crypto/checkout/") || len(u.Query().Get("token")) != 43 || strings.Contains(raw, "attacker") {
				t.Fatal("untrusted checkout origin", field, raw, e)
			}
			if first != "" && raw != first {
				t.Fatal("checkout material links disagree")
			}
			first = raw
		}
		return first
	}
	absolute := assertLink(first, "payurl", "pay_info", "url")
	w = request("/submit.php", legacy)
	if w.Code != 302 || w.Header().Get("Location") != absolute {
		t.Fatal("page submit lost trusted checkout origin", w.Code, w.Header().Get("Location"))
	}
	f.s.CryptoCNYPerUSDT = ""
	f.s.CryptoDefaultNetwork = ""
	w = request("/mapi.php", legacy)
	var replay map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &replay); e != nil || replay["code"] != float64(1) || replay["trade_no"] != first["trade_no"] || replay["usdt_amount"] != "10" || replay["network"] != "ethereum" || assertLink(replay, "payurl", "pay_info", "url") != absolute {
		t.Fatal("disabling new quotes altered an original invoice", w.Body.String(), e)
	}
	w = request("/mapi.php", params("new-quote-disabled"))
	var refused map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &refused); e != nil || refused["code"] != float64(-1) {
		t.Fatal("disabled new quote was created", w.Body.String(), e)
	}
	var used int
	if e := f.p.QueryRow(t.Context(), "SELECT count(*) FROM crypto_addresses WHERE used").Scan(&used); e != nil || used != 1 {
		t.Fatal("disabled quote consumed another address", used, e)
	}
	f.s.CryptoCNYPerUSDT = "8"
	f.s.CryptoDefaultNetwork = "tron"
	w = request("/mapi.php", legacy)
	if e := json.Unmarshal(w.Body.Bytes(), &replay); e != nil || replay["usdt_amount"] != "10" || replay["network"] != "ethereum" || replay["trade_no"] != first["trade_no"] {
		t.Fatal("origin replay used a changed quote or default network", w.Body.String(), e)
	}
	assertQueryNotificationAmounts := func(query compat.Params, route string) {
		t.Helper()
		w := request(route, query)
		var result map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil || result["money"] != "72.00" || result["usdt_amount"] != "10" {
			t.Fatal("query changed original monetary terms", w.Body.String(), e)
		}
		invoice, e := f.s.Crypto.FindInvoice(t.Context(), 1, query["trade_no"], "")
		if e != nil {
			t.Fatal(e)
		}
		merchant, e := f.s.Store.Merchant(t.Context(), 1)
		if e != nil {
			t.Fatal(e)
		}
		callback, e := cryptopay.NotificationURL(invoice.NotifyURL, cryptopay.Invoice{ID: invoice.ID, MerchantID: invoice.MerchantID, OutTradeNo: invoice.OutTradeNo, Network: invoice.Network, Amount: invoice.Amount, State: "paid", CNYAmount: invoice.CNYAmount, Version: invoice.Version, Name: invoice.Name, Param: invoice.Param}, merchant, f.s.PlatformKey, time.Now(), false)
		if e != nil {
			t.Fatal(e)
		}
		u, e := url.Parse(callback)
		if e != nil || u.Query().Get("money") != result["money"] || u.Query().Get("crypto_amount") != result["usdt_amount"] {
			t.Fatal("query and asynchronous notification monetary fields differ", result, e)
		}
	}
	assertQueryNotificationAmounts(compat.Params{"pid": "1", "key": "test-secret", "act": "order", "trade_no": first["trade_no"].(string)}, "/api.php")
	f.s.CryptoCNYPerUSDT = "7.2"
	f.s.CryptoDefaultNetwork = "ethereum"
	platformKey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	f.s.PlatformKey = platformKey
	public, e := x509.MarshalPKIXPublicKey(&f.s.PlatformKey.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(t.Context(), "UPDATE merchants SET signature_algorithm='RSA',keytype=1,rsa_public_key=$1 WHERE id=1", string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}))); e != nil {
		t.Fatal(e)
	}
	v2 := params("v2-public-origin")
	v2["sign_type"] = "RSA"
	v2["timestamp"] = fmt.Sprint(time.Now().Unix())
	v2["sign"], e = compat.SignRSA(v2, f.s.PlatformKey)
	if e != nil {
		t.Fatal(e)
	}
	w = request("/api/pay/create", v2)
	var second map[string]any
	if e = json.Unmarshal(w.Body.Bytes(), &second); e != nil || second["code"] != float64(0) {
		t.Fatal("v2 create", w.Body.String(), e)
	}
	assertLink(second, "pay_info", "url")
	query := compat.Params{"pid": "1", "trade_no": second["trade_no"].(string), "sign_type": "RSA", "timestamp": fmt.Sprint(time.Now().Unix())}
	query["sign"], e = compat.SignRSA(query, f.s.PlatformKey)
	if e != nil {
		t.Fatal(e)
	}
	assertQueryNotificationAmounts(query, "/api/pay/query")
	for _, data := range []map[string]any{first, second} {
		if data["money"] != "72.00" || data["usdt_amount"] != "10" || data["network"] != "ethereum" {
			t.Fatal("origin change altered original monetary terms", data)
		}
	}
	f.s.CryptoCNYPerUSDT = ""
	f.s.CryptoPublicOrigin = ""
	r := httptest.NewRequest("POST", "http://127.0.0.1/console/v1/merchant/crypto/invoices", strings.NewReader(`{"out_trade_no":"native-no-origin","network":"ethereum","amount":"2.5"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.token})
	w = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var native struct {
		Data struct {
			CheckoutURL string `json:"checkout_url"`
		}
	}
	if e = json.Unmarshal(w.Body.Bytes(), &native); e != nil || w.Code != 200 || native.Data.CheckoutURL == "" {
		t.Fatal("native create depends on CNY origin", w.Code, w.Body.String(), e)
	}
	w = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1"+native.Data.CheckoutURL, nil))
	if w.Code != 200 {
		t.Fatal("native loopback checkout needs compatibility configuration", w.Code, w.Body.String())
	}
}
