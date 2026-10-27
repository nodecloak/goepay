package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/riskgeo"
)

type risk177Provider struct {
	channel168Provider
	target   string
	client   *http.Client
	mu       sync.Mutex
	requests map[string]adapter.CreateRequest
}

func (*risk177Provider) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{Methods: []string{"native", "jsapi"}, Query: true}
}

func (p *risk177Provider) Create(ctx context.Context, in adapter.CreateRequest) (adapter.CreateResult, error) {
	raw, e := json.Marshal(in)
	if e != nil {
		return adapter.CreateResult{}, e
	}
	r, e := http.NewRequestWithContext(ctx, "POST", p.target+"/create", bytes.NewReader(raw))
	if e != nil {
		return adapter.CreateResult{}, e
	}
	resp, e := p.client.Do(r)
	if e != nil {
		return adapter.CreateResult{}, e
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		return adapter.CreateResult{}, fmt.Errorf("synthetic risk sink rejected")
	}
	p.mu.Lock()
	p.requests[in.TradeNo] = in
	p.mu.Unlock()
	out, e := p.channel168Provider.Create(ctx, in)
	out.ProviderTradeNo = "R177_" + in.TradeNo
	return out, e
}
func (p *risk177Provider) Query(ctx context.Context, in adapter.QueryRequest) (adapter.QueryResult, error) {
	p.mu.Lock()
	original, ok := p.requests[in.TradeNo]
	p.mu.Unlock()
	if !ok {
		return adapter.QueryResult{}, adapter.ErrInvalid
	}
	r, e := http.NewRequestWithContext(ctx, "GET", p.target+"/query", nil)
	if e != nil {
		return adapter.QueryResult{}, e
	}
	resp, e := p.client.Do(r)
	if e != nil {
		return adapter.QueryResult{}, e
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		return adapter.QueryResult{}, adapter.ErrAuthentication
	}
	buyer := original.OpenID
	if buyer == "" {
		buyer = "synthetic-risk177-buyer"
	}
	if buyer == "synthetic-openid-alias" {
		buyer = "synthetic-userid-alias"
	}
	return adapter.QueryResult{TradeNo: in.TradeNo, ProviderTradeNo: "R177_" + in.TradeNo, State: "SUCCESS", Currency: "CNY", Amount: original.Amount, BuyerID: buyer}, nil
}
func risk177Flow(t *testing.T) (risk177Fixture, *risk177Provider, *atomic.Int32) {
	t.Helper()
	f := risk177Local(t)
	var creates atomic.Int32
	sink := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/create" {
			var original adapter.CreateRequest
			if json.NewDecoder(r.Body).Decode(&original) != nil || original.TradeNo == "" || original.Amount <= 0 {
				t.Error("bad original synthetic create")
			}
			creates.Add(1)
		} else if r.Method != "GET" || r.URL.Path != "/query" {
			t.Error("unexpected local provider request")
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(sink.Close)
	p := &risk177Provider{channel168Provider: channel168Provider{app: "risk177-app"}, target: sink.URL, client: sink.Client(), requests: map[string]adapter.CreateRequest{}}
	f.s.ProviderFlow.Providers[7] = p
	return f, p, &creates
}
func (f risk177Fixture) pay(t *testing.T, no, ip string) map[string]any {
	return f.payBuyer(t, no, ip, "")
}
func (f risk177Fixture) payBuyer(t *testing.T, no, ip, buyer string) map[string]any {
	t.Helper()
	p := compat.Params{"pid": "1", "out_trade_no": no, "type": "alipay", "name": "synthetic risk177", "money": "1.00", "channel_id": "7", "method": "native", "clientip": ip, "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
	if buyer != "" {
		p["sub_openid"] = buyer
		p["method"] = "jsapi"
	}
	p["sign"] = compat.SignMD5(p, "test-secret")
	v := url.Values{}
	for key, value := range p {
		v.Set(key, value)
	}
	r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = net.JoinHostPort(ip, "49177")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var out map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal("nonJSON original payment", w.Code, e)
	}
	return out
}
func TestRisk177BoundPrepaymentIdentityAndVerifiedReceiptAliasCannotResetBuyerCount(t *testing.T) {
	f, _, creates := risk177Flow(t)
	v := risk177Data(t, f.request("GET", "", f.csrf))
	risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) { s["pay_userlimit"] = "1" }), f.csrf))
	p := f.payBuyer(t, "risk177-alias-first", "192.0.2.177", "synthetic-openid-alias")
	if p["code"] != float64(1) {
		t.Fatal(p)
	}
	if _, e := f.s.ProviderFlow.Query(context.Background(), 1, p["trade_no"].(string)); e != nil {
		t.Fatal(e)
	}
	for i, buyer := range []string{"synthetic-openid-alias", "synthetic-userid-alias"} {
		p = f.payBuyer(t, fmt.Sprint("risk177-alias-repeat-", i), fmt.Sprintf("192.0.2.%d", 180+i), buyer)
		if p["code"] == float64(1) || creates.Load() != 1 {
			t.Fatal("provider-bound original buyer was forgotten after receipt used its verified alternate ID", buyer, p["code"], creates.Load())
		}
	}
}
func TestRisk177BuyerConfirmedCountAndAccumulatedGrossThreshold(t *testing.T) {
	for _, rule := range []string{"pay_userlimit", "pay_daymoney"} {
		t.Run(rule, func(t *testing.T) {
			f, _, creates := risk177Flow(t)
			v := risk177Data(t, f.request("GET", "", f.csrf))
			value := "1"
			if rule == "pay_daymoney" {
				value = "1.50"
			}
			risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) { s[rule] = value }), f.csrf))
			n := 1
			if rule == "pay_daymoney" {
				n = 2
			}
			for i := 0; i < n; i++ {
				p := f.payBuyer(t, fmt.Sprint("buyer177-", i), fmt.Sprintf("192.0.2.%d", 180+i), "synthetic-risk177-buyer")
				if p["code"] != float64(1) {
					t.Fatal("threshold was changed into incoming-amount hard cap", p)
				}
				if _, e := f.s.ProviderFlow.Query(context.Background(), 1, p["trade_no"].(string)); e != nil {
					t.Fatal(e)
				}
			}
			before := creates.Load()
			p := f.payBuyer(t, "buyer177-blocked", "192.0.2.190", "synthetic-risk177-buyer")
			if p["code"] == float64(1) || creates.Load() != before {
				t.Fatal("original buyer threshold did not prevent new IO", rule, p["code"], creates.Load())
			}
			p = f.payBuyer(t, "buyer177-other", "192.0.2.191", "synthetic-other-buyer")
			if p["code"] != float64(1) {
				t.Fatal("unrelated buyer blocked", p)
			}
		})
	}
}
func TestRisk177ConcurrentSameBuyerCannotSpendOneCountSlotTwice(t *testing.T) {
	f, _, creates := risk177Flow(t)
	v := risk177Data(t, f.request("GET", "", f.csrf))
	risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) { s["pay_userlimit"] = "1" }), f.csrf))
	start := make(chan struct{})
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			p := f.payBuyer(t, fmt.Sprint("risk177-concurrent-", i), fmt.Sprintf("192.0.2.%d", 180+i), "synthetic-risk177-buyer")
			if p["code"] == float64(1) {
				accepted.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if accepted.Load() != 1 || creates.Load() != 1 {
		t.Fatal("buyer count admission raced original pending slot", accepted.Load(), creates.Load())
	}
}
func TestRisk177ConfiguredIPRulesActuallyBlockNewProviderIO(t *testing.T) {
	for _, rule := range []string{"pay_iplimit", "pay_ip_interval"} {
		t.Run(rule, func(t *testing.T) {
			f, _, creates := risk177Flow(t)
			v := risk177Data(t, f.request("GET", "", f.csrf))
			value := "1"
			if rule == "pay_ip_interval" {
				value = "3600"
			}
			risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) { s[rule] = value }), f.csrf))
			first := f.pay(t, "risk177-first", "192.0.2.177")
			if first["code"] != float64(1) || creates.Load() != 1 {
				t.Fatal("initial valid newpayment was not accepted", first, creates.Load())
			}
			if rule == "pay_iplimit" {
				if _, e := f.s.ProviderFlow.Query(context.Background(), 1, first["trade_no"].(string)); e != nil {
					t.Fatal("original authenticated local success", e)
				}
			}
			second := f.pay(t, "risk177-second", "192.0.2.177")
			if second["code"] == float64(1) || creates.Load() != 1 {
				t.Fatal("saved original IP rule was metadata only; second new Create reached local provider", rule, creates.Load(), second["code"])
			}
		})
	}
}
func TestRisk177EnabledGeographicRuleCannotSilentlyRunWithoutResolver(t *testing.T) {
	f, _, creates := risk177Flow(t)
	f.s.Store.CoreRiskGeo = risk177GeoFunc(func(netip.Addr) (riskgeo.Region, bool, error) {
		return riskgeo.Region{Country: "中国", Province: "允许省"}, true, nil
	})
	v := risk177Data(t, f.request("GET", "", f.csrf))
	risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) { s["pay_region_block"] = "测试省" }), f.csrf))
	f.s.Store.CoreRiskGeo = nil
	p := f.pay(t, "risk177-no-geodata", "192.0.2.177")
	if p["code"] == float64(1) || creates.Load() != 0 {
		t.Fatal("enabled geographic policy silently ignored missing local resolver", p["code"], creates.Load())
	}
}
func TestRisk177CannotEnableGeoWithoutConfiguredLocalDatabases(t *testing.T) {
	f := risk177Local(t)
	v := risk177Data(t, f.request("GET", "", f.csrf))
	for _, key := range []string{"pay_region_block", "pay_block_oversea"} {
		value := "测试省"
		if key == "pay_block_oversea" {
			value = "1"
		}
		if w := f.request("PATCH", risk177Body(t, v, func(s map[string]any) { s[key] = value }), f.csrf); w.Code != 422 {
			t.Fatal("missing local lookup capability accepted an enabled policy", key, w.Code)
		}
	}
	fresh := risk177Data(t, f.request("GET", "", f.csrf))
	if fresh["revision"] != "0" {
		t.Fatal("rejected capability write changed policy")
	}
}
func TestRisk177OriginalTodaySuccessCountsAfterPolicyActivation(t *testing.T) {
	f, _, creates := risk177Flow(t)
	ctx := context.Background()
	// Import genuine legacy history; a new104 birth must retain its immutable
	// created_at commitment rather than being aged after it was authenticated.
	var trade string
	e := f.s.Pool.QueryRow(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount,platform_fee,fee_policy,refund_fee_payer,notify_url,return_url) VALUES(nextval('trade_no_seq')::text,1,'risk177-before-policy','synthetic legacy risk','alipay',100,100,0,'none','platform','https://merchant.example.invalid/notify','https://merchant.example.invalid/return') RETURNING trade_no`).Scan(&trade)
	if e != nil {
		t.Fatal(e)
	}
	o, e := f.s.Store.OrderByTradeNo(ctx, trade)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Store.SetOrderClientIP(ctx, o.ID, "192.0.2.177"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.177", ""); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Pool.Exec(ctx, `UPDATE orders SET created_at=clock_timestamp()-interval '10 days' WHERE id=$1`, o.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	v := risk177Data(t, f.request("GET", "", f.csrf))
	risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) { s["pay_iplimit"] = "1" }), f.csrf))
	p := f.pay(t, "risk177-after-policy", "192.0.2.177")
	if p["code"] == float64(1) || creates.Load() != 1 {
		t.Fatal("real prior success today was excluded by old creation date or missing new risk projection", p["code"], creates.Load())
	}
}
func TestRisk177OriginalCheckActuallyBansUnpaidIPAndLeavesRecovery(t *testing.T) {
	f, _, creates := risk177Flow(t)
	v := risk177Data(t, f.request("GET", "", f.csrf))
	v = risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) {
		s["auto_check_payip"] = "1"
		s["check_payip_second"] = "3600"
		s["check_payip_count"] = "1"
	}), f.csrf))
	first := f.pay(t, "risk177-check-original", "192.0.2.177")
	if first["code"] != float64(1) {
		t.Fatal(first)
	}
	r := httptest.NewRequest("POST", "/console/v1/admin/payment-risk/check", strings.NewReader(fmt.Sprintf(`{"expected_fingerprint":%q,"external_no":"risk177-check-ip"}`, v["fingerprint"])))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	out := risk177Data(t, w)
	if out["actions"] != float64(1) {
		t.Fatal("real core check did not produce IP action", out)
	}
	if repeat := f.check(t, v, "risk177-check-ip-second"); repeat["actions"] != float64(0) {
		t.Fatal("original IP rule must preserve existing permanent/longer-than-four-day ban", repeat)
	}
	second := f.pay(t, "risk177-check-blocked", "192.0.2.177")
	if second["code"] == float64(1) || creates.Load() != 1 {
		t.Fatal("Check action not consumed by new admission", second["code"], creates.Load())
	}
	if _, e := f.s.ProviderFlow.Query(context.Background(), 1, first["trade_no"].(string)); e != nil {
		t.Fatal("original recovery was stopped by core risk ban", e)
	}
}

type risk177GeoFunc func(netip.Addr) (riskgeo.Region, bool, error)

func (f risk177GeoFunc) Lookup(ip netip.Addr) (riskgeo.Region, bool, error) { return f(ip) }
func TestRisk177GeographicRegionOverseaAndExactIPWhiteScope(t *testing.T) {
	f, _, creates := risk177Flow(t)
	f.s.Store.CoreRiskGeo = risk177GeoFunc(func(ip netip.Addr) (riskgeo.Region, bool, error) {
		if ip.String() == "192.0.2.177" {
			return riskgeo.Region{Country: "中国", Province: "测试省", City: "测试市"}, true, nil
		}
		if ip.String() == "192.0.2.178" {
			return riskgeo.Region{Country: "美国"}, true, nil
		}
		return riskgeo.Region{Country: "中国", Province: "允许省"}, true, nil
	})
	v := risk177Data(t, f.request("GET", "", f.csrf))
	v = risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) {
		s["pay_region_block"] = "测试省"
		s["pay_block_oversea"] = "1"
		s["pay_iplimit_white"] = "192.0.2.177"
	}), f.csrf))
	for i, ip := range []string{"192.0.2.177", "192.0.2.178"} {
		p := f.pay(t, fmt.Sprint("risk177-geo-denied-", i), ip)
		if p["code"] == float64(1) || creates.Load() != 0 {
			t.Fatal("geo blocked region or oversea escaped; IP whitelist must only affect IP frequency", ip, p["code"], creates.Load())
		}
	}
	p := f.pay(t, "risk177-geo-allowed", "192.0.2.179")
	if p["code"] != float64(1) || creates.Load() != 1 {
		t.Fatal("allowed local region denied", p)
	}
}
func (f risk177Fixture) check(t *testing.T, v map[string]any, key string) map[string]any {
	t.Helper()
	r := httptest.NewRequest("POST", "/console/v1/admin/payment-risk/check", strings.NewReader(fmt.Sprintf(`{"expected_fingerprint":%q,"external_no":%q}`, v["fingerprint"], key)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return risk177Data(t, w)
}
func TestRisk177CheckOriginalBuyerMerchantChannelAndDisabledRules(t *testing.T) {
	for _, rule := range []string{"buyer", "merchant", "channel", "disabled"} {
		t.Run(rule, func(t *testing.T) {
			f, _, creates := risk177Flow(t)
			v := risk177Data(t, f.request("GET", "", f.csrf))
			v = risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) {
				switch rule {
				case "buyer":
					s["auto_check_payspeed"] = "1"
					s["check_payspeed_second"] = "3600"
					s["check_payspeed_count"] = "1"
					s["check_payspeed_days"] = "0"
				case "merchant":
					s["auto_check_sucrate"] = "1"
					s["check_sucrate_second"] = "3600"
					s["check_sucrate_count"] = "1"
					s["check_sucrate_value"] = "50.00"
				case "channel":
					s["auto_check_channel"] = "1"
					s["check_channel_second"] = "3600"
					s["check_channel_failcount"] = "2"
					s["check_channel_ids"] = "7"
				case "disabled":
					s["check_payip_second"] = "3600"
					s["check_payip_count"] = "1"
				}
			}), f.csrf))
			p := f.payBuyer(t, "risk177-check-first", "192.0.2.177", "synthetic-risk177-buyer")
			if p["code"] != float64(1) {
				t.Fatal(p)
			}
			if rule == "buyer" || rule == "channel" {
				if _, e := f.s.ProviderFlow.Query(context.Background(), 1, p["trade_no"].(string)); e != nil {
					t.Fatal(e)
				}
			}
			if rule == "channel" {
				for i := 0; i < 2; i++ {
					p = f.pay(t, fmt.Sprint("risk177-unpaid-", i), fmt.Sprintf("192.0.2.%d", 180+i))
					if p["code"] != float64(1) {
						t.Fatal(p)
					}
				}
			}
			out := f.check(t, v, "risk177-check-four")
			want := float64(1)
			if rule == "disabled" {
				want = 0
			}
			if out["actions"] != want {
				t.Fatal("original check branch not performed", rule, out)
			}
			before := creates.Load()
			p = f.payBuyer(t, "risk177-after-check", "192.0.2.190", "synthetic-risk177-buyer")
			if rule == "disabled" {
				if p["code"] != float64(1) {
					t.Fatal("disabled Check acted", p)
				}
			} else if p["code"] == float64(1) || creates.Load() != before {
				t.Fatal("persisted Check result ignored", rule, p["code"], creates.Load())
			}
			replay := f.check(t, v, "risk177-check-four")
			if replay["actions"] != out["actions"] {
				t.Fatal("manual run key not idempotent", replay, out)
			}
		})
	}
}
