package cryptopay

import (
	"context"
	"fmt"
	"testing"

	"goepay/internal/cryptochain"
)

type unavailableDispatchChain struct {
	memoryChain
	seen map[string]bool
}

func (a *unavailableDispatchChain) Prepare(_ context.Context, in cryptochain.TransferRequest) (cryptochain.PreparedTransaction, error) {
	a.seen[in.RequestID] = true
	return cryptochain.PreparedTransaction{}, ErrUnavailable
}

func TestPendingQueueRotatesPastPersistentFailures(t *testing.T) {
	s, _ := fundedWithdrawal(t)
	for i := 0; i < 100; i++ {
		v, err := s.RequestWithdrawal(t.Context(), WithdrawalInput{MerchantID: 1, RequestID: fmt.Sprintf("queued-%d", i), Network: "tron", Address: depositTwo, Amount: "0.000001"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.ApproveWithdrawal(t.Context(), v.ID, 9); err != nil {
			t.Fatal(err)
		}
	}
	a := &unavailableDispatchChain{memoryChain: memoryChain{network: cryptochain.Tron}, seen: map[string]bool{}}
	x := Executor{Service: s, Adapters: map[string]cryptochain.Adapter{"tron": a}, Signer: &memorySigner{}, FeeLimits: map[string]uint64{"tron": 100000000}}
	if err := x.RunPendingFor(t.Context(), "tron"); err == nil || len(a.seen) != 100 {
		t.Fatal("first bounded batch must preserve failed intents", len(a.seen), err)
	}
	// A new executor must recover the durable rotation, rather than starting
	// again at the oldest hundred continuously failing requests after restart.
	restarted := x
	if err := restarted.RunPendingFor(t.Context(), "tron"); err == nil || len(a.seen) != 101 {
		t.Fatal("older failures starved the remaining original request", len(a.seen), err)
	}
	b, err := s.Balances(t.Context(), 1)
	if err != nil || len(b) != 1 || b[0].Reserved != "6.0001" || b[0].Available != "3.8999" {
		t.Fatal("dispatch rotation changed reservations", b, err)
	}
	var changed int
	err = s.Pool.QueryRow(t.Context(), "SELECT count(*) FROM crypto_withdrawals WHERE state<>'preparing' OR signed IS NOT NULL OR tx_id<>''").Scan(&changed)
	if err != nil || changed != 0 {
		t.Fatal("failed dispatch created a signature or replacement", changed, err)
	}
	r, err := s.Reconcile(t.Context())
	if err != nil || r.ImbalancedJournals != 0 || r.BalanceMismatches != 0 || r.NegativeBalances != 0 {
		t.Fatal(r, err)
	}
}
