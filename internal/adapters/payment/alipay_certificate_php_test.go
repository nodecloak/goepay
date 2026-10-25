package payment

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestOriginalAlipayCertificateSerialPHPInterop(t *testing.T) {
	if os.Getenv("GOEPAY_PHP_INTEROP") != "1" {
		t.Skip("GOEPAY_PHP_INTEROP=1 required")
	}
	cfg, _, platform, root := alipayCertificateFixture(t)
	// Keep the original trusted root and append an independent RSA root with
	// a large serial plus an EC root. The source SDK concatenates only the
	// selected RSA roots, in PEM order, without integer truncation.
	extra := &x509.Certificate{SerialNumber: new(big.Int).Lsh(big.NewInt(1), 120), Subject: pkix.Name{CommonName: "AdditionalRoot"}, NotBefore: root.NotBefore, NotAfter: root.NotAfter, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, e := x509.CreateCertificate(rand.Reader, extra, extra, &cfg.PrivateKey.PublicKey, cfg.PrivateKey)
	if e != nil {
		t.Fatal(e)
	}
	cfg.CertificateMode.RootPEM += string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	ec, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	ecRoot := *extra
	ecRoot.SerialNumber = big.NewInt(99)
	ecRoot.Subject = pkix.Name{CommonName: "ExcludedEC"}
	ecRoot.SignatureAlgorithm = x509.ECDSAWithSHA256
	der, e = x509.CreateCertificate(rand.Reader, &ecRoot, &ecRoot, &ec.PublicKey, ec)
	if e != nil {
		t.Fatal(e)
	}
	cfg.CertificateMode.RootPEM += string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	a, e := NewAlipay(cfg)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	helper, e := os.ReadFile(filepath.Join("..", "..", "..", "epay_pro", "vendor", "cccyun", "alipay-sdk", "src", "Aop", "AlipayCertHelper.php"))
	if e != nil {
		t.Fatal(e)
	}
	files := map[string][]byte{"helper.php": helper, "app.pem": []byte(cfg.CertificateMode.ApplicationPEM), "platform.pem": []byte(cfg.CertificateMode.PlatformPEM), "root.pem": []byte(cfg.CertificateMode.RootPEM), "run.php": []byte(`<?php require '/work/helper.php';echo json_encode(['app'=>Alipay\Aop\AlipayCertHelper::getCertSN('/work/app.pem'),'platform'=>Alipay\Aop\AlipayCertHelper::getCertSN('/work/platform.pem'),'root'=>Alipay\Aop\AlipayCertHelper::getRootCertSN('/work/root.pem'),'serial'=>openssl_x509_parse(file_get_contents('/work/app.pem'))['serialNumber']]);`)}
	aop, e := os.ReadFile(filepath.Join("..", "..", "..", "epay_pro", "vendor", "cccyun", "alipay-sdk", "src", "Aop", "AopClient.php"))
	if e != nil {
		t.Fatal(e)
	}
	files["aop.php"] = aop
	files["run.php"] = []byte(`<?php require '/work/helper.php';require '/work/aop.php';$in=json_decode(stream_get_contents(STDIN),true);$a=new Alipay\Aop\AopClient();$a->rsaPrivateKey=$in['private'];$method=new ReflectionMethod($a,'generateSign');$method->setAccessible(true);echo json_encode(['app'=>Alipay\Aop\AlipayCertHelper::getCertSN('/work/app.pem'),'platform'=>Alipay\Aop\AlipayCertHelper::getCertSN('/work/platform.pem'),'root'=>Alipay\Aop\AlipayCertHelper::getRootCertSN('/work/root.pem'),'serial'=>openssl_x509_parse(file_get_contents('/work/app.pem'))['serialNumber'],'valid'=>$method->invoke($a,$in['params'],'RSA2')===$in['params']['sign']?'1':'0']);`)
	for name, raw := range files {
		if e = os.WriteFile(filepath.Join(dir, name), raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a.config.Methods = []string{"app"}
	material, e := a.Create(ctx, CreateRequest{TradeNo: "42", Subject: "test", Amount: 100, Method: "app", NotifyURL: "https://pay.example.invalid/notify"})
	if e != nil {
		t.Fatal(e)
	}
	values, e := url.ParseQuery(material.Material)
	if e != nil {
		t.Fatal(e)
	}
	params := map[string]string{}
	for k, v := range values {
		params[k] = v[0]
	}
	payload, _ := json.Marshal(map[string]any{"private": base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(cfg.PrivateKey)), "params": params})
	command := exec.CommandContext(ctx, "docker", "run", "--rm", "--network", "none", "-i", "--mount", "type=bind,src="+dir+",dst=/work,readonly", "php:8.2-cli", "php", "/work/run.php")
	command.Stdin = bytes.NewReader(payload)
	raw, e := command.CombinedOutput()
	if e != nil {
		t.Fatalf("PHP certificate helper: %v %s", e, raw)
	}
	var result map[string]string
	if e = json.Unmarshal(raw, &result); e != nil {
		t.Fatalf("PHP result: %v %s", e, raw)
	}
	platformSN, _ := alipayCertSN(platform)
	rootSN := a.certificates.rootSN
	if result["app"] != a.certificates.appSN || result["platform"] != platformSN || result["root"] != rootSN || result["valid"] != "1" {
		t.Fatalf("Go/PHP certificate SN mismatch: Go %s/%s/%s PHP %v", a.certificates.appSN, platformSN, rootSN, result)
	}
}
