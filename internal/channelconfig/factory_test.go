package channelconfig

import (
	"context"
	"errors"
	provider "goepay/internal/adapters/payment"
	"testing"
)

func TestImportedWechatCannotWriteWithoutExplicitLiveGate(t *testing.T) {
	imp := Import{Version: 1, Channels: []Channel{{ID: 2, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "test-app", "appmchid": "test-mch", "appkey": "0123456789abcdef0123456789abcdef"}}}}
	clients, e := Build(imp, false)
	if e != nil {
		t.Fatal(e)
	}
	p := clients[2]
	if _, e = p.Create(context.Background(), provider.CreateRequest{}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatalf("unguarded create: %v", e)
	}
	if e = p.Close(context.Background(), provider.QueryRequest{}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatalf("unguarded close: %v", e)
	}
	if _, e = p.Refund(context.Background(), provider.RefundRequest{}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatalf("unguarded refund: %v", e)
	}
	if p.Capabilities().Refund {
		t.Fatal("unconfigured mTLS refund advertised")
	}
	tx, ok := p.(provider.TransferProvider)
	if !ok {
		t.Fatal("guard lost transfer interface")
	}
	if _, e = tx.Transfer(context.Background(), provider.TransferRequest{}); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal("unguarded transfer", e)
	}
	if types, ok := p.(provider.TransferCapabilities); !ok || len(types.TransferTypes()) != 0 {
		t.Fatal("mTLS missing but transfer advertised")
	}
	proof, ok := p.(provider.TransferProofProvider)
	if !ok {
		t.Fatal("guard lost optional proof methods")
	}
	if _, e = proof.ApplyTransferProof(context.Background(), "test"); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal("proof apply unguarded", e)
	}
}
func TestFactoryRejectsUnknownProductsAndMissingKey(t *testing.T) {
	for _, c := range []Channel{{ID: 1, Plugin: "alipay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "x"}}, {ID: 2, Plugin: "wxpay", AppTypes: []string{"999"}, Config: map[string]string{"appid": "x", "appmchid": "m", "appkey": "0123456789abcdef0123456789abcdef"}}} {
		if _, e := Build(Import{Version: 1, Channels: []Channel{c}}, false); e == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}
