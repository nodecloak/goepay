package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
)

type checkoutWechatProvider struct{ *channel168CConfirmedProvider }

func (*checkoutWechatProvider) ID() string { return "wxpay" }

type checkoutHTMLProvider struct {
	*channel168Provider
	reject      bool
	unsupported bool
	lastReturn  string
}

func TestCheckoutGuardedNativeAlipayPageAndWapForms(t *testing.T) {
	f := channel168Local(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	private := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}))
	providers, err := channelconfig.Build(channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{{ID: 7, Plugin: "alipay", AppTypes: []string{"1", "2"}, Config: map[string]string{"appid": "synthetic-app", "appsecret": private, "appkey": publicPEM}}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	f.s.ProviderFlow.Providers[7] = providers[7]
	for _, method := range []string{"page", "wap"} {
		t.Run(method, func(t *testing.T) {
			p := compat.Params{"pid": "1", "out_trade_no": "checkout-form-" + method, "type": "alipay", "name": "synthetic item", "money": "1.00", "channel_id": "7", "method": method, "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
			p["sign"] = compat.SignMD5(p, "test-secret")
			form := url.Values{}
			for k, v := range p {
				form.Set(k, v)
			}
			r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(w, r)
			var out map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out["code"] != float64(1) {
				t.Fatalf("synthetic form creation: %v", out)
			}
			o, err := f.s.Store.Order(context.Background(), 1, p["out_trade_no"])
			if err != nil {
				t.Fatal(err)
			}
			page := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(page, httptest.NewRequest("GET", f.s.checkoutURL(o.TradeNo, "checkout"), nil))
			if page.Code != 200 || !strings.Contains(page.Body.String(), `accept-charset="UTF-8"`) || !strings.Contains(page.Body.String(), "charset=utf-8") {
				t.Fatalf("guarded %s checkout: %d", method, page.Code)
			}
		})
	}
}

func (p *checkoutHTMLProvider) Create(context.Context, adapter.CreateRequest) (adapter.CreateResult, error) {
	p.sends.Add(1)
	return adapter.CreateResult{MaterialType: "html", Material: `<form>legacy</form>`}, nil
}
func (p *checkoutHTMLProvider) PrepareCheckoutHTML(material, trade string) (string, error) {
	if p.unsupported {
		return "", adapter.ErrUnsupported
	}
	if p.reject || material != `<form>legacy</form>` || trade == "" {
		return "", errors.New("invalid form")
	}
	return `<form>verified-upgrade</form>`, nil
}

func (p *checkoutHTMLProvider) RefreshCheckoutHTML(material, trade, returnURL string) (string, error) {
	p.lastReturn = returnURL
	return p.PrepareCheckoutHTML(material, trade)
}

func TestCheckoutUsesOriginalAlipayProviderToValidateStoredHTML(t *testing.T) {
	f := channel168Local(t)
	p := &checkoutHTMLProvider{channel168Provider: &channel168Provider{app: "checkout-html"}}
	f.s.ProviderFlow.Providers[7] = p
	if out := f.pay(t, "checkout-form", "1.00", 7); out["code"] != float64(1) {
		t.Fatalf("synthetic create: %v", out)
	}
	o, err := f.s.Store.Order(context.Background(), 1, "checkout-form")
	if err != nil {
		t.Fatal(err)
	}
	get := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, httptest.NewRequest("GET", f.s.checkoutURL(o.TradeNo, "checkout"), nil))
		return w
	}
	page := get()
	if page.Code != 200 || !strings.Contains(page.Body.String(), "verified-upgrade") || strings.Contains(page.Body.String(), "<form>legacy</form>") {
		t.Fatalf("stored HTML was not validated and upgraded: %d", page.Code)
	}
	if p.lastReturn == "" || !f.s.checkoutAuthorized(httptest.NewRequest("GET", p.lastReturn, nil), o.TradeNo, "return") {
		t.Fatal("checkout did not refresh the original return token")
	}
	p.reject = true
	page = get()
	if page.Code != 503 || strings.Contains(page.Body.String(), "<form>legacy</form>") {
		t.Fatalf("failed validation did not reject form: %d", page.Code)
	}
	p.reject, p.unsupported = false, true
	page = get()
	if page.Code != 200 || !strings.Contains(page.Body.String(), "<form>legacy</form>") {
		t.Fatalf("unrelated provider material was not preserved: %d", page.Code)
	}
	a, err := f.s.Store.ProviderAttempt(context.Background(), o.TradeNo)
	if err != nil || a.Material != `<form>legacy</form>` || p.sends.Load() != 1 {
		t.Fatal("stored material or payment sends changed", err)
	}
}

