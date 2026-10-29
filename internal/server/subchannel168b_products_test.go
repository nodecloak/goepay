package server

import (
	"context"
	"encoding/json"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type subchannel168BProducts struct {
	*channel168Provider
	creates, webExchanges, miniExchanges, scans, freezes, authReads, captures, queries, refunds atomic.Int64
	completeURL                                                                                 string
	onWebExchange                                                                               func()
	lastCreate104, lastLaunch104                                                                atomic.Int64
}

func (p *subchannel168BProducts) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{Methods: []string{"native", "page", "jsapi", "mini", "applet", "scan", "preauth"}, Query: true, Refund: true, RefundQuery: true, Close: true}
}
func (p *subchannel168BProducts) Create(_ context.Context, r adapter.CreateRequest) (adapter.CreateResult, error) {
	p.creates.Add(1)
	p.lastCreate104.Store(r.Amount)
	if r.Method == "jsapi" || r.Method == "mini" {
		if r.OpenID != "payer:"+p.app {
			return adapter.CreateResult{}, adapter.ErrAuthentication
		}
		return adapter.CreateResult{MaterialType: "jsapi", Material: `{"synthetic":true}`}, nil
	}
	return adapter.CreateResult{MaterialType: "qrcode", Material: "https://synthetic.invalid/q"}, nil
}
func (p *subchannel168BProducts) OAuthAuthorizationURL(state, redirect string) (string, error) {
	return "https://synthetic.invalid/oauth?" + url.Values{"state": {state}, "redirect_uri": {redirect}, "app_id": {p.app}}.Encode(), nil
}
func (p *subchannel168BProducts) ExchangeOAuth(_ context.Context, code string) (adapter.OAuthIdentity, error) {
	p.webExchanges.Add(1)
	if p.onWebExchange != nil {
		p.onWebExchange()
	}
	if code != "code:"+p.app {
		return adapter.OAuthIdentity{}, adapter.ErrAuthentication
	}
	return adapter.OAuthIdentity{Provider: p.ID(), AppID: p.app, SubjectID: "payer:" + p.app}, nil
}
func (p *subchannel168BProducts) ExchangeMiniCode(_ context.Context, code string) (adapter.OAuthIdentity, error) {
	p.miniExchanges.Add(1)
	if code != "mini:"+p.app {
		return adapter.OAuthIdentity{}, adapter.ErrAuthentication
	}
	return adapter.OAuthIdentity{Provider: p.ID(), AppID: p.app, SubjectID: "payer:" + p.app}, nil
}
func (p *subchannel168BProducts) MiniProgram() (adapter.MiniProgramConfig, bool) {
	return adapter.MiniProgramConfig{AppID: p.app, Path: "pages/pay"}, true
}
func (p *subchannel168BProducts) MiniLaunch(r adapter.MiniLaunchRequest) (adapter.CreateResult, error) {
	p.completeURL = r.CompleteURL
	p.lastLaunch104.Store(r.Amount)
	return adapter.CreateResult{MaterialType: "urlscheme", Material: "alipays://platformapi/startapp?" + url.Values{"appId": {p.app}, "complete": {r.CompleteURL}}.Encode()}, nil
}
func (p *subchannel168BProducts) ValidateScan(r adapter.ScanRequest) error {
	if r.AuthCode != "251234567890123456" {
		return adapter.ErrInvalid
	}
	return nil
}
func (p *subchannel168BProducts) Scan(_ context.Context, r adapter.ScanRequest) (adapter.PaymentEvent, error) {
	p.scans.Add(1)
	return adapter.PaymentEvent{TradeNo: r.TradeNo, ProviderTradeNo: "scan:" + p.app, State: "SUCCESS", Currency: "CNY", Amount: r.Amount}, nil
}
func (p *subchannel168BProducts) Freeze(_ context.Context, r adapter.CreateRequest) (adapter.CreateResult, error) {
	p.freezes.Add(1)
	return adapter.CreateResult{MaterialType: "preauth", Material: "synthetic-freeze:" + p.app}, nil
}
func (p *subchannel168BProducts) VerifyAuthorization(_ context.Context, _ http.Header, raw []byte, w adapter.ExpectedPayment) (adapter.AuthorizationEvent, error) {
	if string(raw) != "auth:"+p.app {
		return adapter.AuthorizationEvent{}, adapter.ErrAuthentication
	}
	return adapter.AuthorizationEvent{TradeNo: w.TradeNo, RequestNo: w.TradeNo, AuthNo: "auth:" + p.app, BuyerID: "payer:" + p.app, State: "SUCCESS", OrderStatus: "AUTHORIZED", Amount: w.Amount, RestAmount: w.Amount, RestAmountKnown: true}, nil
}
func (p *subchannel168BProducts) QueryAuthorization(_ context.Context, r adapter.AuthorizationRequest) (adapter.AuthorizationEvent, error) {
	p.authReads.Add(1)
	return adapter.AuthorizationEvent{TradeNo: r.TradeNo, RequestNo: r.RequestNo, AuthNo: "auth:" + p.app, BuyerID: "payer:" + p.app, State: "SUCCESS", OrderStatus: "AUTHORIZED", Amount: r.Amount, RestAmount: r.Amount, RestAmountKnown: true}, nil
}
func (p *subchannel168BProducts) CaptureAuthorization(_ context.Context, r adapter.AuthorizationCaptureRequest) (adapter.PaymentEvent, error) {
	p.captures.Add(1)
	return adapter.PaymentEvent{TradeNo: r.TradeNo, ProviderTradeNo: "capture:" + p.app, State: "SUCCESS", Currency: "CNY", Amount: r.Amount}, nil
}
func (p *subchannel168BProducts) UnfreezeAuthorization(context.Context, adapter.AuthorizationRequest) (adapter.AuthorizationEvent, error) {
	return adapter.AuthorizationEvent{}, adapter.ErrUnsupported
}
func (p *subchannel168BProducts) CancelAuthorization(context.Context, adapter.AuthorizationRequest) (adapter.AuthorizationEvent, error) {
	return adapter.AuthorizationEvent{}, adapter.ErrUnsupported
}
func (p *subchannel168BProducts) Query(_ context.Context, r adapter.QueryRequest) (adapter.QueryResult, error) {
	p.queries.Add(1)
	return adapter.QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: "query:" + p.app, State: "SUCCESS", Currency: "CNY", Amount: 100}, nil
}
func (p *subchannel168BProducts) Refund(_ context.Context, r adapter.RefundRequest) (adapter.RefundResult, error) {
	p.refunds.Add(1)
	return adapter.RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: "refund:" + p.app, State: "SUCCESS", Amount: r.Amount}, nil
}
func (p *subchannel168BProducts) QueryRefund(_ context.Context, r adapter.RefundRequest) (adapter.RefundResult, error) {
	return adapter.RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: "refund:" + p.app, State: "SUCCESS", Amount: r.Amount}, nil
}
func (p *subchannel168BProducts) VerifyNotification(_ context.Context, _ http.Header, raw []byte, w adapter.ExpectedPayment) (adapter.PaymentEvent, error) {
	if string(raw) != "notify:"+p.app {
		return adapter.PaymentEvent{}, adapter.ErrAuthentication
	}
	return adapter.PaymentEvent{TradeNo: w.TradeNo, EventID: "notification:" + p.app, ProviderTradeNo: "notification:" + p.app, State: "SUCCESS", Currency: "CNY", Amount: w.Amount}, nil
}

