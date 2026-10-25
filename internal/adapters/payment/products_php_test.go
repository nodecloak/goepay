package payment

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestOriginalAPPProductsPHPInterop(t *testing.T) {
	if os.Getenv("GOEPAY_PHP_INTEROP") != "1" {
		t.Skip("GOEPAY_PHP_INTEROP=1 required")
	}
	key := testKey(t)
	dir := t.TempDir()
	for name, source := range map[string]string{"wx.php": "wechatpay-sdk/src/BaseService.php", "wx_payment.php": "wechatpay-sdk/src/PaymentService.php", "ali.php": "alipay-sdk/src/Aop/AopClient.php", "ali_request.php": "alipay-sdk/src/Aop/AlipayRequest.php"} {
		raw, e := os.ReadFile(filepath.Join("..", "..", "..", "epay_pro", "vendor", "cccyun", source))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, name), raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	script := `<?php
require '/work/wx.php';require '/work/wx_payment.php';require '/work/ali.php';require '/work/ali_request.php';
$in=json_decode(stream_get_contents(STDIN),true);
$wx=new WeChatPay\PaymentService(['appid'=>'app','mchid'=>'mch','apikey'=>'synthetic-app-key','sslcert_path'=>'','sslkey_path'=>'']);
$make=new ReflectionMethod($wx,'makeSign');$make->setAccessible(true);$app=new ReflectionMethod($wx,'getAppParameters');$app->setAccessible(true);
$ali=new Alipay\Aop\AopClient();$ali->appId='app';$ali->rsaPrivateKey=$in['private_key'];$ali->signType='RSA2';
$sign=new ReflectionMethod($ali,'generateSign');$sign->setAccessible(true);
$request=new Alipay\Aop\AlipayRequest();$request->setApiMethodName('alipay.trade.app.pay');$request->setNotifyUrl('https://pay.example.invalid/notify');$request->setBizContent(json_decode($in['ali']['biz_content'],true));
echo json_encode(['wx'=>$app->invoke($wx,'synthetic-prepay'),'wx_go_sign'=>$make->invoke($wx,$in['wx']),'ali'=>$ali->sdkExecute($request),'ali_go_valid'=>$sign->invoke($ali,$in['ali'],'RSA2')===$in['ali']['sign']]);`
	if e := os.WriteFile(filepath.Join(dir, "runner.php"), []byte(script), 0600); e != nil {
		t.Fatal(e)
	}
	wx, e := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: "synthetic-app-key", Methods: []string{"app"}})
	if e != nil {
		t.Fatal(e)
	}
	wxApp, e := wx.APPParams("synthetic-prepay", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	ali, e := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"app"}})
	if e != nil {
		t.Fatal(e)
	}
	result, e := ali.Create(context.Background(), CreateRequest{TradeNo: "42", Subject: "中文商品", Method: "app", Amount: 101, NotifyURL: "https://pay.example.invalid/notify"})
	if e != nil {
		t.Fatal(e)
	}
	query, e := url.ParseQuery(result.Material)
	if e != nil {
		t.Fatal(e)
	}
	aliParams := map[string]string{}
	for k, v := range query {
		aliParams[k] = v[0]
	}
	payload, _ := json.Marshal(map[string]any{"private_key": base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key)), "wx": wxApp, "ali": aliParams})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--network", "none", "-i", "--mount", "type=bind,src="+dir+",dst=/work,readonly", "php:8.2-cli", "php", "/work/runner.php")
	cmd.Stdin = bytes.NewReader(payload)
	raw, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("PHP SDK failed: %v %s", e, raw)
	}
	var actual struct {
		WX         map[string]string `json:"wx"`
		WXGoSign   string            `json:"wx_go_sign"`
		Ali        string            `json:"ali"`
		AliGoValid bool              `json:"ali_go_valid"`
	}
	if e = json.Unmarshal(raw, &actual); e != nil {
		t.Fatalf("invalid PHP result %v %s", e, raw)
	}
	if actual.WXGoSign != wxApp["sign"] || !VerifyWechatV2(actual.WX, "synthetic-app-key", "MD5", actual.WX["sign"]) || actual.WX["package"] != "Sign=WXPay" || actual.WX["prepayid"] != "synthetic-prepay" || !actual.AliGoValid {
		t.Fatal("APP Go/PHP signatures or fields differ")
	}
	values, e := url.ParseQuery(actual.Ali)
	if e != nil {
		t.Fatal(e)
	}
	phpParams := map[string]string{}
	for k, v := range values {
		phpParams[k] = v[0]
	}
	want, e := SignAlipayRSA2(phpParams, key)
	if e != nil || want != phpParams["sign"] || phpParams["method"] != "alipay.trade.app.pay" {
		t.Fatal("PHP SDK APP string not compatible with Go verifier", e)
	}
}
