package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goepay/internal/testphp"
)

func TestOriginalWechatBankOAEPInterop(t *testing.T) {
	testphp.Require(t)
	key := testKey(t)
	dir := t.TempDir()
	for name, source := range map[string]string{"base.php": "BaseService.php", "transfer.php": "TransferService.php"} {
		raw, e := os.ReadFile(testphp.Path(t, "epay_pro/vendor/cccyun/wechatpay-sdk/src"+"/"+source))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, name), raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	script := `<?php
require '/work/base.php';require '/work/transfer.php';$in=json_decode(stream_get_contents(STDIN),true);
$transfer=new WeChatPay\TransferService(['appid'=>'synthetic','mchid'=>'synthetic','apikey'=>'synthetic','sslcert_path'=>'','sslkey_path'=>'','publickey_path'=>'']);
$method=new ReflectionMethod($transfer,'encryptData');$method->setAccessible(true);$public=openssl_pkey_get_public($in['public_key']);
$cipher=$method->invoke($transfer,$in['plain'],$public);$valid=openssl_private_decrypt(base64_decode($in['go_cipher']),$plain,$in['private_key'],OPENSSL_PKCS1_OAEP_PADDING);
echo json_encode(['cipher'=>$cipher,'valid'=>$valid&&$plain===$in['plain']]);`
	if e := os.WriteFile(filepath.Join(dir, "runner.php"), []byte(script), 0600); e != nil {
		t.Fatal(e)
	}
	public, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	plain := "测试持卡人"
	cipher, e := rsa.EncryptOAEP(sha1.New(), rand.Reader, &key.PublicKey, []byte(plain), nil)
	if e != nil {
		t.Fatal(e)
	}
	payload, _ := json.Marshal(map[string]any{"public_key": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})), "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), "plain": plain, "go_cipher": base64.StdEncoding.EncodeToString(cipher)})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	raw, e := testphp.Run(t, ctx, testphp.Spec{Mounts: []testphp.Mount{{Source: dir, Target: "/work"}}, Args: []string{"/work/runner.php"}, Input: payload})
	if e != nil {
		t.Fatalf("PHP bank SDK failed %v %s", e, raw)
	}
	var actual struct {
		Cipher string `json:"cipher"`
		Valid  bool   `json:"valid"`
	}
	if e = json.Unmarshal(raw, &actual); e != nil {
		t.Fatalf("invalid PHP response %v %s", e, raw)
	}
	phpCipher, e := base64.StdEncoding.DecodeString(actual.Cipher)
	if e != nil {
		t.Fatal(e)
	}
	got, e := rsa.DecryptOAEP(sha1.New(), rand.Reader, key, phpCipher, nil)
	if e != nil || string(got) != plain || !actual.Valid {
		t.Fatal("original PHP OAEP bank interop mismatch", e)
	}
}
