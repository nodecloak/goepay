package payment

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func stripeChannel(mode, currency, rate string) PluginChannel {
	return PluginChannel{ID: 12, Plugin: "stripe", PayType: "alipay", Config: map[string]string{
		"appid": "sk_live_abc", "appkey": "whsec_test", "appswitch": mode, "currency_code": currency, "currency_rate": rate,
	}}
}

func TestPluginStripeRegistryAndBuild(t *testing.T) {
	spec, ok := LookupPlugin("stripe")
	if !ok || spec.Title != "Stripe" || !spec.Serves("wxpay") || !spec.Serves("paypal") || spec.Serves("qqpay") {
		t.Fatal("spec", spec, ok)
	}
	if _, e := BuildPluginChannel(stripeChannel("0", "CNY", "1")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{}
	unserved := stripeChannel("0", "CNY", "1")
	unserved.PayType = "qqpay"
	missing := stripeChannel("0", "CNY", "1")
	delete(missing.Config, "appkey")
	badCur := stripeChannel("0", "ZZZ", "1")
	bad = append(bad, unserved, missing, badCur)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad", i)
		}
	}
}

func stripeTestPlugin(t *testing.T, host, payType, currency, rate string) *stripePlugin {
	t.Helper()
	c := stripeChannel("0", currency, rate)
	c.PayType = payType
	p, e := BuildPluginChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	return p.(*stripePlugin)
}

func TestPluginStripeCreateSession(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/checkout/sessions" || r.Method != http.MethodPost {
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sk_live_abc" {
			w.WriteHeader(401)
			return
		}
		b, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(b))
		io.WriteString(w, `{"id":"cs_1","url":"https://checkout.stripe.com/c/cs_1"}`)
	}))
	defer srv.Close()
	// alipay pay type -> payment_method_types[0]=alipay
	p := stripeTestPlugin(t, srv.URL, "alipay", "USD", "0.137")
	stripePatchHost(p, srv.URL)
	r := CreateRequest{TradeNo: "T1", Subject: "Plan", Method: "h5", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 10000}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "jump" || res.Material != "https://checkout.stripe.com/c/cs_1" || res.ProviderTradeNo != "cs_1" {
		t.Fatal(res, e)
	}
	if form.Get("client_reference_id") != "T1" || form.Get("mode") != "payment" || form.Get("payment_method_types[0]") != "alipay" {
		t.Fatal("request", form)
	}
	// 100.00 CNY * 0.137 * 100 = 1370 minor units.
	if form.Get("line_items[0][price_data][unit_amount]") != "1370" || form.Get("line_items[0][price_data][currency]") != "usd" {
		t.Fatal("amount", form)
	}

	// wxpay adds the wechat_pay client option.
	p2 := stripeTestPlugin(t, srv.URL, "wxpay", "CNY", "1")
	stripePatchHost(p2, srv.URL)
	if _, e := p2.Create(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	if form.Get("payment_method_types[0]") != "wechat_pay" || form.Get("payment_method_options[wechat_pay][client]") != "web" {
		t.Fatal("wxpay opts", form)
	}
}

func TestPluginStripeCreateOutcomes(t *testing.T) {
	var status int
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := stripeTestPlugin(t, srv.URL, "alipay", "CNY", "1")
	stripePatchHost(p, srv.URL)
	r := CreateRequest{TradeNo: "T1", Subject: "x", Method: "h5", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 100}
	status, reply = 400, `{"error":{"code":"amount_too_small","message":"too small"}}`
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business error", e)
	}
	status, reply = 200, `not json`
	var unknown *UnknownOutcome
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("unreadable", e)
	}
	status, reply = 503, `down`
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("5xx", e)
	}
}

func TestPluginStripeVerifyNotification(t *testing.T) {
	p := stripeTestPlugin(t, "https://api.stripe.com", "alipay", "CNY", "1")
	// Independently computed with node (p03vec.js): HMAC-SHA256 of "ts.body"
	// keyed by whsec_test.
	ts := "1730000000"
	body := `{"id":"evt_1","type":"checkout.session.completed","data":{"object":{"client_reference_id":"T202610290001","payment_status":"paid","payment_intent":"pi_123","amount_total":100}}}`
	sig := "bdefd97f95b5bbfd63f9f1548cafb789f92a42bc5f072fdbf2e6cf9b7c9f851d"
	hdr := http.Header{}
	hdr.Set("Stripe-Signature", "t="+ts+",v1="+sig)
	exp := ExpectedPayment{TradeNo: "T202610290001", Amount: 100}
	ev, e := p.VerifyNotification(context.Background(), hdr, []byte(body), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "pi_123" || ev.Amount != 100 || ev.Currency != "CNY" {
		t.Fatal(ev, e)
	}
	// Bad signature.
	bad := http.Header{}
	bad.Set("Stripe-Signature", "t="+ts+",v1=00")
	if _, e := p.VerifyNotification(context.Background(), bad, []byte(body), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// Correct signature but wrong order / amount.
	if _, e := p.VerifyNotification(context.Background(), hdr, []byte(body), ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong order", e)
	}
	if _, e := p.VerifyNotification(context.Background(), hdr, []byte(body), ExpectedPayment{TradeNo: "T202610290001", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong amount", e)
	}
	// Non-success payment_status on checkout.session.completed.
	body2 := `{"id":"evt_2","type":"checkout.session.completed","data":{"object":{"client_reference_id":"T202610290001","payment_status":"unpaid","amount_total":100}}}`
	sig2 := stripeTestHMAC(ts, body2)
	hdr2 := http.Header{}
	hdr2.Set("Stripe-Signature", "t="+ts+",v1="+sig2)
	if _, e := p.VerifyNotification(context.Background(), hdr2, []byte(body2), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("unpaid", e)
	}
	// Ack bodies.
	if s, _, b := p.NotificationAck(nil); s != 200 || b != "success" {
		t.Fatal(s, b)
	}
	if got := p.NotificationMethods(); len(got) != 1 || got[0] != http.MethodPost {
		t.Fatal(got)
	}
}

func TestPluginStripeRefund(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(b))
		io.WriteString(w, `{"id":"re_1","status":"succeeded","payment_intent":"pi_123","amount":500}`)
	}))
	defer srv.Close()
	p := stripeTestPlugin(t, srv.URL, "alipay", "CNY", "1")
	stripePatchHost(p, srv.URL)
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "pi_123", RefundNo: "R1", Amount: 500, TotalAmount: 1000})
	if e != nil || rr.State != "SUCCESS" || rr.ProviderRefundNo != "pi_123" || rr.Amount != 500 {
		t.Fatal(rr, e)
	}
	if form.Get("payment_intent") != "pi_123" || form.Get("amount") != "500" {
		t.Fatal("refund request", form)
	}
}

// stripeTestHMAC recomputes the signature for a locally-built body so the
// "non-success status" case stays independent of production verify code.
func stripeTestHMAC(ts, body string) string {
	return hmacSHA256Hex("whsec_test", ts+"."+body)
}

// stripePatchHost points the plugin's API calls at a test server.
func stripePatchHost(p *stripePlugin, host string) {
	p.host = host
}
