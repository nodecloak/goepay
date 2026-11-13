package cryptopay

import (
	"context"
	"errors"
	"goepay/internal/cryptochain"
	"testing"
	"time"
)

type memoryChain struct {
	cryptochain.Adapter
	network              cryptochain.Chain
	prepared, broadcasts int
	status               cryptochain.TransactionStatus
	unknown              bool
	balance              string
}

func (c *memoryChain) Chain() cryptochain.Chain                        { return c.network }
func (c *memoryChain) VerifyNetwork(context.Context) error             { return nil }
func (c *memoryChain) Balance(context.Context, string) (string, error) { return c.balance, nil }
func (c *memoryChain) Prepare(_ context.Context, r cryptochain.TransferRequest) (cryptochain.PreparedTransaction, error) {
	c.prepared++
	return reviewPreparedTransfer(r)
}
func (c *memoryChain) Broadcast(_ context.Context, v cryptochain.SignedTransaction) (cryptochain.BroadcastResult, error) {
	c.broadcasts++
	if c.unknown {
		return cryptochain.BroadcastResult{}, errors.New("lost reply")
	}
	return cryptochain.BroadcastResult{TxID: v.TxID, Accepted: true}, nil
}
func (c *memoryChain) Lookup(context.Context, string) (cryptochain.TransactionStatus, error) {
	return c.status, nil
}

type memorySigner struct{ calls int }