type subchannel168BProductFactory struct {
	mu        sync.Mutex
	providers map[string]*subchannel168BProducts
	web, mini bool
}

func (f *subchannel168BProductFactory) Schema() adapter.SubchannelSchema {
	return (&subchannel168BFactory{}).Schema()
}
func (f *subchannel168BProductFactory) Build(v map[string]string) (adapter.SubchannelExecution, error) {
	if len(v) != 2 || v["app"] == "" || v["secret"] != "synthetic-only-secret" {
		return adapter.SubchannelExecution{}, adapter.ErrInvalid
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.providers[v["app"]]
	if p == nil {
		p = &subchannel168BProducts{channel168Provider: &channel168Provider{app: v["app"]}}
		f.providers[v["app"]] = p
	}
	out := adapter.SubchannelExecution{Payment: p, SchemaFingerprint: f.Schema().Fingerprint}
	if f.web {
		out.Web = p
	}
	if f.mini {
		out.Mini = p
	}
	return out, nil
}
func (f *subchannel168BProductFactory) Snapshot(v map[string]string) ([]byte, error) {
	if _, e := f.Build(v); e != nil {
		return nil, e
	}
	return json.Marshal(v)
}
func (f *subchannel168BProductFactory) Restore(raw []byte) (adapter.SubchannelExecution, error) {
	var v map[string]string
	if json.Unmarshal(raw, &v) != nil {
		return adapter.SubchannelExecution{}, adapter.ErrInvalid
	}
	return f.Build(v)
}
func subchannel168BProductLocal(t *testing.T) (channel168Fixture, *subchannel168BProductFactory, map[string]any) {
	f, _ := subchannel168BLocal(t)
	factory := &subchannel168BProductFactory{providers: map[string]*subchannel168BProducts{}, web: true, mini: true}
	f.s.ProviderFlow.SubchannelFactories = map[int64]adapter.SubchannelFactory{7: factory}
	return f, factory, child168BCreate(t, f, "products-v1", true)
}
func subchannel168BProductPay(t *testing.T, f channel168Fixture, no, method string, extra compat.Params) map[string]any {
	t.Helper()
	p := compat.Params{"pid": "1", "out_trade_no": no, "type": "alipay", "name": "Synthetic child product", "money": "1.00", "channel_id": "7", "method": method, "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
	for k, v := range extra {
		p[k] = v
	}
	p["sign"] = compat.SignMD5(p, "test-secret")
	q := url.Values{}
	for k, v := range p {
		q.Set(k, v)
	}
	r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(q.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var out map[string]any
	if json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	return out
}
func subchannel168BRotate(t *testing.T, f channel168Fixture, row map[string]any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"expected_fingerprint": row["fingerprint"], "values": map[string]any{"app": map[string]string{"operation": "replace", "value": "products-v2"}}})
	w := f.childHTTP("PATCH", "/"+row["id"].(string), string(b))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestSubchannel168BActualHTTPOAuthUsesOriginalChildAtCheckoutExchangeAndSend(t *testing.T) {
	f, factory, row := subchannel168BProductLocal(t)
	out := subchannel168BProductPay(t, f, "oauth168b", "jsapi", compat.Params{"oauth": "1"})
	if out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(context.Background(), 1, "oauth168b")
	if e != nil {
		t.Fatal(e)
	}
	subchannel168BRotate(t, f, row)
	checkout := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(checkout, httptest.NewRequest("GET", f.s.checkoutURL(o.TradeNo, "checkout"), nil))
	if checkout.Code != 302 {
		t.Fatal(checkout.Code, checkout.Body.String())
	}
	u, e := url.Parse(checkout.Header().Get("Location"))
	if e != nil || u.Query().Get("app_id") != "products-v1" {
		t.Fatal("checkout borrowed new or parent OAuth", u, e)
	}
	callback := u.Query().Get("redirect_uri") + "?" + url.Values{"state": {u.Query().Get("state")}, "auth_code": {"code:products-v1"}, "app_id": {"products-v1"}}.Encode()
	r := httptest.NewRequest("GET", callback, nil)
	for _, c := range checkout.Result().Cookies() {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 302 {
		t.Fatal(w.Code, w.Body.String())
	}
	p := factory.providers["products-v1"]
	if p.webExchanges.Load() != 1 || p.creates.Load() != 1 || factory.providers["products-v2"].creates.Load() != 0 {
		t.Fatal("OAuth original account lost")
	}
	again := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(again, r.Clone(context.Background()))
	if p.creates.Load() != 1 || p.webExchanges.Load() != 1 {
		t.Fatal("OAuth state replayed")
	}
}
func TestSubchannel168BActualHTTPMiniUsesOriginalChildForLaunchExchangeAndSend(t *testing.T) {
	f, factory, row := subchannel168BProductLocal(t)
	out := subchannel168BProductPay(t, f, "mini168b", "applet", nil)
	if out["code"] != float64(1) {
		t.Fatal(out)
	}
	p := factory.providers["products-v1"]
	o, e := f.s.Store.Order(context.Background(), 1, "mini168b")
	if e != nil {
		t.Fatal(e)
	}
	checkout := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(checkout, httptest.NewRequest("GET", f.s.checkoutURL(o.TradeNo, "checkout"), nil))
	if checkout.Code != 200 {
		t.Fatal(checkout.Code, checkout.Body.String())
	}
	complete := p.completeURL
	if !strings.Contains(complete, "/oauth/mini/complete/alipay/7/") {
		t.Fatal("no protected mini complete", complete)
	}
	subchannel168BRotate(t, f, row)
	r := httptest.NewRequest("POST", complete, strings.NewReader(`{"code":"mini:products-v1"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if p.miniExchanges.Load() != 1 || p.creates.Load() != 1 || factory.providers["products-v2"].creates.Load() != 0 {
		t.Fatal("mini original account lost")
	}
	replay := httptest.NewRecorder()
	r = httptest.NewRequest("POST", complete, strings.NewReader(`{"code":"mini:products-v1"}`))
	r.Header.Set("Content-Type", "application/json")
	f.s.Handler().ServeHTTP(replay, r)
	if replay.Code == 200 || p.creates.Load() != 1 || p.miniExchanges.Load() != 1 {
		t.Fatal("mini state replayed")
	}
}
func TestSubchannel168BActualHTTPScanAndPreauthorizationNeverBorrowParentCapabilities(t *testing.T) {
	f, factory, row := subchannel168BProductLocal(t)
	out := subchannel168BProductPay(t, f, "scan168b", "scan", compat.Params{"auth_code": "251234567890123456"})
	if out["code"] != float64(1) {
		t.Fatal(out)
	}
	if factory.providers["products-v1"].scans.Load() != 1 {
		t.Fatal("scan not sent by actual child")
	}
	out = subchannel168BProductPay(t, f, "preauth168b", "preauth", nil)
	if out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(context.Background(), 1, "preauth168b")
	if e != nil {
		t.Fatal(e)
	}
	subchannel168BRotate(t, f, row)
	// The trusted authorization callback must capture with the original child
	// even though the current head now names a different account version.
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/pay/preauthnotify/"+o.TradeNo+"/", strings.NewReader("auth:products-v1")))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	p := factory.providers["products-v1"]
	if p.freezes.Load() != 1 || p.captures.Load() != 1 || factory.providers["products-v2"].captures.Load() != 0 {
		t.Fatal("preauth original account lost")
	}
	if f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 0 {
		t.Fatal("parent sent child operation")
	}
}
func TestSubchannel168BActualHTTPCallbackQueryRefundRemainOnOriginalChild(t *testing.T) {
	f, factory, row := subchannel168BProductLocal(t)
	for _, no := range []string{"callback168b", "query168b"} {
		if out := subchannel168BProductPay(t, f, no, "native", nil); out["code"] != float64(1) {
			t.Fatal(out)
		}
	}
	subchannel168BRotate(t, f, row)
	a, _ := f.s.Store.Order(context.Background(), 1, "callback168b")
	b, _ := f.s.Store.Order(context.Background(), 1, "query168b")
	wrong := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(wrong, httptest.NewRequest("POST", fmt.Sprintf("/callbacks/providers/7/%s", a.TradeNo), strings.NewReader("notify:products-v2")))
	if wrong.Code == 200 {
		t.Fatal("new child accepted old payment notification")
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, httptest.NewRequest("POST", fmt.Sprintf("/callbacks/providers/7/%s", a.TradeNo), strings.NewReader("notify:products-v1")))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, e := f.s.ProviderFlow.Query(context.Background(), 1, b.TradeNo); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.ProviderFlow.Query(context.Background(), 2, b.TradeNo); e == nil {
		t.Fatal("other merchant queried owned original")
	}
	if _, e := f.s.ProviderFlow.Refund(context.Background(), 1, b.TradeNo, "refund168b", 100); e != nil {
		t.Fatal("original refund", e)
	}
	p := factory.providers["products-v1"]
	if p.queries.Load() != 1 || p.refunds.Load() != 1 || factory.providers["products-v2"].refunds.Load() != 0 {
		t.Fatal("query/refund borrowed new account")
	}
}
func TestSubchannel168BChildMissingOAuthDoesNotBorrowParentExchange(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	parent := &subchannel168BProducts{channel168Provider: &channel168Provider{app: "parent-oauth"}}
	f.s.OAuthProviders = map[int64]adapter.WebOAuthProvider{7: parent}
	f.s.MiniOAuthProviders = map[int64]adapter.MiniOAuthProvider{7: parent}
	factory.web = false
	factory.mini = false
	for i, method := range []string{"jsapi", "applet"} {
		extra := compat.Params{}
		if method == "jsapi" {
			extra["oauth"] = "1"
		}
		out := subchannel168BProductPay(t, f, fmt.Sprintf("no-oauth-%d", i), method, extra)
		if out["code"] == float64(1) {
			t.Fatal("child borrowed parent exchange", out)
		}
	}
	if parent.webExchanges.Load() != 0 || parent.miniExchanges.Load() != 0 {
		t.Fatal("parent exchange called")
	}
}
func TestSubchannel168BActualChildOAuthStopDuringExchangeNeverCreatesPayment(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	out := subchannel168BProductPay(t, f, "oauth168b-stop", "jsapi", compat.Params{"oauth": "1"})
	if out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(context.Background(), 1, "oauth168b-stop")
	if e != nil {
		t.Fatal(e)
	}
	p := factory.providers["products-v1"]
	p.onWebExchange = func() { f.s.ProviderFlow.AllowLive = false }
	checkout := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(checkout, httptest.NewRequest("GET", f.s.checkoutURL(o.TradeNo, "checkout"), nil))
	if checkout.Code != 302 {
		t.Fatal(checkout.Code)
	}
	u, _ := url.Parse(checkout.Header().Get("Location"))
	callback := u.Query().Get("redirect_uri") + "?" + url.Values{"state": {u.Query().Get("state")}, "auth_code": {"code:products-v1"}, "app_id": {"products-v1"}}.Encode()
	r := httptest.NewRequest("GET", callback, nil)
	for _, c := range checkout.Result().Cookies() {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 503 || p.creates.Load() != 0 {
		t.Fatal("child execution snapshot bypassed changed global write gate", w.Code, p.creates.Load())
	}
}
