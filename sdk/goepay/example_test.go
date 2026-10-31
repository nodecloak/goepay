package goepay

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/url"
)

func ExampleClient_SubmitURL() {
	// Disposable keys make this example self-contained and completely offline.
	// Your application must load its merchant private key and the real platform
	// public key from protected files instead; never generate new keys per order.
	merchant, _ := rsa.GenerateKey(rand.Reader, 2048)
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	public, _ := x509.MarshalPKIXPublicKey(&platform.PublicKey)
	c, err := New(Config{BaseURL: "https://pay.example.com", MerchantID: 1001,
		MerchantPrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(merchant)}),
		PlatformPublicKeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})})
	if err != nil {
		panic(err)
	}
	defer c.CloseIdleConnections()
	target, err := c.SubmitURL(url.Values{"out_trade_no": {"offline-example-217"}, "name": {"示例商品"}, "money": {"1.00"}, "type": {"alipay"}, "notify_url": {"https://merchant.example.com/payments/notify"}})
	if err != nil {
		panic(err)
	}
	u, _ := url.Parse(target)
	// Only print the path: do not log full signed URLs or send this example.
	fmt.Println(u.Path)
	// Output: /api/pay/submit
}
