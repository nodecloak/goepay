package server

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"goepay/internal/compat"
	"goepay/internal/testphp"
	"testing"
	"time"
)

func TestGenerateMerchantRSAFixedPHPCompatibleFormats(t *testing.T) {
	public, private, e := generateMerchantRSA()
	if e != nil {
		t.Fatal(e)
	}
	b, rest := pem.Decode([]byte(public))
	if b == nil || b.Type != "PUBLIC KEY" || len(rest) != 0 {
		t.Fatal("public PKIX PEM")
	}
	if _, e = x509.ParsePKIXPublicKey(b.Bytes); e != nil {
		t.Fatal(e)
	}
	b, rest = pem.Decode([]byte(private))
	if b == nil || b.Type != "PRIVATE KEY" || len(rest) != 0 {
		t.Fatal("private PKCS8 PEM")
	}
	if _, e = x509.ParsePKCS8PrivateKey(b.Bytes); e != nil {
		t.Fatal(e)
	}
	key, e := compat.ParsePrivateKey(private)
	if e != nil || key.N.BitLen() != 2048 {
		t.Fatal("fixed 2048 bit key")
	}
	pub, e := compat.ParsePublicKey(public)
	if e != nil {
		t.Fatal(e)
	}
	params := compat.Params{"pid": "1", "name": "商户 RSA", "money": "1.00", "sign_type": "RSA"}
	signature, e := compat.SignRSAWithProfile(params, key, compat.ProfileSDKV2)
	if e != nil || !compat.VerifyRSAWithProfile(params, signature, pub, compat.ProfileSDKV2) {
		t.Fatal("real signature invalid")
	}
}

func TestGenerateMerchantRSAPHPInterop(t *testing.T) {
	testphp.Require(t)
	public, private, e := generateMerchantRSA()
	if e != nil {
		t.Fatal(e)
	}
	key, _ := compat.ParsePrivateKey(private)
	params := compat.Params{"pid": "1", "name": "商户 RSA", "timestamp": "123456", "money": "1.00"}
	signature, e := compat.SignRSAWithProfile(params, key, compat.ProfileSDKV2)
	if e != nil {
		t.Fatal(e)
	}
	input, _ := json.Marshal(map[string]string{"public": public, "private": private, "canonical": compat.CanonicalSDK(params), "signature": signature})
	script := `$v=json_decode(stream_get_contents(STDIN),true,512,JSON_THROW_ON_ERROR);$priv=openssl_pkey_get_private($v['private']);$pub=openssl_pkey_get_public($v['public']);if(!$priv||!$pub||openssl_verify($v['canonical'],base64_decode($v['signature'],true),$pub,OPENSSL_ALGO_SHA256)!==1)exit(2);if(!openssl_sign($v['canonical'],$sig,$priv,OPENSSL_ALGO_SHA256))exit(3);echo base64_encode($sig);`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, e := testphp.Run(t, ctx, testphp.Spec{Mounts: nil, Args: []string{"-r", script}, Input: input})
	if e != nil {
		t.Fatal("local PHP interoperability failed")
	}
	pub, _ := compat.ParsePublicKey(public)
	if !compat.VerifyRSAWithProfile(params, string(output), pub, compat.ProfileSDKV2) {
		t.Fatal("PHP generated signature rejected")
	}
}
