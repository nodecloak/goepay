package backoffice

import (
	"context"
	"errors"
	"testing"

	"goepay/internal/payment"
)

// 站内分账 multi-receiver (a4 N3): a rule may carry a `receivers` list of up to
// 10 legs, each recipient distinct and not the owner, with the rate total <=100%
// (within the rule and across the owner's enabled rules). The stored body is
// canonicalized to the receivers list plus a top-level rate_bps equal to the sum.
func TestProfitRuleMultiReceiverValidationAndCanonicalBody(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	if e := s.store.CreateMerchant(ctx, payment.Merchant{ID: 103, Name: "r3", APIKey: "r3-secret-long"}); e != nil {
		t.Fatal(e)
	}
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "m1"}

	// Two receivers, 30% + 20% = 50%, accepted.
	rule, e := s.Create(ctx, merchant, "profitsharing-rules", input(`{"name":"multi","receivers":[{"recipient_id":"102","rate_bps":"3000"},{"recipient_id":"103","rate_bps":"2000"}]}`))
	if e != nil {
		t.Fatal("multi-receiver create", e)
	}
	// Body canonicalized: receivers preserved + summed rate_bps.
	got, e := s.Get(ctx, merchant, "profitsharing-rules", rule.ID)
	if e != nil {
		t.Fatal(e)
	}
	body := got.Fields
	if body["rate_bps"] != "5000" {
		t.Fatalf("summed rate_bps=%v want 5000", body["rate_bps"])
	}
	legs, ok := body["receivers"].([]any)
	if !ok || len(legs) != 2 {
		t.Fatalf("receivers not stored as a 2-element list: %v", body["receivers"])
	}

	// A second enabled rule pushing the owner's total over 100% is rejected.
	if _, e = s.Create(ctx, merchant, "profitsharing-rules", input(`{"name":"over","receivers":[{"recipient_id":"102","rate_bps":"6000"}]}`)); !errors.Is(e, ErrInvalid) {
		t.Fatalf("cross-rule Σ>100%% accepted: %v", e)
	}

	// Within-rule total over 100% rejected.
	if _, e = s.Create(ctx, merchant, "profitsharing-rules", input(`{"name":"inner","receivers":[{"recipient_id":"102","rate_bps":"7000"},{"recipient_id":"103","rate_bps":"4000"}]}`)); !errors.Is(e, ErrInvalid) {
		t.Fatalf("within-rule Σ>100%% accepted: %v", e)
	}

	// Duplicate recipient rejected.
	if _, e = s.Create(ctx, merchant, "profitsharing-rules", input(`{"name":"dup","receivers":[{"recipient_id":"102","rate_bps":"1000"},{"recipient_id":"102","rate_bps":"1000"}]}`)); !errors.Is(e, ErrInvalid) {
		t.Fatalf("duplicate recipient accepted: %v", e)
	}

	// Owner as its own recipient rejected.
	if _, e = s.Create(ctx, merchant, "profitsharing-rules", input(`{"name":"self","receivers":[{"recipient_id":"101","rate_bps":"1000"}]}`)); !errors.Is(e, ErrInvalid) {
		t.Fatalf("self recipient accepted: %v", e)
	}

	// More than 10 legs rejected.
	big := `{"name":"big","receivers":[`
	for i := 0; i < 11; i++ {
		if i > 0 {
			big += ","
		}
		big += `{"recipient_id":"102","rate_bps":"1"}`
	}
	big += `]}`
	if _, e = s.Create(ctx, merchant, "profitsharing-rules", input(big)); !errors.Is(e, ErrInvalid) {
		t.Fatalf(">10 receivers accepted: %v", e)
	}

	// Legacy single-receiver create still works and stays canonical.
	single, e := s.Create(ctx, Principal{Role: "merchant", SubjectID: 102, Username: "m2"}, "profitsharing-rules", input(`{"name":"legacy","recipient_id":"101","rate_bps":"1000"}`))
	if e != nil {
		t.Fatal("legacy single create", e)
	}
	sgot, _ := s.Get(ctx, Principal{Role: "merchant", SubjectID: 102, Username: "m2"}, "profitsharing-rules", single.ID)
	if sgot.Fields["recipient_id"] != "101" || sgot.Fields["rate_bps"] != "1000" {
		t.Fatalf("legacy body not preserved: %v", sgot.Fields)
	}
	var one []any
	if one, ok = sgot.Fields["receivers"].([]any); !ok || len(one) != 1 {
		t.Fatalf("legacy rule not normalized to a 1-leg receivers list: %v", sgot.Fields["receivers"])
	}
}
