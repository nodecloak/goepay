package server

import (
	"context"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"goepay/internal/riskgeo"
)

type risk177HTTPURLProvider struct {
	*risk177Provider
	method string
}

func (p *risk177HTTPURLProvider) ID() string {
	if p.method == "h5" {
		return "wxpay"
	}
	return "alipay"
}
func (p *risk177HTTPURLProvider) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{Methods: []string{"native", "wap", "h5"}, Query: true}
}
func (p *risk177HTTPURLProvider) Create(ctx context.Context, in adapter.CreateRequest) (adapter.CreateResult, error) {
	out, e := p.risk177Provider.Create(ctx, in)
	out.Material = "http://pay.example.invalid/original-http-material"
	if p.method != "native" {
		out.MaterialType = "jump"
	}
	return out, e
}

func TestRisk177EveryNativeAndWAPHTTPMaterialUsesProtectedIntermediary(t *testing.T) {
	for _, method := range []string{"native", "wap", "h5"} {
		t.Run(method, func(t *testing.T) {
			f, p, _ := risk177Flow(t)
			provider := &risk177HTTPURLProvider{p, method}
			f.s.ProviderFlow.Providers[7] = provider
			f.s.Store.CoreRiskGeo = risk177GeoFunc(risk177ScanResolver)
			v := risk177Data(t, f.request("GET", "", f.csrf))
			risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) { s["check_pay_regoin"] = "1" }), f.csrf))
			ctx := payment.WithCoreRiskRequest(context.Background(), payment.CoreRiskRequest{PeerIP: "192.0.2.177", BuyerIP: "192.0.2.177", Source: "direct_http"})
			order, e := f.s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "risk177-http-url-" + method, Name: "synthetic HTTP URL", Type: provider.ID(), Amount: 100})
			if e != nil {
				t.Fatal(e)
			}
			if e = f.s.Store.SetOrderClientIP(ctx, order.ID, "192.0.2.177"); e != nil {
				t.Fatal(e)
			}
			if _, e = f.s.ProviderFlow.Submit(ctx, order.TradeNo, 7, method, "192.0.2.177", ""); e != nil {
				t.Fatal(e)
			}
			page := risk177ScanGET(f, f.s.checkoutURL(order.TradeNo, "checkout"), "192.0.2.177")
			if page.Code != 200 || strings.Contains(page.Body.String(), "http://pay.example.invalid/original-http-material") || !strings.Contains(page.Body.String(), "scan-region") {
				t.Fatal("outgoing HTTP material bypassed intermediary", method, page.Code)
			}
			same := risk177ScanGET(f, f.s.checkoutURL(order.TradeNo, "scan-region"), "192.0.2.177")
			if same.Code != 302 || same.Header().Get("Location") != "http://pay.example.invalid/original-http-material" {
				t.Fatal("original HTTP destination not preserved", same.Code)
			}
			denied := risk177ScanGET(f, f.s.checkoutURL(order.TradeNo, "scan-region-continue"), "192.0.2.178")
			if denied.Code != 403 {
				t.Fatal("mode1 accepted direct continue", denied.Code)
			}
		})
	}
}

func risk177ScanResolver(ip netip.Addr) (riskgeo.Region, bool, error) {
	city := "杭州市"
	if ip.String() == "192.0.2.178" {
		city = "宁波市"
	}
	return riskgeo.Region{Country: "中国", Province: "浙江省", City: city}, true, nil
}

