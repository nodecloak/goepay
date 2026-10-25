package channelconfig

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	provider "goepay/internal/adapters/payment"
	"math/big"
	"testing"
	"time"
)

func TestWechatV2Sharing79FactoryExplicitProductCertificateAndIndependentGuard(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(79), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	c := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "app79", "appmchid": "mch79", "appkey": "0123456789abcdef0123456789abcdef", "enable_scan_pay": "true"}, Certificates: map[string]string{"sslcert_path": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), "sslkey_path": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))}}
	build := func(live bool) (map[int64]provider.Provider, error) {
		return Build(Import{Version: 1, Channels: []Channel{c}}, live)
	}
	out, e := build(false)
	if e != nil || out[7].(provider.ChannelSharingProvider).ChannelSharingCapabilities().Product != "" {
		t.Fatal("V2 sharing not default off", e)
	}
	c.Config["enable_channel_sharing"] = "true"
	for _, live := range []bool{false, true} {
		out, e = build(live)
		if e != nil {
			t.Fatal("offline V2 sharing factory", e)
		}
		p := out[7].(provider.ChannelSharingProvider)
		caps := p.ChannelSharingCapabilities()
		if caps.Product != "wxpay_v2_single_direct" || !caps.ReturnSupported || !caps.FinishSupported || !caps.AutoFinishSupported {
			t.Fatal(caps)
		}
		if mode, e := out[7].(provider.ChannelSharingPaymentProfile).ChannelSharingRefundMode("scan"); e != nil || mode != "merchant_funded" {
			t.Fatal("V2 barcode funding profile", mode, e)
		}
		if !live {
			if e = p.BindChannelSharingReceiver(context.Background(), "BIND79", provider.SharingReceiver{}); !errors.Is(e, ErrLiveDisabled) {
				t.Fatal(e)
			}
			if _, e = p.SubmitChannelSharing(context.Background(), provider.ChannelSharingRequest{}); !errors.Is(e, ErrLiveDisabled) {
				t.Fatal(e)
			}
			if _, e = p.ReturnChannelSharing(context.Background(), provider.ChannelSharingReturnRequest{}); !errors.Is(e, ErrLiveDisabled) {
				t.Fatal(e)
			}
			if _, e = out[7].(provider.ChannelSharingFinisher).FinishChannelSharing(context.Background(), provider.ChannelSharingFinishRequest{}); !errors.Is(e, ErrLiveDisabled) {
				t.Fatal(e)
			}
		}
		if out[7].(provider.ScanCancellationProvider).CanCancelScan() {
			t.Fatal("sharing silently enabled independent barcode cancellation")
		}
	}
	delete(c.Certificates, "sslcert_path")
	delete(c.Certificates, "sslkey_path")
	if _, e = build(false); e == nil {
		t.Fatal("declared V2 sharing missing financial client certificate admitted")
	}
}
