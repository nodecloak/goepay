package channelconfig

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	provider "goepay/internal/adapters/payment"
	"testing"
)

func TestWechatV3ExplicitFactoryOfflineAndGuarded(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	public, _ := x509.MarshalPKIXPublicKey(&platform.PublicKey)
	c := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1", "2"}, Config: map[string]string{"wechat_api_version": "v3", "appid": "local-app", "appmchid": "local-mch", "v3_merchant_serial": "MERCHANT_SERIAL", "v3_api_key": "0123456789abcdef0123456789abcdef"}, Certificates: map[string]string{"v3_merchant_key": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), "v3_verifier_PUB_KEY_ID_LOCAL": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}))}}
	out, e := Build(Import{Version: 1, Channels: []Channel{c}}, false)
	if e != nil {
		t.Fatal(e)
	}
	p := out[7]
	if p.(interface{ NotificationFormat() string }).NotificationFormat() != "wechat-v3" || p.(provider.TransferAccountIdentity).TransferAccountIdentity() != "local-mch" {
		t.Fatal("guard lost callback/account identity")
	}
	if _, e = p.Create(context.Background(), provider.CreateRequest{}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal("V3 create not gated", e)
	}
	if _, e = p.Refund(context.Background(), provider.RefundRequest{}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal("V3 refund not gated", e)
	}
	if len(p.(provider.TransferCapabilities).TransferTypes()) != 0 {
		t.Fatal("V3 advertised unsupported outgoing transfers")
	}
	c.Config["enable_channel_sharing"] = "true"
	out, e = Build(Import{Version: 1, Channels: []Channel{c}}, false)
	if e != nil {
		t.Fatal(e)
	}
	sharing, ok := out[7].(provider.ChannelSharingProvider)
	if !ok {
		t.Fatal("guard erased channel sharing interface")
	}
	if sharing.ChannelSharingCapabilities().Product != "wxpay_v3_direct" {
		t.Fatal("explicit sharing configuration was ignored")
	}
	if e = sharing.BindChannelSharingReceiver(context.Background(), "BIND_1", provider.SharingReceiver{}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal("receiver bind escaped write gate", e)
	}
	if _, e = sharing.SubmitChannelSharing(context.Background(), provider.ChannelSharingRequest{}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal("sharing escaped write gate", e)
	}
	if _, e = sharing.ReturnChannelSharing(context.Background(), provider.ChannelSharingReturnRequest{}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal("return escaped write gate", e)
	}
	for _, field := range []string{"enable_scan_pay", "enable_bank_transfer", "enable_mini_launch"} {
		c.Config[field] = "true"
		if _, e = Build(Import{Version: 1, Channels: []Channel{c}}, false); e == nil {
			t.Fatal("unsupported V3 capability accepted", field)
		}
		delete(c.Config, field)
	}
	delete(c.Certificates, "v3_verifier_PUB_KEY_ID_LOCAL")
	if _, e = Build(Import{Version: 1, Channels: []Channel{c}}, false); e == nil {
		t.Fatal("V3 accepted without a configured verifier")
	}
}
