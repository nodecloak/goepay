package cryptopay

import (
	"context"
	"goepay/internal/cryptochain"
	"testing"
	"time"
)

type scanChain struct {
	memoryChain
	hash      string
	transfers []cryptochain.Transfer
}

func (c *scanChain) FinalizedHeight(context.Context) (uint64, error)            { return 1, nil }
func (c *scanChain) CanonicalBlockHash(context.Context, uint64) (string, error) { return c.hash, nil }
func (c *scanChain) ScanTransfers(context.Context, uint64, uint64) ([]cryptochain.Transfer, error) {
	return c.transfers, nil
}
func TestScannerDisagreementCannotCreditAndRecoveryDoesNotDuplicate(t *testing.T) {
	s := testService(t)
	v := invoice(t, s, "scan")
	c := &scanChain{memoryChain: memoryChain{network: cryptochain.Tron}, hash: "a", transfers: []cryptochain.Transfer{{Chain: cryptochain.Tron, TxID: "tx", EventIndex: 0, BlockHeight: 1, BlockHash: "a", To: v.Address, AtomicAmount: "10000000", Contract: cryptochain.TronUSDTContract, OccurredAt: time.Now().Add(2 * time.Second)}}}
	other := *c
	other.hash = "b"
	scanner := Scanner{Service: s, Primary: map[string]cryptochain.Adapter{"tron": c}, Witness: map[string]cryptochain.Adapter{"tron": &other}, StartHeight: map[string]uint64{"tron": 1}}
	if e := scanner.Scan(t.Context(), "tron"); e == nil {
		t.Fatal("disagreement credited")
	}
	b, _ := s.Balances(t.Context(), 1)
	if len(b) != 0 {
		t.Fatal(b)
	}
	other.hash = "a"
	if e := scanner.Scan(t.Context(), "tron"); e != nil {
		t.Fatal(e)
	}
	if e := scanner.Scan(t.Context(), "tron"); e != nil {
		t.Fatal(e)
	}
	b, _ = s.Balances(t.Context(), 1)
	if len(b) != 1 || b[0].Available != "9.9" {
		t.Fatal(b)
	}
	other.hash = "reorg"
	if e := scanner.Scan(t.Context(), "tron"); e == nil {
		t.Fatal("canonical cursor changed")
	}
}
func TestLateFundsAndExpiredAddressCannotBeReassigned(t *testing.T) {
	s := testService(t)
	v := invoice(t, s, "late")
	_, e := s.Pool.Exec(t.Context(), "UPDATE crypto_invoices SET expires_at=clock_timestamp()-interval '1 minute' WHERE id=$1", v.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RecordDeposit(t.Context(), Deposit{Network: "tron", TxID: "late", BlockHeight: 1, BlockHash: "b", Address: v.Address, Amount: "10", OccurredAt: time.Now().Add(2 * time.Second)}); e != nil {
		t.Fatal(e)
	}
	got, _ := s.GetInvoice(t.Context(), 1, v.ID)
	b, _ := s.Balances(t.Context(), 1)
	if got.State != "late" || len(b) != 0 {
		t.Fatalf("%+v %+v", got, b)
	}
	if _, e = s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: 1, OutTradeNo: "new", Network: "tron", Amount: "10"}); e == nil {
		t.Fatal("old address reused")
	}
}
func TestHistoricalFundsCannotPayNewInvoice(t *testing.T) {
	s := testService(t)
	v := invoice(t, s, "history")
	if e := s.RecordDeposit(t.Context(), Deposit{Network: "tron", TxID: "historical", BlockHeight: 1, BlockHash: "b", Address: v.Address, Amount: "10", OccurredAt: time.Now().Add(-time.Hour)}); e != nil {
		t.Fatal(e)
	}
	v, _ = s.GetInvoice(t.Context(), 1, v.ID)
	b, _ := s.Balances(t.Context(), 1)
	if v.State == "paid" || len(b) > 0 {
		t.Fatalf("historical funds assigned to new order %+v %+v", v, b)
	}
}
