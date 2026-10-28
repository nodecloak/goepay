package payment

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goepay/internal/testphp"
)

func TestOriginalAlipayOrderCodePHPInterop(t *testing.T) {
	testphp.Require(t)
	merchant, platform := testKey(t), testKey(t)
	var captured map[string]string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		captured = map[string]string{}
		for k, values := range r.Form {
			if len(values) != 1 {
				t.Error("duplicate signed parameter")
			}
			captured[k] = values[0]
		}
		node := `{"code":"10000","out_trade_no":"ORDER8","qr_code":"https://qr.alipay.com/synthetic"}`
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_precreate_response": json.RawMessage(node), "sign": independentSignature(t, node, platform)})
	}))
	defer upstream.Close()
	p, err := NewAlipay(AlipayConfig{AppID: "synthetic-app", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"native"}, NativeOrderCode: true, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Create(context.Background(), CreateRequest{TradeNo: "ORDER8", Subject: "原订单码 中文 + &", Amount: 101, Method: "native", ClientIP: "192.0.2.1", NotifyURL: "https://pay.example.invalid/notify"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, source := range map[string]string{"aop.php": "AopClient.php", "request.php": "AlipayRequest.php"} {
		raw, err := os.ReadFile(testphp.Path(t, "epay_pro/vendor/cccyun/alipay-sdk/src/Aop"+"/"+source))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	script := `<?php
require '/work/aop.php';require '/work/request.php';
$in=json_decode(stream_get_contents(STDIN),true);
$aop=new Alipay\Aop\AopClient();$aop->rsaPrivateKey=$in['private_key'];
$sign=new ReflectionMethod($aop,'generateSign');$sign->setAccessible(true);
$request=new Alipay\Aop\AlipayRequest();$request->setApiMethodName('alipay.trade.precreate');$request->setBizContent(json_decode($in['params']['biz_content'],true));
$biz=json_decode($request->getBizContent(),true);
echo json_encode(['valid'=>$sign->invoke($aop,$in['params'],'RSA2')===$in['params']['sign'],'method'=>$request->getApiMethodName(),'product'=>$biz['product_code'],'ip'=>$biz['business_params']['mc_create_trade_ip'],'amount'=>$biz['total_amount']]);`
	if err = os.WriteFile(filepath.Join(dir, "runner.php"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"private_key": base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(merchant)), "params": captured})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	raw, err := testphp.Run(t, ctx, testphp.Spec{Mounts: []testphp.Mount{{Source: dir, Target: "/work"}}, Args: []string{"/work/runner.php"}, Input: payload})
	if err != nil {
		t.Fatalf("original PHP AOP failed: %v %s", err, raw)
	}
	var result struct {
		Valid                       bool
		Method, Product, IP, Amount string
	}
	if err = json.Unmarshal(raw, &result); err != nil || !result.Valid || result.Method != "alipay.trade.precreate" || result.Product != "QR_CODE_OFFLINE" || result.IP != "192.0.2.1" || result.Amount != "1.01" {
		t.Fatalf("original PHP order-code mismatch: %+v %v %s", result, err, raw)
	}
}
