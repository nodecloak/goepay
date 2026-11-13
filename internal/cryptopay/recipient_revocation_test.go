package cryptopay

import (
	"context"
	"goepay/internal/cryptochain"
	"testing"
)

func TestRevokedRecipientStopsUnsignedOutgoing(t *testing.T) {
	s, w := fundedWithdrawal(t)
	if _, e := s.Pool.Exec(t.Context(), "UPDATE crypto_recipients SET state='revoked' WHERE address=$1", depositTwo); e != nil {
		t.Fatal(e)
	}
	c := &memoryChain{network: cryptochain.Tron}
	signer := &memorySigner{}
	x := Executor{Service: s, Adapters: map[string]cryptochain.Adapter{"tron": c}, Signer: signer, FeeLimits: map[string]uint64{"tron": 100000000}}
	if e := x.Execute(context.Background(), w.ID); e == nil {
		t.Fatal("revoked recipient admitted")
	}
	if signer.calls != 0 || c.prepared != 0 || c.broadcasts != 0 {
		t.Fatal("revocation passed external boundary")
	}
}
