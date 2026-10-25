package payment

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestOriginalScanSignaturePHPInterop(t *testing.T) {
	if os.Getenv("GOEPAY_PHP_INTEROP") != "1" {
		t.Skip("GOEPAY_PHP_INTEROP=1 required")
	}
	key := testKey(t)
	const apiKey = "0123456789abcdef0123456789abcdef"
	var wxParams, aliParams map[string]string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pay/micropay" {
			raw, _ := io.ReadAll(r.Body)
			wxParams, _ = decodeXML(raw)
			out := map[string]string{"return_code": "SUCCESS", "result_code": "FAIL", "err_code": "USERPAYING", "appid": "app", "mch_id": "mch"}
			out["sign"], _ = SignWechatV2(out, apiKey, "MD5")
			w.Write(encodeXML(out))
			return
		}
		r.ParseForm()
		aliParams = map[string]string{}
		for k, v := range r.PostForm {
			aliParams[k] = v[0]
		}
		node := []byte(`{"code":"10003"}`)
		sign, _ := signRSA(node, key)
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_pay_response": json.RawMessage(node), "sign": sign})
	}))
	defer upstream.Close()
	wx, e := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: apiKey, Methods: []string{"scan"}, BaseURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	ali, e := NewAlipay(AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"scan"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	request := ScanRequest{TradeNo: "42", Subject: "中文扫码商品", Amount: 101, ClientIP: "192.0.2.1", NotifyURL: "https://pay.example.invalid/notify", AuthCode: "120000000000000001"}
	wx.Scan(context.Background(), request)
	request.AuthCode = "280000000000000001"
	ali.Scan(context.Background(), request)
	if wxParams == nil || aliParams == nil {
		t.Fatal("scan request not captured")
	}
	dir := t.TempDir()
	files := map[string]string{"wx.php": "wechatpay-sdk/src/BaseService.php", "wx_payment.php": "wechatpay-sdk/src/PaymentService.php", "ali.php": "alipay-sdk/src/Aop/AopClient.php"}
	for name, source := range files {
		raw, e := os.ReadFile(filepath.Join("..", "..", "..", "epay_pro", "vendor", "cccyun", source))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, name), raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	script := `<?php require '/work/wx.php';require '/work/wx_payment.php';require '/work/ali.php';$in=json_decode(stream_get_contents(STDIN),true);$wx=new WeChatPay\PaymentService(['appid'=>'app','mchid'=>'mch','apikey'=>'0123456789abcdef0123456789abcdef','sslcert_path'=>'','sslkey_path'=>'']);$wm=new ReflectionMethod($wx,'makeSign');$wm->setAccessible(true);$ali=new Alipay\Aop\AopClient();$ali->rsaPrivateKey=$in['private'];$am=new ReflectionMethod($ali,'generateSign');$am->setAccessible(true);echo json_encode(['wx'=>$wm->invoke($wx,$in['wx']),'ali'=>$am->invoke($ali,$in['ali'],'RSA2')]);`
	if e = os.WriteFile(filepath.Join(dir, "run.php"), []byte(script), 0600); e != nil {
		t.Fatal(e)
	}
	payload, _ := json.Marshal(map[string]any{"wx": wxParams, "ali": aliParams, "private": base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key))})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "docker", "run", "--rm", "--network", "none", "-i", "--mount", "type=bind,src="+dir+",dst=/work,readonly", "php:8.2-cli", "php", "/work/run.php")
	command.Stdin = bytes.NewReader(payload)
	raw, e := command.CombinedOutput()
	if e != nil {
		t.Fatalf("scan PHP interop: %v %s", e, raw)
	}
	var result map[string]string
	if e = json.Unmarshal(raw, &result); e != nil || result["wx"] != wxParams["sign"] || result["ali"] != aliParams["sign"] {
		t.Fatal("original SDK scan signatures differ", e)
	}
}
