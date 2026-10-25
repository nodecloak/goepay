package payment

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	return k
}
func independentSignature(t *testing.T, body string, key *rsa.PrivateKey) string {
	t.Helper()
	d := sha256.Sum256([]byte(body))
	b, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, d[:])
	if e != nil {
		t.Fatal(e)
	}
	return base64.StdEncoding.EncodeToString(b)
}
func TestWechatV2CanonicalAndSign(t *testing.T) {
	p := map[string]string{"b": "0", "a": " 中文 ", "sign_type": "HMAC-SHA256", "sign": "ignored", "empty": ""}
	if got := WechatV2Canonical(p); got != "a= 中文 &b=0&sign_type=HMAC-SHA256" {
		t.Fatal(got)
	}
	if _, e := SignWechatV2(p, "secret", "RSA"); e == nil {
		t.Fatal("unknown algorithm accepted")
	}
	sig, e := SignWechatV2(p, "secret", "HMAC-SHA256")
	if e != nil {
		t.Fatal(e)
	}
	if !VerifyWechatV2(p, "secret", "HMAC-SHA256", sig) {
		t.Fatal("valid sign rejected")
	}
	if VerifyWechatV2(p, "secret", "HMAC-SHA256", strings.ToLower(sig)) {
		t.Fatal("accepted lowercase wire signature")
	}
}
func TestWechatV3RequestExactBytes(t *testing.T) {
	key := testKey(t)
	body := []byte("{\"description\": \"中文\"}")
	target := "/v3/pay/transactions/native?x=a%2Bb"
	want := "POST\n" + target + "\n1700000000\nnonce123\n" + string(body) + "\n"
	message, e := WechatV3RequestMessage("POST", target, "1700000000", "nonce123", body)
	if e != nil || string(message) != want {
		t.Fatalf("message %q %v", message, e)
	}
	sig, e := SignWechatV3Request("POST", target, "1700000000", "nonce123", body, key)
	if e != nil {
		t.Fatal(e)
	}
	if sig != independentSignature(t, want, key) {
		t.Fatal("wrong RSA wire signature")
	}
	if _, e := WechatV3RequestMessage("POST", "https://evil.invalid/path", "1700000000", "n", body); e == nil {
		t.Fatal("absolute URL accepted")
	}
	if _, e := WechatV3RequestMessage("POST", target, "1700000000", "bad\nnonce", body); e == nil {
		t.Fatal("newline nonce accepted")
	}
}
func TestWechatV3ResponseAuthentication(t *testing.T) {
	key := testKey(t)
	now := time.Unix(1700000000, 0)
	body := []byte("{\"id\":\"evt1\"}")
	headers := http.Header{}
	headers.Set("Wechatpay-Timestamp", "1700000000")
	headers.Set("Wechatpay-Nonce", "nonce")
	headers.Set("Wechatpay-Serial", "PUB_KEY_ID_TEST")
	headers.Set("Wechatpay-Signature", independentSignature(t, "1700000000\nnonce\n"+string(body)+"\n", key))
	trusted := map[string]*rsa.PublicKey{"PUB_KEY_ID_TEST": &key.PublicKey}
	if e := VerifyWechatV3Message(headers, body, trusted, now, 5*time.Minute); e != nil {
		t.Fatal(e)
	}
	if e := VerifyWechatV3Message(headers, append(body, ' '), trusted, now, 5*time.Minute); e == nil {
		t.Fatal("tampered raw body accepted")
	}
	if e := VerifyWechatV3Message(headers, body, trusted, now.Add(301*time.Second), 5*time.Minute); e == nil {
		t.Fatal("stale callback accepted")
	}
	headers.Set("Wechatpay-Serial", "UNKNOWN")
	if e := VerifyWechatV3Message(headers, body, trusted, now, 5*time.Minute); e == nil {
		t.Fatal("untrusted serial accepted")
	}
	headers.Set("Wechatpay-Serial", "PUB_KEY_ID_TEST")
	headers.Add("Wechatpay-Nonce", "duplicate")
	if e := VerifyWechatV3Message(headers, body, trusted, now, 5*time.Minute); e == nil {
		t.Fatal("duplicate signature header accepted")
	}
}
func TestWechatAES256GCMKnownVector(t *testing.T) {
	ciphertext, _ := hex.DecodeString("cea7403d4d606b6e074ec5d3baf39d18d0d1c8a799996bf0265b98b5d48ab919")
	resource := EncryptedResource{Algorithm: "AEAD_AES_256_GCM", Nonce: string(make([]byte, 12)), Ciphertext: base64.StdEncoding.EncodeToString(ciphertext)}
	plain, e := DecryptWechatResource(make([]byte, 32), resource)
	if e != nil || string(plain) != string(make([]byte, 16)) {
		t.Fatalf("AES vector %x %v", plain, e)
	}
	resource.AssociatedData = "tampered"
	if _, e := DecryptWechatResource(make([]byte, 32), resource); e == nil {
		t.Fatal("tampered associated data accepted")
	}
	resource.Nonce = "short"
	if _, e := DecryptWechatResource(make([]byte, 32), resource); e == nil {
		t.Fatal("bad nonce accepted")
	}
}
func TestAlipayRSA2DifferentSigningScopes(t *testing.T) {
	key := testKey(t)
	p := map[string]string{"sign_type": "RSA2", "app_id": "app1", "biz_content": "{\"amount\":\"1.00\"}", "empty": "", "zero": "0"}
	request := AlipayRequestCanonical(p)
	if request != "app_id=app1&biz_content={\"amount\":\"1.00\"}&sign_type=RSA2&zero=0" {
		t.Fatal(request)
	}
	sig, e := SignAlipayRSA2(p, key)
	if e != nil || sig != independentSignature(t, request, key) {
		t.Fatal("Alipay request signature mismatch")
	}
	p["sign"] = independentSignature(t, "app_id=app1&biz_content={\"amount\":\"1.00\"}&zero=0", key)
	if !VerifyAlipayNotification(p, &key.PublicKey) {
		t.Fatal("notification rejected")
	}
	p["sign_type"] = "RSA"
	if VerifyAlipayNotification(p, &key.PublicKey) {
		t.Fatal("Alipay RSA2 downgraded")
	}
}
