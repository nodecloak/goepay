package cryptopay

import (
	"context"
	"encoding/json"
	"goepay/internal/cryptochain"
	"goepay/internal/cryptosigner"
	"path/filepath"
	"testing"
)

type unusedKeys struct{}

func (unusedKeys) PublicKey(context.Context, string) ([]byte, error) { return nil, ErrUnavailable }
func (unusedKeys) SignDigest(context.Context, string, []byte) ([]byte, error) {
	return nil, ErrUnavailable
}
func TestUnsignedPreparedCancellationRequiresDurableSignerTombstone(t *testing.T) {
	s, w := fundedWithdrawal(t)
	p, e := reviewPreparedTransfer(cryptochain.TransferRequest{RequestID: w.ID, From: depositOne, To: depositTwo, AtomicAmount: "6000000", FeeLimit: 100000000})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(p)
	if _, e = s.Pool.Exec(t.Context(), "UPDATE crypto_withdrawals SET state='prepared',prepared=$2 WHERE id=$1", w.ID, raw); e != nil {
		t.Fatal(e)
	}
	v, e := s.CancelWithdrawal(t.Context(), 1, w.ID)
	if e != nil || !v.CancellationRequested {
		t.Fatal(v, e)
	}
	signer, e := cryptosigner.NewService(cryptosigner.Config{StatePath: filepath.Join(t.TempDir(), "journal"), Rules: []cryptosigner.Rule{{Chain: cryptochain.Tron, Network: "mainnet", ChainID: "728126428", From: depositOne, Contract: cryptochain.TronUSDTContract, KeyID: "unused", Destinations: []string{depositTwo}, MaxDailyTransactions: 100, MaxDailyFeeAtomic: "1000000000000000000", MaxAmount: "10000000", DailyLimit: "10000000", MaxFeeLimit: 100000000, MaxTTLSeconds: 300, TRONRPCURL: "http://127.0.0.1:1"}}}, unusedKeys{})
	if e != nil {
		t.Fatal(e)
	}
	defer signer.Close()
	c := &memoryChain{network: cryptochain.Tron}
	x := Executor{Service: s, Adapters: map[string]cryptochain.Adapter{"tron": c}, Signer: signer, FeeLimits: map[string]uint64{"tron": 100000000}}
	if e = x.Execute(t.Context(), w.ID); e != nil {
		t.Fatal(e)
	}
	v, _ = s.GetWithdrawal(t.Context(), w.ID)
	b, _ := s.Balances(t.Context(), 1)
	if v.State != "cancelled" || b[0].Available != "9.9" || b[0].Reserved != "0" || c.broadcasts != 0 {
		t.Fatal(v, b)
	}
	if _, e = signer.Sign(t.Context(), p); e == nil {
		t.Fatal("cancelled request could be signed")
	}
}
