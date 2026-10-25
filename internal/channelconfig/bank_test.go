package channelconfig

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"math/big"
	"reflect"
	"testing"
	"time"
)

func TestFactoryBankRequiresExplicitFlagPublicKeyAndMTLS(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(42), Subject: pkix.Name{CommonName: "synthetic-mch"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	public, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	channel := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "app", "appmchid": "mch", "appkey": "0123456789abcdef0123456789abcdef", "enable_bank_transfer": "true", "bank_public_key": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}))}, Certificates: map[string]string{"sslcert_path": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), "sslkey_path": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))}}
	build := func() (map[int64]adapter.Provider, error) {
		return Build(Import{Version: 1, Channels: []Channel{channel}}, false)
	}
	clients, e := build()
	if e != nil {
		t.Fatal(e)
	}
	p := clients[7]
	if types := p.(adapter.TransferCapabilities).TransferTypes(); !reflect.DeepEqual(types, []string{"wxpay", "bank"}) {
		t.Fatal("explicit bank not advertised", types)
	}
	validator, ok := p.(adapter.TransferValidator)
	if !ok {
		t.Fatal("guard lost local transfer validation")
	}
	if e = validator.ValidateTransfer(adapter.TransferRequest{}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal("write guard bypass", e)
	}
	if _, e = p.(adapter.TransferProvider).Transfer(context.Background(), adapter.TransferRequest{}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal(e)
	}
	channel.Config["enable_bank_transfer"] = "false"
	clients, e = build()
	if e != nil {
		t.Fatal(e)
	}
	if len(clients[7].(adapter.TransferCapabilities).TransferTypes()) != 1 {
		t.Fatal("bank enabled implicitly by uploaded key")
	}
	channel.Config["enable_bank_transfer"] = "true"
	saved := channel.Config["bank_public_key"]
	delete(channel.Config, "bank_public_key")
	if _, e = build(); e == nil {
		t.Fatal("bank enabled without public key")
	}
	channel.Config["bank_public_key"] = saved
	delete(channel.Certificates, "sslcert_path")
	delete(channel.Certificates, "sslkey_path")
	if _, e = build(); e == nil {
		t.Fatal("bank enabled without mTLS")
	}
}
