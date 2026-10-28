package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goepay/internal/testphp"
)

func TestOriginalProviderPHPInterop(t *testing.T) {
	testphp.Require(t)
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	der, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	for name, source := range map[string]string{"wx.php": "wechatpay-sdk/src/BaseService.php", "ali.php": "alipay-sdk/src/Aop/AopClient.php"} {
		b, e := os.ReadFile(testphp.Path(t, "epay_pro/vendor/cccyun"+"/"+source))
		if e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.WriteFile(filepath.Join(dir, "runner.php"), []byte(providerPHPRunner), 0600); e != nil {
		t.Fatal(e)
	}
	params := map[string]string{"appid": "app", "money": "1.00", "zero": "0", "space": " \t", "name": " 中文😀+% ", "sign_type": "RSA2"}
	ali, e := SignAlipayRSA2(params, key)
	if e != nil {
		t.Fatal(e)
	}
	callback := params
	callback["sign"] = independentSignature(t, AlipayNotificationCanonical(callback), key)
	md5, e := SignWechatV2(params, "test-key", "MD5")
	if e != nil {
		t.Fatal(e)
	}
	hmac, e := SignWechatV2(params, "test-key", "HMAC-SHA256")
	if e != nil {
		t.Fatal(e)
	}
	payload, _ := json.Marshal(map[string]any{"params": params, "private_key": base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key)), "public_key": base64.StdEncoding.EncodeToString(der)})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, e := testphp.Run(t, ctx, testphp.Spec{Mounts: []testphp.Mount{{Source: dir, Target: "/work"}}, Args: []string{"/work/runner.php"}, Input: payload})
	if e != nil {
		t.Fatalf("PHP error %v %s", e, out)
	}
	var result struct {
		MD5, HMAC, Alipay string
		Valid             bool
	}
	if e := json.Unmarshal(out, &result); e != nil {
		t.Fatalf("%v %s", e, out)
	}
	if result.MD5 != md5 || result.HMAC != hmac || result.Alipay != ali || !result.Valid {
		t.Fatalf("interop MD5=%v HMAC=%v Alipay=%v Callback=%v", result.MD5 == md5, result.HMAC == hmac, result.Alipay == ali, result.Valid)
	}
}

const providerPHPRunner = `<?php
require '/work/wx.php';require '/work/ali.php';
$in=json_decode(stream_get_contents(STDIN),true);
$wx=new WeChatPay\BaseService(['appid'=>'app','mchid'=>'mch','apikey'=>'test-key','sslcert_path'=>'','sslkey_path'=>'']);
$type=new ReflectionProperty($wx,'signType');$type->setAccessible(true);
$method=new ReflectionMethod($wx,'makeSign');$method->setAccessible(true);
$type->setValue($wx,'MD5');$md5=$method->invoke($wx,$in['params']);
$type->setValue($wx,'HMAC-SHA256');$hmac=$method->invoke($wx,$in['params']);
$ali=new Alipay\Aop\AopClient();$ali->rsaPrivateKey=$in['private_key'];$ali->rsaPublicKey=$in['public_key'];
$sign=new ReflectionMethod($ali,'generateSign');$sign->setAccessible(true);
echo json_encode(['MD5'=>$md5,'HMAC'=>$hmac,'Alipay'=>$sign->invoke($ali,$in['params'],'RSA2'),'Valid'=>$ali->verify($in['params'])]);
`
