package cryptopay

import (
	"goepay/internal/cryptochain"
	"testing"
	"time"
)

func TestSweepQueueTracksNewFundsAndNeverLosesLaterAddresses(t *testing.T) {
	s := testService(t)
	v := invoice(t, s, "sweepqueue")
	cfg := s.Networks["tron"]
	cfg.TreasuryAddress = depositTwo
	s.Networks["tron"] = cfg
	c := &memoryChain{network: cryptochain.Tron, balance: "10000000"}
	x := Executor{Service: s, Adapters: map[string]cryptochain.Adapter{"tron": c}}
	if e := s.RecordDeposit(t.Context(), Deposit{Network: "tron", TxID: "queuefund", BlockHeight: 1, BlockHash: "b", Address: v.Address, Amount: "10", OccurredAt: v.ExpiresAt.Add(-time.Minute)}); e != nil {
		t.Fatal(e)
	}
	if e := x.RunSweeps(t.Context(), "tron"); e != nil {
		t.Fatal(e)
	}
	rows, _ := s.ListWithdrawals(t.Context(), 1)
	if len(rows) != 1 || rows[0].Kind != "sweep" {
		t.Fatal(rows)
	}
	var revision int
	var idle bool
	if e := s.Pool.QueryRow(t.Context(), "SELECT revision,next_attempt='infinity'::timestamptz FROM crypto_sweep_queue WHERE address=$1", v.Address).Scan(&revision, &idle); e != nil || revision != 1 || !idle {
		t.Fatal(revision, idle, e)
	}
	if e := s.RecordDeposit(t.Context(), Deposit{Network: "tron", TxID: "queue-more", BlockHeight: 2, BlockHash: "b2", Address: v.Address, Amount: "1", OccurredAt: time.Now().Add(time.Minute)}); e != nil {
		t.Fatal(e)
	}
	if e := s.Pool.QueryRow(t.Context(), "SELECT revision,next_attempt='infinity'::timestamptz FROM crypto_sweep_queue WHERE address=$1", v.Address).Scan(&revision, &idle); e != nil || revision != 2 || idle {
		t.Fatal(revision, idle, e)
	}
}
func TestFinalFailedSweepRequeuesWithoutReleasingMerchantLiabilities(t *testing.T) {
	s := testService(t)
	v := invoice(t, s, "failedsweep")
	cfg := s.Networks["tron"]
	cfg.TreasuryAddress = depositTwo
	s.Networks["tron"] = cfg
	if e := s.RecordDeposit(t.Context(), Deposit{Network: "tron", TxID: "sweep-fund", BlockHeight: 1, BlockHash: "b", Address: v.Address, Amount: "10", OccurredAt: v.ExpiresAt.Add(-time.Minute)}); e != nil {
		t.Fatal(e)
	}
	c := &memoryChain{network: cryptochain.Tron, balance: "10000000"}
	x := Executor{Service: s, Adapters: map[string]cryptochain.Adapter{"tron": c}, Witness: map[string]cryptochain.Adapter{"tron": &witnessMemory{c}}, Signer: &memorySigner{}, FeeLimits: map[string]uint64{"tron": 100000000}}
	if e := x.RunSweeps(t.Context(), "tron"); e != nil {
		t.Fatal(e)
	}
	out, _ := s.ListWithdrawals(t.Context(), 1)
	if len(out) != 1 {
		t.Fatal(out)
	}
	if e := x.Execute(t.Context(), out[0].ID); e != nil {
		t.Fatal(e)
	}
	w, _ := s.GetWithdrawal(t.Context(), out[0].ID)
	c.status = cryptochain.TransactionStatus{Chain: cryptochain.Tron, TxID: w.TxID, Found: true, Finalized: true, Success: false, FeeAtomic: "0"}
	if e := x.Execute(t.Context(), w.ID); e != nil {
		t.Fatal(e)
	}
	var revision, failures int
	var idle bool
	if e := s.Pool.QueryRow(t.Context(), "SELECT revision,failures,next_attempt='infinity'::timestamptz FROM crypto_sweep_queue WHERE address=$1", v.Address).Scan(&revision, &failures, &idle); e != nil || revision != 2 || failures != 1 || idle {
		t.Fatal(revision, failures, idle, e)
	}
	b, _ := s.Balances(t.Context(), 1)
	if b[0].Available != "9.9" || b[0].Reserved != "0" {
		t.Fatal(b)
	}
	if _, e := s.Pool.Exec(t.Context(), "UPDATE crypto_sweep_queue SET next_attempt=clock_timestamp()-interval '1 second' WHERE address=$1", v.Address); e != nil {
		t.Fatal(e)
	}
	if e := x.RunSweeps(t.Context(), "tron"); e != nil {
		t.Fatal(e)
	}
	out, _ = s.ListWithdrawals(t.Context(), 1)
	if len(out) != 2 || out[0].RequestID == out[1].RequestID {
		t.Fatal("safe finalized failure not requeued", out)
	}
}
