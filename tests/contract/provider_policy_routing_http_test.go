package contract_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/backoffice"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"goepay/internal/server"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type routingCalls struct {
	mu       sync.Mutex
	channels map[string][]int64
}

func (c *routingCalls) list(trade string) []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int64{}, c.channels[trade]...)
}
func routingHTTPFixture(t *testing.T) (*server.Server, *pgxpool.Pool, *rsa.PrivateKey, *routingCalls) {
	t.Helper()
	s, pool, key := fixture(t)
	calls := &routingCalls{channels: map[string][]int64{}}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("method") != "alipay.trade.precreate" {
			t.Error("routing invoked unexpected product", r.Form.Get("method"))
			http.Error(w, "bad method", 500)
			return
		}
		// precreate carries an object-valued business_params, as the original
		// payment plugin does. Keep scalar fields typed without flattening it.
		var biz struct {
			TradeNo        string            `json:"out_trade_no"`
			Subject        string            `json:"subject"`
			Amount         string            `json:"total_amount"`
			BusinessParams map[string]string `json:"business_params"`
		}
		if e := json.Unmarshal([]byte(r.Form.Get("biz_content")), &biz); e != nil {
			t.Error(e)
			http.Error(w, "bad body", 500)
			return
		}
		if biz.TradeNo == "" || biz.Subject == "" || biz.Amount == "" || len(biz.BusinessParams) != 1 || biz.BusinessParams["mc_create_trade_ip"] != "192.0.2.1" {
			t.Error("routing request lost original typed payment fields/client IP")
			http.Error(w, "bad original fields", 500)
			return
		}
		id := int64(7)
		if r.Form.Get("app_id") == "app-eight" {
			id = 8
		} else if r.Form.Get("app_id") != "app-seven" {
			t.Error("unconfigured app", r.Form.Get("app_id"))
		}
		trade := biz.TradeNo
		calls.mu.Lock()
		calls.channels[trade] = append(calls.channels[trade], id)
		calls.mu.Unlock()
		if strings.Contains(biz.Subject, "unknown") {
			w.Write([]byte(`{"invalid":"unknown"}`))
			return
		}
		node, _ := json.Marshal(map[string]string{"code": "10000", "out_trade_no": trade, "qr_code": "https://qr.example.invalid/" + fmt.Sprint(id) + "/" + trade})
		digest := sha256.Sum256(node)
		sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_precreate_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(sig)})
	}))
	t.Cleanup(up.Close)
	providers := map[int64]adapter.Provider{}
	for id, app := range map[int64]string{7: "app-seven", 8: "app-eight"} {
		p, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: app, PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"native"}, GatewayURL: up.URL, HTTPClient: up.Client()})
		if e != nil {
			t.Fatal(e)
		}
		providers[id] = p
	}
	s.Sandbox = false
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: providers, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{11}, 32)
	s.MerchantChannels = map[int64][]int64{1: {7, 8}, 2: {7, 8}}
	return s, pool, key, calls
}
func routingPolicy(t *testing.T, s *server.Server, mid, strategy, ids, limits, rules string) int64 {
	t.Helper()
	v := map[string]any{"name": "route", "merchant_id": mid, "type": "alipay", "strategy": strategy, "channel_ids": ids, "enabled": true, "limits": limits, "rules": rules}
	raw := map[string]json.RawMessage{}
	for k, x := range v {
		raw[k], _ = json.Marshal(x)
	}
	item, e := backoffice.New(s.Pool, s.Store).Create(context.Background(), backoffice.Principal{Role: "admin", SubjectID: 1, Username: "admin"}, "pay-rolls", raw)
	if e != nil {
		t.Fatal("save routing policy", e)
	}
	return item.ID
}
func routingPatch(t *testing.T, s *server.Server, id int64, v map[string]any) {
	t.Helper()
	raw := map[string]json.RawMessage{}
	for k, x := range v {
		raw[k], _ = json.Marshal(x)
	}
	if _, e := backoffice.New(s.Pool, s.Store).Update(context.Background(), backoffice.Principal{Role: "admin", SubjectID: 1, Username: "admin"}, "pay-rolls", id, raw); e != nil {
		t.Fatal("update routing", e)
	}
}
func routingInvoke(t *testing.T, s *server.Server, key *rsa.PrivateKey, mid int64, no, name, money string, extra compat.Params) map[string]any {
	t.Helper()
	p := compat.Params{"pid": fmt.Sprint(mid), "out_trade_no": no, "type": "alipay", "method": "native", "name": name, "money": money, "clientip": "192.0.2.1"}
	for k, v := range extra {
		p[k] = v
	}
	path := "/mapi.php"
	var values url.Values
	if mid == 1 {
		// The original mapi requires notify_url (api/Pay.php create()).
		if p["notify_url"] == "" {
			p["notify_url"] = "https://merchant.example.invalid/notify"
		}
		values = signed(p, "contract-secret")
	} else {
		path = "/api/pay/create"
		p["sign_type"] = "RSA"
		p["timestamp"] = fmt.Sprint(time.Now().Unix())
		p["sign"], _ = compat.SignRSA(p, key)
		values = url.Values{}
		for k, x := range p {
			values.Set(k, x)
		}
	}
	_, out := call(t, s.Handler(), "POST", path, values)
	return out
}
func routingTrade(t *testing.T, s *server.Server, mid int64, no string) string {
	t.Helper()
	o, e := s.Store.Order(context.Background(), mid, no)
	if e != nil {
		t.Fatal(e)
	}
	return o.TradeNo
}

