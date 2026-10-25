package payment

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestWechatAPPCompleteSecondSignature(t *testing.T) {
	key := "synthetic-key-01234567890123456789"
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		raw, _ := io.ReadAll(r.Body)
		p, e := decodeXML(raw)
		if e != nil || p["trade_type"] != "APP" || p["openid"] != "" || !VerifyWechatV2(p, key, "MD5", p["sign"]) {
			t.Error("wrong APP request", p, e)
		}
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app", "mch_id": "mch", "prepay_id": "synthetic-prepay", "trade_type": "APP"}
		out["sign"], _ = SignWechatV2(out, key, "MD5")
		w.Write(encodeXML(out))
	}))
	defer upstream.Close()
	p, e := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: key, Methods: []string{"app"}, BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	result, e := p.Create(context.Background(), CreateRequest{TradeNo: "42", Subject: "test", Method: "app", Amount: 100, ClientIP: "192.0.2.1", NotifyURL: "https://pay.example.invalid/notify"})
	if e != nil || result.MaterialType != "app" {
		t.Fatal(result, e)
	}
	var material map[string]string
	if e = json.Unmarshal([]byte(result.Material), &material); e != nil {
		t.Fatal(e)
	}
	if len(material) != 7 || material["appid"] != "app" || material["partnerid"] != "mch" || material["prepayid"] != "synthetic-prepay" || material["package"] != "Sign=WXPay" || material["timestamp"] == "" || material["noncestr"] == "" || !VerifyWechatV2(material, key, "MD5", material["sign"]) {
		t.Fatal("invalid APP material", material)
	}
	if requests != 1 {
		t.Fatal(requests)
	}
}
func TestWechatH5RequiresTrustedSceneAndPreservesSignedReturn(t *testing.T) {
	key := "synthetic-key-01234567890123456789"
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		raw, _ := io.ReadAll(r.Body)
		p, e := decodeXML(raw)
		var scene struct {
			H5 map[string]string `json:"h5_info"`
		}
		json.Unmarshal([]byte(p["scene_info"]), &scene)
		if e != nil || p["trade_type"] != "MWEB" || scene.H5["type"] != "Wap" || scene.H5["wap_url"] != "https://pay.example.invalid/" || scene.H5["wap_name"] != "测试收银台" || !VerifyWechatV2(p, key, "MD5", p["sign"]) {
			t.Error("wrong H5 request", p, e)
		}
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app", "mch_id": "mch", "mweb_url": "https://wx.tenpay.com/cgi-bin/mmpayweb-bin/checkmweb?prepay_id=synthetic"}
		out["sign"], _ = SignWechatV2(out, key, "MD5")
		w.Write(encodeXML(out))
	}))
	defer upstream.Close()
	config := WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: key, Methods: []string{"h5"}, BaseURL: upstream.URL, HTTPClient: upstream.Client()}
	if _, e := NewWechatV2(config); !errors.Is(e, ErrInvalid) {
		t.Fatal("missing trusted scene accepted", e)
	}
	config.H5SiteURL = "https://pay.example.invalid/"
	config.H5SiteName = "测试收银台"
	p, e := NewWechatV2(config)
	if e != nil {
		t.Fatal(e)
	}
	result, e := p.Create(context.Background(), CreateRequest{TradeNo: "42", Subject: "test", Method: "h5", Amount: 100, ClientIP: "192.0.2.1", NotifyURL: "https://pay.example.invalid/notify", ReturnURL: "https://pay.example.invalid/checkout/42/return?token=signed&expires=12"})
	if e != nil || result.MaterialType != "jump" {
		t.Fatal(result, e)
	}
	u, e := url.Parse(result.Material)
	if e != nil || u.Query().Get("prepay_id") != "synthetic" || u.Query().Get("redirect_url") != "https://pay.example.invalid/checkout/42/return?token=signed&expires=12" {
		t.Fatal(result, e)
	}
	if requests != 1 {
		t.Fatal(requests)
	}
}

func TestWechatH5RejectsUnsafeSceneConfigurationAndResponse(t *testing.T) {
	config := WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: "synthetic-key-01234567890123456789", Methods: []string{"h5"}, H5SiteName: "test"}
	for _, site := range []string{"http://pay.example.invalid/", "https://user@pay.example.invalid/", "https://pay.example.invalid/?query=1", "https://pay.example.invalid/#fragment"} {
		config.H5SiteURL = site
		if _, e := NewWechatV2(config); !errors.Is(e, ErrInvalid) {
			t.Fatal("unsafe H5 site enabled", site, e)
		}
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app", "mch_id": "mch", "mweb_url": "javascript:alert(1)"}
		p["sign"], _ = SignWechatV2(p, config.APIKey, "MD5")
		w.Write(encodeXML(p))
	}))
	defer upstream.Close()
	config.BaseURL = upstream.URL
	config.HTTPClient = upstream.Client()
	config.H5SiteURL = "https://pay.example.invalid/"
	p, e := NewWechatV2(config)
	if e != nil {
		t.Fatal(e)
	}
	_, e = p.Create(context.Background(), CreateRequest{TradeNo: "42", Subject: "test", Method: "h5", Amount: 100, ClientIP: "192.0.2.1", NotifyURL: "https://pay.example.invalid/notify"})
	var unknown *UnknownOutcome
	if !errors.As(e, &unknown) {
		t.Fatal("unusable H5 response lost unknown outcome", e)
	}
}
func TestAlipayAPPReturnsSignedSDKStringWithoutNetwork(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		t.Error("APP signing must not request upstream")
	}))
	defer upstream.Close()
	now := time.Unix(1800000000, 0)
	a, e := NewAlipay(AlipayConfig{AppID: "app", SellerID: "seller", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"app"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client(), Now: func() time.Time { return now }})
	if e != nil {
		t.Fatal(e)
	}
	expiry := now.Add(2 * time.Hour)
	result, e := a.Create(context.Background(), CreateRequest{TradeNo: "42", Subject: "商品 + & 中文", Method: "app", Amount: 101, ClientIP: "192.0.2.1", NotifyURL: "https://pay.example.invalid/notify", ExpiresAt: &expiry})
	if e != nil || result.MaterialType != "app" {
		t.Fatal(result, e)
	}
	values, e := url.ParseQuery(result.Material)
	if e != nil {
		t.Fatal(e)
	}
	p := map[string]string{}
	for k, v := range values {
		if len(v) != 1 {
			t.Fatal("duplicate SDK field")
		}
		p[k] = v[0]
	}
	if p["method"] != "alipay.trade.app.pay" || p["notify_url"] != "https://pay.example.invalid/notify" || !verifyRSA([]byte(AlipayRequestCanonical(p)), p["sign"], &merchant.PublicKey) {
		t.Fatal("invalid APP signature")
	}
	var biz map[string]any
	json.Unmarshal([]byte(p["biz_content"]), &biz)
	if biz["out_trade_no"] != "42" || biz["subject"] != "商品 + & 中文" || biz["total_amount"] != "1.01" || biz["product_code"] != "QUICK_MSECURITY_PAY" || biz["seller_id"] != "seller" || biz["time_expire"] != expiry.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04:05") {
		t.Fatal(biz)
	}
	if called || strings.Contains(result.Material, "<form") {
		t.Fatal("APP data incorrectly treated as webpage")
	}
}