func TestRisk177ScanCustomerBaselineIsDeclaredAPIOrDirectSubmitNeverMerchantPeer(t *testing.T) {
	for _, entry := range []string{"/mapi.php", "/submit.php"} {
		t.Run(entry, func(t *testing.T) {
			f, _, _ := risk177Flow(t)
			f.s.Store.CoreRiskGeo = risk177GeoFunc(risk177ScanResolver)
			v := risk177Data(t, f.request("GET", "", f.csrf))
			risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) { s["check_pay_regoin"] = "1" }), f.csrf))
			declared, peer := "192.0.2.177", "192.0.2.178"
			expectedSource := "signed_api_clientip"
			if entry == "/submit.php" {
				declared, peer = peer, declared
				expectedSource = "direct_http"
			}
			p := compat.Params{"pid": "1", "out_trade_no": "scan177-baseline", "type": "alipay", "name": "synthetic customer baseline", "money": "1.00", "channel_id": "7", "method": "native", "clientip": declared, "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
			p["sign"] = compat.SignMD5(p, "test-secret")
			form := url.Values{}
			for k, v := range p {
				form.Set(k, v)
			}
			req := httptest.NewRequest("POST", entry, strings.NewReader(form.Encode()))
			req.RemoteAddr = peer + ":49177"
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(w, req)
			var trade, source string
			if e := f.s.Pool.QueryRow(context.Background(), `SELECT o.trade_no,k.facts->>'ScanBaselineSource' FROM orders o JOIN core_risk_origins k ON k.order_id=o.id WHERE o.out_trade_no='scan177-baseline'`).Scan(&trade, &source); e != nil {
				t.Fatal("original customer birth missing", w.Code, e)
			}
			if source != expectedSource {
				t.Fatal("customer provenance lost", source)
			}
			target := f.s.checkoutURL(trade, "scan-region")
			if w := risk177ScanGET(f, target, "192.0.2.177"); w.Code != 302 {
				t.Fatal("baseline incorrectly used merchant peer/API value on direct submit", w.Code)
			}
			if w := risk177ScanGET(f, target, "192.0.2.178"); w.Code != 403 {
				t.Fatal("different customer city allowed", w.Code)
			}
			page := risk177ScanGET(f, f.s.checkoutURL(trade, "checkout"), "192.0.2.177")
			if page.Code != 200 || strings.Contains(page.Body.String(), "https://local.example.invalid/channel168") || !strings.Contains(page.Body.String(), "scan-region") {
				t.Fatal("checkout exposes unguarded HTTP original material", page.Code)
			}
			attempt, e := f.s.Store.ProviderAttempt(context.Background(), trade)
			if e != nil || attempt.Material != "https://local.example.invalid/channel168" {
				t.Fatal("private original provider material was changed")
			}
		})
	}
}