func TestPayRollHTTPRoundRobinStickyConcurrencyAndUnknownNoFailover(t *testing.T) {
	s, pool, key, calls := routingHTTPFixture(t)
	ctx := context.Background()
	rule := routingPolicy(t, s, "1", "round_robin", "7:1,8:1", "{}", "{}")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); routingInvoke(t, s, key, 1, "same-order", "item", "1.00", nil) }()
	}
	wg.Wait()
	trade := routingTrade(t, s, 1, "same-order")
	if got := calls.list(trade); len(got) != 1 || got[0] != 7 {
		t.Fatal("retry changed channel or sent twice", got)
	}
	var assignments, assigned, cursor int64
	if e := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM provider_route_assignments WHERE merchant_id=1),(SELECT assigned FROM provider_route_counters WHERE rule_id=$1),(SELECT cursor FROM provider_route_counters WHERE rule_id=$1)", rule).Scan(&assignments, &assigned, &cursor); e != nil || assignments != 1 || assigned != 1 || cursor != 1 {
		t.Fatal("duplicate consumed routing quota", assignments, assigned, cursor, e)
	}
	// A second server has no shared in-memory selection counter.
	s2 := &server.Server{Store: s.Store, Pool: s.Pool, ProviderFlow: s.ProviderFlow, CheckoutKey: s.CheckoutKey, MerchantChannels: s.MerchantChannels, PlatformKey: s.PlatformKey, Sessions: s.Sessions}
	if out := routingInvoke(t, s2, key, 1, "next-order", "item", "1.00", nil); out["code"] != float64(1) {
		t.Fatal(out)
	}
	if got := calls.list(routingTrade(t, s, 1, "next-order")); len(got) != 1 || got[0] != 8 {
		t.Fatal("round robin was not persistent", got)
	}
	if out := routingInvoke(t, s, key, 1, "unknown-order", "unknown-result", "1.00", nil); out["code"] != float64(-1) {
		t.Fatal("unknown became success", out)
	}
	unknown := routingTrade(t, s, 1, "unknown-order")
	routingPatch(t, s, rule, map[string]any{"enabled": false})
	routingInvoke(t, s2, key, 1, "unknown-order", "unknown-result", "1.00", nil)
	if got := calls.list(unknown); len(got) != 1 || got[0] != 7 {
		t.Fatal("unknown silently failed over", got)
	}
	if out := routingInvoke(t, s, key, 1, "unknown-order", "unknown-result", "1.00", compat.Params{"channel_id": "8"}); out["code"] != float64(-1) {
		t.Fatal("explicit channel changed existing attempt", out)
	}
	s.MerchantChannels[1] = []int64{8}
	if out := routingInvoke(t, s, key, 1, "unknown-order", "unknown-result", "1.00", nil); out["code"] != float64(-1) {
		t.Fatal("revoked channel material exposed", out)
	}
	if got := calls.list(unknown); len(got) != 1 {
		t.Fatal("revocation resubmitted", got)
	}
	if _, e := pool.Exec(ctx, "UPDATE provider_route_assignments SET channel_id=8 WHERE merchant_id=1 AND out_trade_no='unknown-order'"); e == nil {
		t.Fatal("sticky route was mutable")
	}
}

