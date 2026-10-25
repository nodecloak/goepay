package channelconfig

import (
	provider "goepay/internal/adapters/payment"
	"testing"
)

func TestGuardedRealChannelPreservesReceivingAndPayerAccountIdentity(t *testing.T) {
	build := func(mch, key string) provider.Provider {
		t.Helper()
		out, e := Build(Import{Version: 1, Channels: []Channel{{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "same-app", "appmchid": mch, "appkey": key}}}}, false)
		if e != nil {
			t.Fatal(e)
		}
		return out[7]
	}
	original := build("original-mch", "0123456789abcdef0123456789abcdef")
	changed := build("changed-mch", "0123456789abcdef0123456789abcdef")
	rotated := build("original-mch", "abcdef0123456789abcdef0123456789")
	if _, ok := original.(provider.TransferAccountIdentity); !ok {
		t.Fatal("live safety wrapper erased the actual account identity")
	}
	a, e := provider.TransferAccountScope(original)
	if e != nil {
		t.Fatal(e)
	}
	b, e := provider.TransferAccountScope(changed)
	if e != nil {
		t.Fatal(e)
	}
	c, e := provider.TransferAccountScope(rotated)
	if e != nil {
		t.Fatal(e)
	}
	if a == b {
		t.Fatal("same AppID with a changed merchant inherited the original account scope")
	}
	if a != c {
		t.Fatal("key rotation changed the merchant identity")
	}
}
