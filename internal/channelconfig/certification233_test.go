package channelconfig

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestKYC233ChannelSnapshotPreservesExactAccountAndCopiesSecrets(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	c := Channel{ID: 7, Plugin: "alipay", AppTypes: []string{"1"}, Config: map[string]string{
		"appid": "2021000000000001", "appmchid": "2088000000000001",
		"appsecret": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})),
		"appkey":    string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)})),
	}}
	f, err := NewParentChannelFactory(c, ParentBuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	current := f.Bootstrap()
	current.Config["appid"] = "2021000000000002"
	_, raw, err := f.BuildSnapshot(99, current)
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.CertificationSnapshot233(99, raw)
	if err != nil || out.ID != 99 || out.Config["appid"] != "2021000000000002" {
		t.Fatal("current account replaced by bootstrap", err)
	}
	out.Config["appsecret"] = "mutated"
	again, err := f.CertificationSnapshot233(99, raw)
	if err != nil || again.Config["appsecret"] != c.Config["appsecret"] {
		t.Fatal("snapshot aliased mutable map", err)
	}
	if _, err = f.CertificationSnapshot233(7, raw); err == nil {
		t.Fatal("cross-channel snapshot accepted")
	}
	broken := append([]byte(nil), raw...)
	broken[len(broken)-1] = '!'
	if _, err = f.CertificationSnapshot233(99, broken); err == nil {
		t.Fatal("malformed account accepted")
	}
}

func TestKYC233ChannelSnapshotRejectsOtherProviders(t *testing.T) {
	c := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "wx-test", "appmchid": "test", "appkey": "0123456789abcdef0123456789abcdef"}}
	f, err := NewParentChannelFactory(c, ParentBuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := f.BootstrapSnapshot(7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.CertificationSnapshot233(7, raw); err == nil {
		t.Fatal("non-Alipay credentials used for certification")
	}
}