func (s *memorySigner) Sign(_ context.Context, p cryptochain.PreparedTransaction) (cryptochain.SignedTransaction, error) {
	s.calls++
	return reviewSignPrepared(p)
}
func fundedWithdrawal(t *testing.T) (*Service, Withdrawal) {
	t.Helper()
	s := testService(t)
	v := invoice(t, s, "outgoing")
	e := s.RecordDeposit(t.Context(), Deposit{Network: "tron", TxID: "fund", BlockHeight: 1, BlockHash: "b", Address: v.Address, Amount: "10", OccurredAt: time.Now().Add(2 * time.Second)})
	if e != nil {
		t.Fatal(e)
	}
	cfg := s.Networks["tron"]
	cfg.HotAddress = depositOne
	cfg.TreasuryAddress = depositTwo
	s.Networks["tron"] = cfg
	seedRecipient(t, s, depositTwo)
	w, e := s.RequestWithdrawal(t.Context(), WithdrawalInput{MerchantID: 1, RequestID: "settle", Network: "tron", Address: depositTwo, Amount: "6"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApproveWithdrawal(t.Context(), w.ID, 9); e != nil {
		t.Fatal(e)
	}
	return s, w
}
func TestUnknownBroadcastRetainsReserveAndOriginalSignedTransaction(t *testing.T) {
	s, w := fundedWithdrawal(t)
	c := &memoryChain{network: cryptochain.Tron, unknown: true}
	signer := &memorySigner{}
	x := Executor{Service: s, Adapters: map[string]cryptochain.Adapter{"tron": c}, Witness: map[string]cryptochain.Adapter{"tron": &witnessMemory{c}}, Signer: signer, FeeLimits: map[string]uint64{"tron": 100000000}}
	if e := x.Execute(t.Context(), w.ID); e == nil {
		t.Fatal("expected unknown broadcast")
	}
	a, _ := s.GetWithdrawal(t.Context(), w.ID)
	if a.State != "unknown" || a.TxID != reviewFixtureTxID {
		t.Fatalf("%+v", a)
	}
	if e := x.Execute(t.Context(), w.ID); e == nil {
		t.Fatal("unknown remains")
	}
	if c.prepared != 1 || signer.calls != 1 {
		t.Fatalf("new financial intent %d %d", c.prepared, signer.calls)
	}
	c.status = cryptochain.TransactionStatus{Chain: cryptochain.Tron, TxID: reviewFixtureTxID, FeeAtomic: "0", Found: true, Finalized: true, Success: true, Transfers: []cryptochain.Transfer{{Chain: cryptochain.Tron, TxID: reviewFixtureTxID, From: depositOne, To: depositTwo, Contract: cryptochain.TronUSDTContract, AtomicAmount: "6000000"}}}
	if e := x.Execute(t.Context(), w.ID); e != nil {
		t.Fatal(e)
	}
	a, _ = s.GetWithdrawal(t.Context(), w.ID)
	b, _ := s.Balances(t.Context(), 1)
	if a.State != "confirmed" || b[0].Reserved != "0" || b[0].Available != "3.9" {
		t.Fatalf("%+v %+v", a, b)
	}
	if e := x.Execute(t.Context(), w.ID); e != nil {
		t.Fatal(e)
	}
	b, _ = s.Balances(t.Context(), 1)
	if b[0].Reserved != "0" {
		t.Fatal(b)
	}
}
func TestRefundLimitCancellationAndFailedReceipt(t *testing.T) {
	s, w := fundedWithdrawal(t)
	if _, e := s.CancelWithdrawal(t.Context(), 2, w.ID); e == nil {
		t.Fatal("another merchant cancellation")
	}
	c := &memoryChain{network: cryptochain.Tron}
	x := Executor{Service: s, Adapters: map[string]cryptochain.Adapter{"tron": c}, Witness: map[string]cryptochain.Adapter{"tron": &witnessMemory{c}}, Signer: &memorySigner{}, FeeLimits: map[string]uint64{"tron": 100000000}}
	if e := x.Execute(t.Context(), w.ID); e != nil {
		t.Fatal(e)
	}
	c.status = cryptochain.TransactionStatus{Chain: cryptochain.Tron, TxID: reviewFixtureTxID, FeeAtomic: "0", Found: true, Finalized: true, Success: false}
	if e := x.Execute(t.Context(), w.ID); e != nil {
		t.Fatal(e)
	}
	b, _ := s.Balances(t.Context(), 1)
	if b[0].Available != "9.9" || b[0].Reserved != "0" {
		t.Fatal(b)
	}
	v, _ := s.ListInvoices(t.Context(), 1)
	r, e := s.Refund(t.Context(), RefundInput{MerchantID: 1, RequestID: "refund", InvoiceID: v[0].ID, Address: depositTwo, Amount: "4"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Refund(t.Context(), RefundInput{MerchantID: 1, RequestID: "refund", InvoiceID: v[0].ID, Address: depositTwo, Amount: "5"}); e == nil {
		t.Fatal("mutated refund accepted")
	}
	if _, e = s.CancelWithdrawal(t.Context(), 1, r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Refund(t.Context(), RefundInput{MerchantID: 1, RequestID: "too-large", InvoiceID: v[0].ID, Address: depositTwo, Amount: "11"}); e == nil {
		t.Fatal("over-refund")
	}
}
func TestConflictingOutgoingWitnessCannotReleaseReserve(t *testing.T) {
	s, w := fundedWithdrawal(t)
	a := &memoryChain{network: cryptochain.Tron}
	b := &memoryChain{network: cryptochain.Tron}
	x := Executor{Service: s, Adapters: map[string]cryptochain.Adapter{"tron": a}, Witness: map[string]cryptochain.Adapter{"tron": b}, Signer: &memorySigner{}, FeeLimits: map[string]uint64{"tron": 100000000}}
	if e := x.Execute(t.Context(), w.ID); e != nil {
		t.Fatal(e)
	}
	a.status = cryptochain.TransactionStatus{Chain: cryptochain.Tron, TxID: reviewFixtureTxID, FeeAtomic: "0", Found: true, Finalized: true, Success: false}
	if e := x.Execute(t.Context(), w.ID); e == nil {
		t.Fatal("unconfirmed witness released reserve")
	}
	v, _ := s.GetWithdrawal(t.Context(), w.ID)
	balances, _ := s.Balances(t.Context(), 1)
	if v.State == "failed" || balances[0].Reserved != "6" {
		t.Fatal(v, balances)
	}
}

type witnessMemory struct{ *memoryChain }