func TestPayRollHTTPStrategiesScopeAndEligibility(t *testing.T) {
	s, pool, key, calls := routingHTTPFixture(t)
	ctx := context.Background()
	// Old enabled records contain no executable scope/type and remain descriptive.
	if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('pay-rolls','{"name":"legacy","strategy":"weighted","channel_ids":"8:100","enabled":true}')`); e != nil {
		t.Fatal(e)
	}
	routingInvoke(t, s, key, 1, "legacy-ignored", "item", "1.00", nil)
	if got := calls.list(routingTrade(t, s, 1, "legacy-ignored")); len(got) != 1 || got[0] != 7 {
		t.Fatal("legacy data unexpectedly executed", got)
	}
	global := routingPolicy(t, s, "0", "first", "8:1", "{}", "{}")
	local := routingPolicy(t, s, "1", "weighted", "7:0,8:3", "{}", "{}")
	for _, scenario := range []struct {
		strategy, ids, rules string
		want                 int64
	}{{"weighted", "7:0,8:3", "{}", 8}, {"first", "7:0,8:3", "{}", 7}, {"priority", "7:1,8:2", "{}", 8}, {"first", "7:1,8:1", `{"7":{"min_amount":"2.00"}}`, 8}} {
		routingPatch(t, s, local, map[string]any{"strategy": scenario.strategy, "channel_ids": scenario.ids, "rules": scenario.rules})
		no := scenario.strategy + "-" + fmt.Sprint(scenario.want) + "-" + fmt.Sprint(len(scenario.rules))
		out := routingInvoke(t, s, key, 1, no, "item", "1.00", nil)
		if out["code"] != float64(1) {
			t.Fatal(out)
		}
		if got := calls.list(routingTrade(t, s, 1, no)); len(got) != 1 || got[0] != scenario.want {
			t.Fatal("strategy did not drive real upstream", scenario, got)
		}
	}
	routingInvoke(t, s, key, 2, "global-scope", "item", "1.00", nil)
	if got := calls.list(routingTrade(t, s, 2, "global-scope")); len(got) != 1 || got[0] != 8 {
		t.Fatal("global scope not applied", got)
	}
	routingPatch(t, s, local, map[string]any{"channel_ids": "7:1", "rules": "{}"})
	s.MerchantChannels[1] = []int64{8}
	if out := routingInvoke(t, s, key, 1, "no-eligible", "item", "1.00", nil); out["code"] != float64(-1) {
		t.Fatal("empty eligible set fell back to global", out)
	}
	var count int
	if e := pool.QueryRow(ctx, "SELECT count(*) FROM provider_route_assignments WHERE out_trade_no='no-eligible'").Scan(&count); e != nil || count != 0 {
		t.Fatal("ineligible request reserved quota", count, e)
	}
	wx, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "wxapp", MerchantID: "mch", APIKey: "0123456789abcdef0123456789abcdef", Methods: []string{"native"}})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow.Providers[9] = wx
	s.MerchantChannels[1] = []int64{9}
	routingPatch(t, s, local, map[string]any{"channel_ids": "9:1"})
	if out := routingInvoke(t, s, key, 1, "wrong-provider-type", "item", "1.00", nil); out["code"] != float64(-1) {
		t.Fatal("cross-provider routing accepted", out)
	}
	scanUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("default routing submitted a scan payment")
		http.Error(w, "unexpected", 500)
	}))
	defer scanUp.Close()
	scanOnly, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "scan-only", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"scan"}, GatewayURL: scanUp.URL, HTTPClient: scanUp.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow.Providers[9] = scanOnly
	if out := routingInvoke(t, s, key, 1, "scan-must-be-explicit", "item", "1.00", compat.Params{"method": "", "auth_code": "280000000000000001"}); out["code"] != float64(-1) {
		t.Fatal("scan was chosen as a default product", out)
	}
	_ = global
}

func TestPayRollHTTPAmountTimeRateDailyLimitsAndLocalPreflight(t *testing.T) {
	s, pool, key, calls := routingHTTPFixture(t)
	ctx := context.Background()
	rule := routingPolicy(t, s, "1", "first", "7:1,8:1", `{"min_amount":"1.00","max_amount":"2.00"}`, "{}")
	for _, money := range []string{"0.99", "2.01", "invalid"} {
		if out := routingInvoke(t, s, key, 1, "bad-amount-"+money, "item", money, nil); out["code"] != float64(-1) {
			t.Fatal(out)
		}
	}
	// An over-long name is NOT a rejection case: the legacy wire truncates a
	// >127-byte name at a UTF-8 boundary (mb_strcut) and still creates the order
	// (see internal/server/provider_http.go), so it never blocks routing.
	for _, extra := range []compat.Params{{"method": "h5"}, {"method": "jsapi"}, {"notify_url": "javascript:alert(1)"}, {"clientip": "host.example.invalid"}} {
		if out := routingInvoke(t, s, key, 1, "invalid-prerequisite-"+fmt.Sprint(len(extra)), "item", "1.00", extra); out["code"] != float64(-1) {
			t.Fatal("invalid local prerequisite routed", out)
		}
	}
	var count int
	if e := pool.QueryRow(ctx, "SELECT count(*) FROM provider_route_assignments").Scan(&count); e != nil || count != 0 {
		t.Fatal("bad local input consumed selection", count, e)
	}
	hour := time.Now().In(time.FixedZone("UTC+8", 28800)).Hour()
	blocked := fmt.Sprintf(`{"start_hour":%d,"end_hour":%d}`, (hour+1)%24, (hour+2)%24)
	routingPatch(t, s, rule, map[string]any{"limits": blocked})
	if out := routingInvoke(t, s, key, 1, "outside-hours", "item", "1.00", nil); out["code"] != float64(-1) {
		t.Fatal("UTC+8 period bypass", out)
	}
	routingPatch(t, s, rule, map[string]any{"limits": `{"daily_count":2,"daily_amount":"2.00"}`, "rules": `{"7":{"rate_limit_seconds":60,"rate_limit_count":1},"8":{"rate_limit_seconds":60,"rate_limit_count":1}}`})
	for i, want := range []int64{7, 8} {
		no := fmt.Sprintf("rate-%d", i)
		if out := routingInvoke(t, s, key, 1, no, "item", "1.00", nil); out["code"] != float64(1) {
			t.Fatal(out)
		}
		if got := calls.list(routingTrade(t, s, 1, no)); len(got) != 1 || got[0] != want {
			t.Fatal("channel rate limit not enforced", want, got)
		}
	}
	for _, no := range []string{"third-rate", "fourth-rate"} {
		if out := routingInvoke(t, s, key, 1, no, "item", "1.00", nil); out["code"] != float64(-1) {
			t.Fatal("daily count/amount exceeded", out)
		}
	}
	// Retries of an existing binding do not consume quota a second time.
	if out := routingInvoke(t, s, key, 1, "rate-0", "item", "1.00", nil); out["code"] != float64(1) {
		t.Fatal("sticky retry was rate limited", out)
	}
	var assigned int64
	if e := pool.QueryRow(ctx, "SELECT assigned FROM provider_route_counters WHERE rule_id=$1", rule).Scan(&assigned); e != nil || assigned != 2 {
		t.Fatal("quota counted a retry", assigned, e)
	}
	// Verify each group limit independently of the per-channel limiter.
	routingPatch(t, s, rule, map[string]any{"limits": `{"daily_count":3}`, "rules": "{}"})
	if out := routingInvoke(t, s, key, 1, "daily-count-third", "item", "1.00", nil); out["code"] != float64(1) {
		t.Fatal(out)
	}
	if out := routingInvoke(t, s, key, 1, "daily-count-fourth", "item", "1.00", nil); out["code"] != float64(-1) {
		t.Fatal("daily count ignored", out)
	}
	routingPatch(t, s, rule, map[string]any{"limits": `{"daily_amount":"3.50"}`})
	if out := routingInvoke(t, s, key, 1, "daily-money-over", "item", "0.51", nil); out["code"] != float64(-1) {
		t.Fatal("daily amount ignored", out)
	}
	if out := routingInvoke(t, s, key, 1, "daily-money-boundary", "item", "0.50", nil); out["code"] != float64(1) {
		t.Fatal("daily amount exact boundary rejected", out)
	}
}

func TestPayRollHTTPConcurrentDistinctOrdersCannotOverbookRate(t *testing.T) {
	s, pool, key, calls := routingHTTPFixture(t)
	rule := routingPolicy(t, s, "1", "first", "7:1", `{"rate_limit_seconds":60,"rate_limit_count":3}`, "{}")
	var wg sync.WaitGroup
	results := make(chan map[string]any, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results <- routingInvoke(t, s, key, 1, fmt.Sprintf("rate-race-%d", i), "item", "1.00", nil)
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for out := range results {
		if out["code"] == float64(1) {
			success++
		}
	}
	var count, assigned int
	if e := pool.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM provider_route_assignments),(SELECT assigned FROM provider_route_counters WHERE rule_id=$1)", rule).Scan(&count, &assigned); e != nil || count != 3 || assigned != 3 || success != 3 {
		t.Fatal("concurrent rate overbooking", count, assigned, success, e)
	}
	calls.mu.Lock()
	defer calls.mu.Unlock()
	sent := 0
	for _, channels := range calls.channels {
		sent += len(channels)
	}
	if sent != 3 {
		t.Fatal("rate limit did not govern real submissions", sent)
	}
}
