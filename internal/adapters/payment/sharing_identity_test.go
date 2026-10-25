package payment

import (
	"errors"
	"testing"
)

func TestAlipaySharingRequiresCanonicalSellerAndAccountScopeIncludesIt(t *testing.T) {
	key := testKey(t)
	if _, e := NewAlipay(AlipayConfig{AppID: "same-app", PrivateKey: key, PublicKey: &key.PublicKey, EnableChannelSharing: true}); !errors.Is(e, ErrInvalid) {
		t.Fatal("sharing had no stable upstream relationship owner", e)
	}
	build := func(seller string) *Alipay {
		t.Helper()
		p, e := NewAlipay(AlipayConfig{AppID: "same-app", SellerID: seller, PrivateKey: key, PublicKey: &key.PublicKey})
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	a, e := TransferAccountScope(build("2088000000000001"))
	if e != nil {
		t.Fatal(e)
	}
	b, e := TransferAccountScope(build("2088000000000002"))
	if e != nil {
		t.Fatal(e)
	}
	if a == b {
		t.Fatal("same application changed seller inherited an original payment/payer scope")
	}
}
