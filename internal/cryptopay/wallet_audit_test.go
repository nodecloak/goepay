package cryptopay

import (
	"goepay/internal/cryptochain"
	"testing"
	"time"
)

func TestWalletAuditDoesNotHideDisagreementOrInventBalancingEntries(t *testing.T) {
	s := testService(t)
	v := invoice(t, s, "walletaudit")
	if e := s.RecordDeposit(t.Context(), Deposit{Network: "tron", TxID: "auditfund", BlockHeight: 1, BlockHash: "b", Address: v.Address, Amount: "10", OccurredAt: v.ExpiresAt.Add(-time.Minute)}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Pool.Exec(t.Context(), "INSERT INTO crypto_scan_cursors VALUES('tron',1,'b')"); e != nil {
		t.Fatal(e)
	}
	a := &scanChain{memoryChain: memoryChain{network: cryptochain.Tron, balance: "10000000"}}
	b := *a
	x := Executor{Service: s, Adapters: map[string]cryptochain.Adapter{"tron": a}, Witness: map[string]cryptochain.Adapter{"tron": &b}}
	r, e := x.AuditWallets(t.Context(), "tron")
	if e != nil || !r.Stable || r.DifferenceAtomic != "0" {
		t.Fatal(r, e)
	}
	b.balance = "11000000"
	if _, e = x.AuditWallets(t.Context(), "tron"); e == nil {
		t.Fatal("conflicting node balances accepted")
	}
	a.balance = "11000000"
	r, e = x.AuditWallets(t.Context(), "tron")
	if e != nil || r.DifferenceAtomic != "1000000" {
		t.Fatal(r, e)
	}
	balances, _ := s.Balances(t.Context(), 1)
	if balances[0].Available != "9.9" {
		t.Fatal("audit changed ledger", balances)
	}
}
