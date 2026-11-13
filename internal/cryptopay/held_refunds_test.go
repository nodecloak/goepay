package cryptopay

import (
	"testing"
	"time"
)

func TestHeldRefundCannotSpendAnotherInvoicesFunds(t *testing.T) {
	s := testService(t)
	v := invoice(t, s, "held")
	seedRecipient(t, s, depositTwo)
	if e := s.RecordDeposit(t.Context(), Deposit{Network: "tron", TxID: "short", BlockHeight: 1, BlockHash: "b", Address: v.Address, Amount: "4", OccurredAt: v.ExpiresAt.Add(-time.Minute)}); e != nil {
		t.Fatal(e)
	}
	r, e := s.RefundHeldFunds(t.Context(), RefundInput{MerchantID: 1, RequestID: "held-refund", InvoiceID: v.ID, Address: depositTwo, Amount: "4"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RefundHeldFunds(t.Context(), RefundInput{MerchantID: 1, RequestID: "duplicate", InvoiceID: v.ID, Address: depositTwo, Amount: "1"}); e == nil {
		t.Fatal("held amount reused")
	}
	if r.Kind != "exception_refund" {
		t.Fatal(r)
	}
	if _, e = s.CancelWithdrawal(t.Context(), 1, r.ID); e != nil {
		t.Fatal(e)
	}
	b, _ := s.Balances(t.Context(), 1)
	if len(b) > 0 && b[0].Available != "0" {
		t.Fatalf("held refund cancellation created spendable balance %+v", b)
	}
	report, e := s.Reconcile(t.Context())
	if e != nil || report.BalanceMismatches != 0 || report.ImbalancedJournals != 0 {
		t.Fatal(report, e)
	}
}
