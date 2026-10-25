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
	"encoding/xml"
	"fmt"
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

func TestCommonProviderProductsExposeSDKMaterials(t *testing.T) {
	s, pool, rsaKey := fixture(t)
	key := "0123456789abcdef0123456789abcdef"
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var in struct {
			TradeType string `xml:"trade_type"`
			OpenID    string `xml:"openid"`
			IP        string `xml:"spbill_create_ip"`
		}
		xml.NewDecoder(r.Body).Decode(&in)
		if in.IP != "203.0.113.5" {
			t.Error("signed end-user IP was ignored", in.IP)
		}
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "wxapp", "mch_id": "mch", "prepay_id": "synthetic-prepay", "trade_type": in.TradeType}
		if in.TradeType == "MWEB" {
			out["mweb_url"] = "https://wx.tenpay.com/cgi-bin/mmpayweb-bin/checkmweb?prepay_id=synthetic"
		}
		if in.TradeType == "JSAPI" && in.OpenID != "openid" {
			t.Error("missing signed openid")
		}
		out["sign"], _ = adapter.SignWechatV2(out, key, "MD5")
		w.Write(providerXML(out))
	}))
	defer upstream.Close()
	wx, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "wxapp", MerchantID: "mch", APIKey: key, Methods: []string{"app", "h5", "jsapi"}, H5SiteURL: "https://pay.example.invalid/", H5SiteName: "测试站点", BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "aliapp", PrivateKey: rsaKey, PublicKey: &rsaKey.PublicKey, Methods: []string{"app"}})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: wx, 8: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.ProviderFlow.ReturnURLBuilder = s.ProviderReturnURL
	s.CheckoutKey = bytes.Repeat([]byte{8}, 32)
	s.MerchantChannels = map[int64][]int64{1: {7, 8}, 2: {7, 8}}
	invoke := func(p compat.Params) map[string]any {
		p["pid"] = "2"
		p["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
		p["sign_type"] = "RSA"
		p["sign"], _ = compat.SignRSA(p, rsaKey)
		values := url.Values{}
		for k, v := range p {
			values.Set(k, v)
		}
		_, out := call(t, s.Handler(), "POST", "/api/pay/create", values)
		return out
	}
	for _, tt := range []struct{ typ, method, material, channel string }{{"wxpay", "app", "app", "7"}, {"alipay", "app", "app", "8"}, {"wxpay", "jsapi", "jsapi", "7"}, {"wxpay", "h5", "jump", "7"}} {
		p := compat.Params{"type": tt.typ, "method": tt.method, "channel_id": tt.channel, "out_trade_no": "product-" + tt.typ + tt.method, "name": "test", "money": "1.00", "clientip": "203.0.113.5"}
		if tt.method == "jsapi" {
			p["sub_openid"] = "openid"
			p["sub_appid"] = "wxapp"
			p["is_applet"] = "1"
		}
		out := invoke(p)
		if out["code"] != float64(0) || out["pay_type"] != tt.material {
			t.Fatal(tt, out)
		}
		material, ok := out["pay_info"].(string)
		if !ok || material == "" {
			t.Fatal("SDK data must remain a string", out)
		}
		if tt.material == "app" && strings.Contains(material, "/checkout/") {
			t.Fatal("APP SDK data replaced by checkout jump", out)
		}
		if tt.typ == "wxpay" && (tt.method == "app" || tt.method == "jsapi") {
			var sdk map[string]string
			if json.Unmarshal([]byte(material), &sdk) != nil {
				t.Fatal("invalid SDK JSON")
			}
			if tt.method == "app" && sdk["package"] != "Sign=WXPay" {
				t.Fatal(sdk)
			}
			if tt.method == "jsapi" && sdk["appId"] != "wxapp" {
				t.Fatal(sdk)
			}
		}
	}
	before := calls
	bad := invoke(compat.Params{"type": "wxpay", "method": "jsapi", "channel_id": "7", "out_trade_no": "wrong-app", "name": "test", "money": "1.00", "sub_openid": "openid", "sub_appid": "different-app"})
	if bad["code"] != float64(-1) || calls != before {
		t.Fatal("untrusted AppID reached provider", bad)
	}
	var count int
	if e := pool.QueryRow(context.Background(), "SELECT count(*) FROM orders WHERE out_trade_no='wrong-app'").Scan(&count); e != nil || count != 0 {
		t.Fatal("invalid app reserved order", count, e)
	}
	v1 := compat.Params{"pid": "1", "type": "wxpay", "method": "app", "channel_id": "7", "out_trade_no": "v1-app", "name": "test", "money": "1.00", "clientip": "203.0.113.5"}
	_, out := call(t, s.Handler(), "POST", "/mapi.php", signed(v1, "contract-secret"))
	if out["code"] != float64(1) || out["payurl"] == nil || out["pay_type"] != nil {
		t.Fatal("legacy material contract changed", out)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprint(out["payurl"]), nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "weixin://app/") {
		t.Fatal("legacy APP checkout cannot launch", w.Code, w.Body.String())
	}
}

func TestAlipayNativeCheckoutUsesCorrectClientAndNeverCredits(t *testing.T) {
	s, _, key := fixture(t)
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		r.ParseForm()
		if r.Form.Get("method") != "alipay.trade.precreate" || r.Form.Get("notify_url") == "" {
			t.Error("missing native callback")
		}
		var biz map[string]any
		json.Unmarshal([]byte(r.Form.Get("biz_content")), &biz)
		node, _ := json.Marshal(map[string]any{"code": "10000", "out_trade_no": biz["out_trade_no"], "qr_code": "https://qr.alipay.example.invalid/42"})
		sum := sha256.Sum256(node)
		signature, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
		if e != nil {
			t.Error(e)
		}
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_precreate_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(signature)})
	}))
	defer upstream.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"native"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{9: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{9}, 32)
	s.MerchantChannels = map[int64][]int64{2: {9}}
	p := compat.Params{"pid": "2", "type": "alipay", "method": "jump", "channel_id": "9", "out_trade_no": "ali-native", "name": "test", "money": "1.00", "sign_type": "RSA", "timestamp": strconv.FormatInt(time.Now().Unix(), 10)}
	p["sign"], _ = compat.SignRSA(p, key)
	values := url.Values{}
	for k, v := range p {
		values.Set(k, v)
	}
	_, out := call(t, s.Handler(), "POST", "/api/pay/create", values)
	if out["code"] != float64(0) || out["pay_type"] != "jump" {
		t.Fatal(out)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprint(out["pay_info"]), nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "支付宝") || strings.Contains(w.Body.String(), "请使用微信扫描") {
		t.Fatal("native checkout used wrong client", w.Code, w.Body.String())
	}
	b, e := s.Store.Balance(context.Background(), 2)
	if e != nil || b.Available != 0 || requests != 1 {
		t.Fatal("native material falsely credited", b, e, requests)
	}
}
