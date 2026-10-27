package contract_test

import (
	"bytes"
	"context"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPreauthOnlyChannelUsesDedicatedCreationAndHonestSDKMaterial(t *testing.T) {
	runChannel168PreauthOnlyChannelUsesDedicatedCreationAndHonestSDKMaterial(t, nil)
}
func TestChannel168PreauthOnlyChannelUsesDedicatedCreationAndHonestSDKMaterial(t *testing.T) {
	runChannel168PreauthOnlyChannelUsesDedicatedCreationAndHonestSDKMaterial(t, installChannel168Fees)
}
func runChannel168PreauthOnlyChannelUsesDedicatedCreationAndHonestSDKMaterial(t *testing.T, channelFees channel168FixturePolicy) {
	s, pool, key := fixture(t)
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; t.Error("freeze signing reached upstream") }))
	defer upstream.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"preauth"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{9: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{9}, 32)
	s.MerchantChannels = map[int64][]int64{2: {9}}
	if channelFees != nil {
		applyChannel168Fixture(t, s, channelFees)
	}
	for _, method := range []string{"jsapi", "app", "web"} {
		p := compat.Params{"pid": "2", "type": "alipay", "method": method, "channel_id": "9", "out_trade_no": "preauth-" + method, "name": "test", "money": "1.00", "sign_type": "RSA", "timestamp": strconv.FormatInt(time.Now().Unix(), 10)}
		if method == "jsapi" {
			p["is_applet"] = "1"
		}
		p["sign"], _ = compat.SignRSA(p, key)
		values := url.Values{}
		for k, v := range p {
			values.Set(k, v)
		}
		_, out := call(t, s.Handler(), "POST", "/api/pay/create", values)
		if out["code"] != float64(0) {
			t.Fatal("preauth channel was not routed", method, out)
		}
		want := method
		if method == "web" {
			want = "jump"
		}
		if out["pay_type"] != want {
			t.Fatal("wrong source material type", method, out)
		}
		trade := out["trade_no"].(string)
		attempt, e := s.Store.ProviderAttempt(context.Background(), trade)
		if e != nil || attempt.Method != "preauth" || attempt.MaterialType != "preauth" {
			t.Fatal(attempt, e)
		}
		params, e := url.ParseQuery(attempt.Material)
		if e != nil || params.Get("method") != "alipay.fund.auth.order.app.freeze" || !strings.Contains(params.Get("notify_url"), "/pay/preauthnotify/") {
			t.Fatal("ordinary callback used for authorization", params, e)
		}
		if method != "web" && out["pay_info"] != attempt.Material {
			t.Fatal("raw SDK freeze string lost", out)
		}
		if method == "web" {
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("GET", out["pay_info"].(string), nil))
			if w.Code != 200 || !strings.Contains(w.Body.String(), "授权") || !strings.Contains(w.Body.String(), "alipays://") {
				t.Fatal("authorization checkout unavailable", w.Code, w.Body.String())
			}
		}
	}
	mixed, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"page", "preauth"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow.Providers[10] = mixed
	s.MerchantChannels[2] = append(s.MerchantChannels[2], 10)
	for _, method := range []string{"web", "jsapi"} {
		p := compat.Params{"pid": "2", "type": "alipay", "method": method, "channel_id": "10", "out_trade_no": "mixed-" + method, "name": "test", "money": "1.00", "sub_openid": "2088000000000000", "sign_type": "RSA", "timestamp": strconv.FormatInt(time.Now().Unix(), 10)}
		p["sign"], _ = compat.SignRSA(p, key)
		values := url.Values{}
		for k, v := range p {
			values.Set(k, v)
		}
		_, out := call(t, s.Handler(), "POST", "/api/pay/create", values)
		if method == "web" {
			if out["code"] != float64(0) {
				t.Fatal(out)
			}
			attempt, e := s.Store.ProviderAttempt(context.Background(), out["trade_no"].(string))
			if e != nil || attempt.Method != "page" || attempt.MaterialType != "html" {
				t.Fatal("ordinary page silently replaced by authorization", attempt, e)
			}
		} else if out["code"] != float64(-1) {
			t.Fatal("unsupported mixed-channel JSAPI fell back to preauth", out)
		}
	}
	b, e := s.Store.Balance(context.Background(), 2)
	if e != nil || b.Available != 0 || b.Reserved != 0 || calls != 0 {
		t.Fatal("authorization material affected funds", b, e, calls)
	}
	var captures int
	if e = pool.QueryRow(context.Background(), "SELECT count(*) FROM payment_authorizations").Scan(&captures); e != nil || captures != 0 {
		t.Fatal("freeze material falsely authorized", captures, e)
	}
	if channelFees != nil {
		var successes int
		if e = pool.QueryRow(context.Background(), `SELECT count(*) FROM runtime_channel_success_events`).Scan(&successes); e != nil || successes != 0 {
			t.Fatal("authorization material consumed successful daily quota", successes, e)
		}
	}
}
