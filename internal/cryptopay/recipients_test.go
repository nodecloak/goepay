package cryptopay

import (
	"testing"
	"time"
)

func TestRecipientRequiresApprovalAndCoolingBeforeReservation(t *testing.T) {
	s, _ := fundedWithdrawal(t)
	r, e := s.RequestRecipient(t.Context(), 1, "tron", depositOne)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RequestWithdrawal(t.Context(), WithdrawalInput{1, "new-address", "tron", depositOne, "1"}); e == nil {
		t.Fatal("unapproved address")
	}
	if _, e = s.ApproveRecipient(t.Context(), r.ID, 9); e != nil {
		t.Fatal(e)
	}
	if _, e = s.RequestWithdrawal(t.Context(), WithdrawalInput{1, "new-address", "tron", depositOne, "1"}); e == nil {
		t.Fatal("cooling address")
	}
	if r.UsableAt.Before(time.Now().Add(23 * time.Hour)) {
		t.Fatal(r)
	}
	if _, e = s.Pool.Exec(t.Context(), "UPDATE crypto_recipients SET usable_at=clock_timestamp()-interval '1 second' WHERE id=$1", r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.RequestWithdrawal(t.Context(), WithdrawalInput{1, "new-address", "tron", depositOne, "1"}); e != nil {
		t.Fatal(e)
	}
}