func TestRisk177ScanTrustsOnlyExplicitProxyAndMissingRegionIsError(t *testing.T) {
	f, _, _ := risk177Flow(t)
	f.s.Store.CoreRiskGeo = risk177GeoFunc(risk177ScanResolver)
	v := risk177Data(t, f.request("GET", "", f.csrf))
	risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) { s["check_pay_regoin"] = "1" }), f.csrf))
	p := f.pay(t, "scan177-proxy", "192.0.2.177")
	target := f.s.checkoutURL(p["trade_no"].(string), "scan-region")
	for _, trusted := range []bool{false, true} {
		prefix := ""
		if trusted {
			prefix = "192.0.2.178/32"
		}
		wrap, e := NewTrustedProxy(prefix)
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest("GET", target, nil)
		r.RemoteAddr = "192.0.2.178:49177"
		r.Header.Set("X-Real-IP", "192.0.2.177")
		r.Header.Set("X-Forwarded-For", "192.0.2.177")
		w := httptest.NewRecorder()
		wrap(f.s.Handler()).ServeHTTP(w, r)
		want := 403
		if trusted {
			want = 302
		}
		if w.Code != want {
			t.Fatal("scan untrusted header replaced peer", trusted, w.Code)
		}
	}
	f.s.Store.CoreRiskGeo = risk177GeoFunc(func(netip.Addr) (riskgeo.Region, bool, error) { return riskgeo.Region{}, false, nil })
	if w := risk177ScanGET(f, target, "192.0.2.177"); w.Code != 503 {
		t.Fatal("missing province/city silently passed", w.Code)
	}
}
func risk177ScanGET(f risk177Fixture, target, ip string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", target, nil)
	r.RemoteAddr = ip + ":49177"
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func TestRisk177ScanRegionFrozenPolicyAndExplicitContinue(t *testing.T) {
	for _, mode := range []string{"1", "2"} {
		t.Run(mode, func(t *testing.T) {
			f, _, creates := risk177Flow(t)
			f.s.Store.CoreRiskGeo = risk177GeoFunc(risk177ScanResolver)
			v := risk177Data(t, f.request("GET", "", f.csrf))
			risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) { s["check_pay_regoin"] = mode }), f.csrf))
			p := f.pay(t, "scan177-frozen-"+mode, "192.0.2.177")
			if p["code"] != float64(1) {
				t.Fatal(p)
			}
			trade := p["trade_no"].(string)
			target := f.s.checkoutURL(trade, "scan-region")
			same := risk177ScanGET(f, target, "192.0.2.177")
			if same.Code != 302 {
				t.Fatal("same region must continue", same.Code)
			}
			different := risk177ScanGET(f, target, "192.0.2.178")
			if mode == "1" && different.Code != 403 {
				t.Fatal("different city must reject", different.Code)
			}
			if mode == "2" {
				if different.Code != 200 || !strings.Contains(different.Body.String(), "地区不一致") || !strings.Contains(different.Body.String(), "继续支付") {
					t.Fatal("warning missing", different.Code)
				}
				continued := risk177ScanGET(f, f.s.checkoutURL(trade, "scan-region-continue"), "192.0.2.178")
				if continued.Code != 302 {
					t.Fatal("explicit continue unavailable", continued.Code)
				}
			}
			// A later config change must not rewrite the already born policy.
			v = risk177Data(t, f.request("GET", "", f.csrf))
			risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) { s["check_pay_regoin"] = "0" }), f.csrf))
			again := risk177ScanGET(f, target, "192.0.2.178")
			if again.Code != different.Code {
				t.Fatal("birth policy was replaced", again.Code, different.Code)
			}
			f.s.Store.CoreRiskGeo = risk177GeoFunc(func(netip.Addr) (riskgeo.Region, bool, error) {
				return riskgeo.Region{}, false, errors.New("synthetic local lookup failure")
			})
			if w := risk177ScanGET(f, target, "192.0.2.177"); w.Code != 503 {
				t.Fatal("runtime lookup failure silently allowed", w.Code)
			}
			if _, e := f.s.ProviderFlow.Query(context.Background(), 1, trade); e != nil {
				t.Fatal("query must survive scan restriction", e)
			}
			if w := risk177ScanGET(f, f.s.ProviderReturnURL(trade), "192.0.2.178"); w.Code != 302 {
				t.Fatal("paid return was scan restricted", w.Code)
			}
			if creates.Load() != 1 {
				t.Fatal("scan or query caused another create")
			}
		})
	}
}

func TestRisk177GroupScanOverrideUsesConfigAndInheritedFingerprint(t *testing.T) {
	f, _, _ := risk177Flow(t)
	f.s.Store.CoreRiskGeo = risk177GeoFunc(risk177ScanResolver)
	g := risk177Data(t, f.riskPath("GET", "/groups/0", ""))
	if g["effective_scan_region"] != "0" {
		t.Fatal("scan default missing", g["effective_scan_region"])
	}
	body := `{"expected_fingerprint":"` + g["fingerprint"].(string) + `","config":{"pay_block_oversea":"_","check_pay_regoin":"2"}}`
	g = risk177Data(t, f.riskPath("PATCH", "/groups/0", body))
	if g["effective_scan_region"] != "2" {
		t.Fatal("group scan override missing")
	}
	p := f.pay(t, "scan177-group-warning", "192.0.2.177")
	if p["code"] != float64(1) {
		t.Fatal(p)
	}
	if w := risk177ScanGET(f, f.s.checkoutURL(p["trade_no"].(string), "scan-region"), "192.0.2.178"); w.Code != http.StatusOK {
		t.Fatal("group policy not consumed", w.Code)
	}
}
