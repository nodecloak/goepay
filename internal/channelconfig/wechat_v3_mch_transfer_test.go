package channelconfig

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	provider "goepay/internal/adapters/payment"
	"sync/atomic"
	"testing"
)

func TestWechatV3MerchantTransferFollowsSiteSwitchThroughGuard(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	public, _ := x509.MarshalPKIXPublicKey(&platform.PublicKey)
	c := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"wechat_api_version": "v3", "appid": "local-app", "appmchid": "local-mch", "v3_merchant_serial": "MERCHANT_SERIAL", "v3_api_key": "0123456789abcdef0123456789abcdef"}, Certificates: map[string]string{"v3_merchant_key": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), "v3_verifier_PUB_KEY_ID_LOCAL": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}))}}
	var on atomic.Bool
	out, e := BuildWithOptions(Import{Version: 1, Channels: []Channel{c}}, BuildOptions{WechatMerchantTransfer: on.Load})
	if e != nil {
		t.Fatal(e)
	}
	p := out[7]
	mt, ok := p.(provider.MerchantTransferProduct)
	if !ok || !mt.UsesMerchantTransfer() {
		t.Fatal("guard erased the merchant-transfer product")
	}
	if len(p.(provider.TransferCapabilities).TransferTypes()) != 0 {
		t.Fatal("switched-off product offered transfers")
	}
	on.Store(true)
	if types := p.(provider.TransferCapabilities).TransferTypes(); len(types) != 1 || types[0] != "wxpay" {
		t.Fatal("site switch not followed", types)
	}
	if mch, app, pkg := mt.MerchantTransferConfirmation("pkg=="); mch != "local-mch" || app != "local-app" || pkg != "pkg==" {
		t.Fatal("confirmation parameters lost through guard")
	}
	if p.(provider.TransferProductIdentity).TransferProductIdentity() != "wechat.v3.mch-transfer.transfer-bills" {
		t.Fatal("product identity lost")
	}
	if _, e = p.(provider.TransferProvider).Transfer(context.Background(), provider.TransferRequest{}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal("merchant transfer escaped write gate", e)
	}
	plain, e := Build(Import{Version: 1, Channels: []Channel{c}}, false)
	if e != nil {
		t.Fatal(e)
	}
	if mt, ok := plain[7].(provider.MerchantTransferProduct); !ok || mt.UsesMerchantTransfer() || len(plain[7].(provider.TransferCapabilities).TransferTypes()) != 0 {
		t.Fatal("V3 channel without the site switch carries merchant transfers")
	}
}
