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
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAlipayJSAPIMiniCreateDoesNotCreditUntilVerifiedPayment(t *testing.T) {
	s, pool, key := fixture(t)
	var creates atomic.Int32
	var paid atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		var biz map[string]any
		json.Unmarshal([]byte(r.Form.Get("biz_content")), &biz)
		node := map[string]any{"code": "10000", "out_trade_no": biz["out_trade_no"], "trade_no": "synthetic-ali-" + biz["out_trade_no"].(string)}
		switch r.Form.Get("method") {
		case "alipay.trade.create":
			creates.Add(1)
			if r.Form.Get("notify_url") == "" {
				t.Error("missing notify_url")
			}
			if biz["product_code"] == "JSAPI_PAY" {
				if biz["op_app_id"] != "miniapp" || biz["op_buyer_open_id"] != "authorized-openid" || biz["buyer_open_id"] != nil {
					t.Error("mini identity was not bound", biz)
				}
			} else {
				if biz["buyer_id"] != "2088000000000000" {
					t.Error("traditional buyer was not bound", biz)
				}
			}
		case "alipay.trade.query":
			node["total_amount"] = "1.00"
			node["trade_status"] = "WAIT_BUYER_PAY"
			if paid.Load() {
				node["trade_status"] = "TRADE_SUCCESS"
			}
		default:
			t.Error("unexpected upstream method")
		}
		raw, _ := json.Marshal(node)
		sum := sha256.Sum256(raw)
		signature, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
		if e != nil {
			t.Error(e)
		}
		json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(r.Form.Get("method"), ".", "_") + "_response": json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(signature)})
	}))
	defer upstream.Close()
	traditional, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "webapp", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"jsapi"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	mini, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "miniapp", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"mini"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{9: traditional, 10: mini}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{9}, 32)
	s.MerchantChannels = map[int64][]int64{2: {9, 10}}
	invoke := func(p compat.Params) map[string]any {
		p["pid"] = "2"
		p["type"] = "alipay"
		p["method"] = "jsapi"
		p["name"] = "test"
		p["money"] = "1.00"
		p["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
		p["sign_type"] = "RSA"
		p["sign"], _ = compat.SignRSA(p, key)
		values := url.Values{}
		for k, v := range p {
			values.Set(k, v)
		}
		_, out := call(t, s.Handler(), "POST", "/api/pay/create", values)
		return out
	}
	for i, p := range []compat.Params{{"channel_id": "9", "out_trade_no": "ali-jsapi", "sub_openid": "2088000000000000", "sub_appid": "webapp"}, {"channel_id": "10", "out_trade_no": "ali-mini", "sub_openid": "authorized-openid", "sub_appid": "miniapp", "is_applet": "1"}} {
		out := invoke(p)
		if out["code"] != float64(0) || out["pay_type"] != "jsapi" || out["pay_info"] != "synthetic-ali-"+out["trade_no"].(string) {
			t.Fatal("wrong V2 JSAPI material", out)
		}
		trade := out["trade_no"].(string)
		again := invoke(p)
		if again["trade_no"] != trade || creates.Load() != int32(i+1) {
			t.Fatal("duplicate create resent upstream", again)
		}
		b, e := s.Store.Balance(context.Background(), 2)
		if e != nil || b.Available != int64(i)*100 {
			t.Fatal("trade.create falsely credited", b, e)
		}
		paid.Store(false)
		if _, e = s.ProviderFlow.Query(context.Background(), 2, trade); e != nil {
			t.Fatal(e)
		}
		b, _ = s.Store.Balance(context.Background(), 2)
		if b.Available != int64(i)*100 {
			t.Fatal("pending query credited", b)
		}
		paid.Store(true)
		for j := 0; j < 2; j++ {
			if _, e = s.ProviderFlow.Query(context.Background(), 2, trade); e != nil {
				t.Fatal(e)
			}
		}
		b, _ = s.Store.Balance(context.Background(), 2)
		if b.Available != int64(i+1)*100 {
			t.Fatal("verified payment not idempotent", b)
		}
	}
	before := creates.Load()
	for _, p := range []compat.Params{{"channel_id": "10", "out_trade_no": "missing-buyer", "is_applet": "1"}, {"channel_id": "10", "out_trade_no": "wrong-mini-app", "is_applet": "1", "sub_appid": "foreign-app", "sub_openid": "authorized-openid"}} {
		out := invoke(p)
		if out["code"] != float64(-1) {
			t.Fatal(out)
		}
	}
	var count int
	if e := pool.QueryRow(context.Background(), "SELECT count(*) FROM orders WHERE out_trade_no IN ('missing-buyer','wrong-mini-app')").Scan(&count); e != nil || count != 0 || creates.Load() != before {
		t.Fatal("invalid identity allocated order/upstream", count, e)
	}
}
