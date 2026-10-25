package backoffice

import (
	"context"
	"testing"
)

func TestMerchantDomainChangeRevokesApproval(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	m := Principal{Role: "merchant", SubjectID: 101, Username: "m1"}
	d, err := s.Create(ctx, m, "domains", input(`{"domain":"approved.example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, admin, "domains", d.ID, input(`{"approved":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Create(ctx, admin, "settings", input(`{"key":"enforce_domains","value":"true"}`)); err != nil {
		t.Fatal(err)
	}
	changed, err := s.Update(ctx, m, "domains", d.ID, input(`{"domain":"unreviewed.example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	if changed.Fields["approved"] != false {
		t.Fatal("merchant retained approval after changing domain")
	}
	if err = s.CheckPayment(ctx, PaymentContext{MerchantID: 101, ReturnURL: "https://unreviewed.example.com/return"}); err == nil {
		t.Fatal("unreviewed domain passed policy")
	}
	if _, err = s.Update(ctx, admin, "domains", d.ID, input(`{"approved":true}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.CheckPayment(ctx, PaymentContext{MerchantID: 101, ReturnURL: "https://unreviewed.example.com/return"}); err != nil {
		t.Fatal(err)
	}
}