func TestRepeatedV2CreateReturnsPreparedStoredHTMLWithoutResubmitting(t *testing.T) {
	f, merchantKey := paymentDefend206WireFixture(t, true)
	p := &checkoutHTMLProvider{channel168Provider: &channel168Provider{app: "checkout-html-replay"}}
	f.s.ProviderFlow.Providers[7] = p
	for attempt := 0; attempt < 2; attempt++ {
		w := paymentDefend206Wire(t, f, merchantKey, "/api/pay/create", "checkout-html-replay")
		var got map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got["code"] != float64(0) || got["pay_type"] != "html" || got["pay_info"] != `<form>verified-upgrade</form>` {
			t.Fatalf("v2 replay %d did not return prepared original HTML: %v", attempt, got)
		}
	}
	o, err := f.s.Store.Order(context.Background(), 1, "checkout-html-replay")
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.s.Store.ProviderAttempt(context.Background(), o.TradeNo)
	if err != nil || a.Material != `<form>legacy</form>` || p.sends.Load() != 1 {
		t.Fatal("replay altered stored material or resent payment", err)
	}
	if p.lastReturn == "" || !f.s.checkoutAuthorized(httptest.NewRequest("GET", p.lastReturn, nil), o.TradeNo, "return") {
		t.Fatal("v2 replay did not refresh the original return token")
	}
	p.reject = true
	w := paymentDefend206Wire(t, f, merchantKey, "/api/pay/create", "checkout-html-replay")
	var got map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &got); err != nil || got["code"] != float64(-1) || got["pay_info"] != nil || p.sends.Load() != 1 {
		t.Fatalf("invalid stored form was not rejected safely: %v", got)
	}
}

// The only upstream payment is an in-process provider double. Status polling
// must not query that provider or create another payment attempt.
func TestCheckoutQRReturnsOnlyAfterLocallyConfirmedPayment(t *testing.T) {
	for _, payType := range []string{"alipay", "wxpay"} {
		t.Run(payType, func(t *testing.T) { testCheckoutQRReturnsOnlyAfterLocallyConfirmedPayment(t, payType) })
	}
}

func testCheckoutQRReturnsOnlyAfterLocallyConfirmedPayment(t *testing.T, payType string) {
	f := channel168CConfirmedLocal(t)
	p := f.s.ProviderFlow.Providers[7].(*channel168CConfirmedProvider)
	if payType == "wxpay" {
		f.s.ProviderFlow.Providers[7] = &checkoutWechatProvider{p}
	}
	params := compat.Params{"pid": "1", "out_trade_no": "checkout-qr-redirect", "type": payType, "name": "synthetic item", "money": "1.00", "channel_id": "7", "method": "native", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
	params["sign"] = compat.SignMD5(params, "test-secret")
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["code"] != float64(1) {
		t.Fatalf("synthetic create: %v", result)
	}
	o, err := f.s.Store.Order(context.Background(), 1, "checkout-qr-redirect")
	if err != nil {
		t.Fatal(err)
	}
	checkout := f.s.checkoutURL(o.TradeNo, "checkout")
	u, err := url.Parse(checkout)
	if err != nil {
		t.Fatal(err)
	}
	u.Path += "/status"
	statusURL := u.String()
	get := func(target string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		return w
	}
	page := get(checkout)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "/status") || !strings.Contains(page.Body.String(), "fetch(") || !strings.Contains(page.Header().Get("Content-Security-Policy"), "connect-src 'self'") {
		t.Fatalf("QR checkout lacks same-origin status polling: %d", page.Code)
	}
	if !strings.Contains(page.Body.String(), "response.status===403") || !strings.Contains(page.Body.String(), "页面验证已过期，请回到商户订单页重新打开；已付款请勿重复支付") {
		t.Fatal("expired checkout lacks a terminal status and payment-safe guidance")
	}
	if bad := get("/checkout/" + o.TradeNo + "/status?expires=1&token=bad"); bad.Code != 403 {
		t.Fatalf("forged status token: %d", bad.Code)
	}
	status := get(statusURL)
	var state struct {
		Paid bool `json:"paid"`
	}
	if status.Code != 200 || json.Unmarshal(status.Body.Bytes(), &state) != nil || state.Paid || status.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("pending status: %d %s", status.Code, status.Body.String())
	}
	// Only the explicit synthetic provider query below confirms the payment.
	if _, err = f.s.ProviderFlow.Query(context.Background(), 1, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	status = get(statusURL)
	if status.Code != 200 || json.Unmarshal(status.Body.Bytes(), &state) != nil || !state.Paid {
		t.Fatalf("paid status: %d %s", status.Code, status.Body.String())
	}
	page = get(checkout)
	if page.Code != 302 || !strings.Contains(page.Header().Get("Location"), "/return?") {
		t.Fatalf("paid QR revisit did not redirect to signed return: %d", page.Code)
	}
	returned := get(page.Header().Get("Location"))
	if returned.Code != 302 || !strings.HasPrefix(returned.Header().Get("Location"), "https://merchant.example.invalid/return?") {
		t.Fatalf("confirmed merchant return: %d", returned.Code)
	}
	if p.sends.Load() != 1 {
		t.Fatalf("unexpected payment sends: %d", p.sends.Load())
	}
}
