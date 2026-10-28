package payment

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func paypalChannel(mode, currency, rate string) PluginChannel {
	return PluginChannel{ID: 11, Plugin: "paypal", PayType: "paypal", Config: map[string]string{
		"appid": "client-abc", "appkey": "secret-xyz", "appswitch": mode, "currency_code": currency, "currency_rate": rate,
	}}
}

func TestPluginPaypalRegistryAndBuild(t *testing.T) {
	spec, ok := LookupPlugin("paypal")
	if !ok || spec.Title != "PayPal" || !spec.Serves("paypal") || spec.Serves("alipay") {
		t.Fatal("spec", spec, ok)
	}
	if _, e := BuildPluginChannel(paypalChannel("0", "USD", "0.137")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{
		paypalChannel("0", "USD", "0.137"), // will mutate below
	}
	bad[0].PayType = "alipay" // type not served
	missing := paypalChannel("0", "USD", "")
	delete(missing.Config, "appkey")
	badRate := paypalChannel("0", "USD", "abc")
	badCur := paypalChannel("0", "ZZZ", "1")
	badMode := paypalChannel("9", "USD", "1")
	bad = append(bad, missing, badRate, badCur, badMode)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginPaypalAmountRate(t *testing.T) {
	p, e := BuildPluginChannel(paypalChannel("0", "USD", "0.137"))
	if e != nil {
		t.Fatal(e)
	}
	pp := p.(*paypalPlugin)
	// 100.00 CNY -> 13.70 USD
	if got := pp.paypalAmount(10000); got != "13.70" {
		t.Fatal("rate amount", got)
	}
	// Empty rate defaults to 1: 12.34 -> 12.34.
	p2, _ := BuildPluginChannel(paypalChannel("0", "USD", ""))
	if got := p2.(*paypalPlugin).paypalAmount(1234); got != "12.34" {
		t.Fatal("default rate", got)
	}
}

func TestPluginPaypalCreateJump(t *testing.T) {
	var tokPath, orderPath string
	var sawBasic, sawBearer bool
	var orderBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/oauth2/token":
			tokPath = r.URL.Path
			if _, _, ok := r.BasicAuth(); ok {
				sawBasic = true
			}
			io.WriteString(w, `{"access_token":"tok-1","token_type":"Bearer"}`)
		case r.URL.Path == "/v2/checkout/orders":
			orderPath = r.URL.Path
			sawBearer = r.Header.Get("Authorization") == "Bearer tok-1"
			b, _ := io.ReadAll(r.Body)
			json.Unmarshal(b, &orderBody)
			io.WriteString(w, `{"id":"ORD1","links":[{"rel":"self","href":"https://x/self"},{"rel":"approve","href":"https://www.paypal.com/approve/ORD1"}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	p := paypalTestPlugin(t, srv.URL, "USD", "1")
	r := CreateRequest{TradeNo: "T1", Subject: "Pro Plan", Method: "h5", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 1999}
	res, e := p.Create(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	if res.MaterialType != "jump" || res.Material != "https://www.paypal.com/approve/ORD1" || res.ProviderTradeNo != "ORD1" {
		t.Fatal("result", res)
	}
	if tokPath == "" || orderPath == "" || !sawBasic || !sawBearer {
		t.Fatal("request shape", tokPath, orderPath, sawBasic, sawBearer)
	}
	units, _ := orderBody["purchase_units"].([]any)
	if len(units) != 1 {
		t.Fatal("units", orderBody)
	}
	u0 := units[0].(map[string]any)
	if u0["custom_id"] != "T1" || u0["invoice_id"] != "T1" {
		t.Fatal("trade binding", u0)
	}
	amt := u0["amount"].(map[string]any)
	if amt["currency_code"] != "USD" || amt["value"] != "19.99" {
		t.Fatal("amount", amt)
	}

	// Non-h5 method is refused by ValidateCreate.
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "native", NotifyURL: r.NotifyURL, Amount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("native must be refused", e)
	}
}

func TestPluginPaypalCreateOutcomes(t *testing.T) {
	var status int
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			io.WriteString(w, `{"access_token":"tok-1"}`)
			return
		}
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := paypalTestPlugin(t, srv.URL, "USD", "1")
	r := CreateRequest{TradeNo: "T1", Subject: "x", Method: "h5", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 100}

	// No approve link -> definitive refusal.
	status, reply = 200, `{"id":"ORD2","links":[{"rel":"self","href":"https://x"}]}`
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("missing approve link", e)
	}
	// Unreadable 2xx -> unknown.
	status, reply = 200, `not json`
	var unknown *UnknownOutcome
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("unreadable", e)
	}
	// 5xx -> unknown.
	status, reply = 502, `bad gateway`
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("5xx", e)
	}
}

func TestPluginPaypalRefund(t *testing.T) {
	var refundPath string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			io.WriteString(w, `{"access_token":"tok-1"}`)
			return
		}
		refundPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)
		io.WriteString(w, `{"id":"REF1","status":"COMPLETED","amount":{"value":"5.00"}}`)
	}))
	defer srv.Close()
	p := paypalTestPlugin(t, srv.URL, "USD", "1")
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "CAP123", RefundNo: "R1", Amount: 500, TotalAmount: 1999})
	if e != nil || rr.State != "SUCCESS" || rr.ProviderRefundNo != "REF1" || rr.Amount != 500 {
		t.Fatal(rr, e)
	}
	if refundPath != "/v2/payments/captures/CAP123/refund" {
		t.Fatal("refund path", refundPath)
	}
	if amt := body["amount"].(map[string]any); amt["value"] != "5.00" {
		t.Fatal("refund amount", amt)
	}
	// VerifyNotification must not be implemented.
	var prov Provider = p
	if _, ok := prov.(NotificationVerifier); ok {
		t.Fatal("paypal must not expose VerifyNotification")
	}
}

// paypalTestPlugin builds a plugin whose API host points at the test server by
// overriding apiHost after a normal Build (Build itself rejects non-PayPal
// hosts via the mode map, so we patch the field directly for the httptest URL).
func paypalTestPlugin(t *testing.T, apiHost, currency, rate string) *paypalPlugin {
	t.Helper()
	p, e := BuildPluginChannel(paypalChannel("0", currency, rate))
	if e != nil {
		t.Fatal(e)
	}
	pp := p.(*paypalPlugin)
	pp.apiHost = apiHost
	return pp
}

// guard against accidental query leakage in the approval link finder.
func TestPluginPaypalApprovalLinkParsing(t *testing.T) {
	out := map[string]any{}
	json.Unmarshal([]byte(`{"links":[{"rel":"approve","href":"https://a/b?x=1"}]}`), &out)
	if got := paypalApprovalLink(out); got != "https://a/b?x=1" {
		t.Fatal(got)
	}
	if got := paypalApprovalLink(map[string]any{}); got != "" {
		t.Fatal("empty", got)
	}
}
