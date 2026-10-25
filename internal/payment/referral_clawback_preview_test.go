package payment

import (
	"context"
	"errors"
	"testing"
)

func TestReferralClawbackPreviewBindsOriginalBudgetAndEveryRelevantState(t *testing.T) {
	s, _, _, o := referralClawbackFixture(t, "platform")
	ctx := context.Background()
	preview, err := s.ReferralRefundPreview(ctx, s.pool, o.ID, "first", 98)
	if err != nil || preview.Amount != 5 || preview.OriginalFee != 10 || preview.PriorFeeUsed != 0 || preview.RefundFee != 8 || preview.FeeContribution != 3 || preview.ClawbackState != "eligible" || len(preview.RewardHash) != 32 {
		t.Fatalf("unbound new budget %+v %v", preview, err)
	}
	r, err := s.ReserveRefund(ctx, 902, o.OutTradeNo, "first", 98)
	if err != nil {
		t.Fatal(err)
	}
	preview, err = s.ReferralRefundPreview(ctx, s.pool, o.ID, "first", 98)
	if err != nil || preview.ExistingRefundID != r.ID || preview.ClawbackRefundID != r.ID || preview.ClawbackState != "pending" || len(preview.RequestHash) != 32 || preview.Amount != 5 {
		t.Fatalf("replay lost original recovery %+v %v", preview, err)
	}
	if _, err = s.ReferralRefundPreview(ctx, s.pool, o.ID, "different", 1); !errors.Is(err, ErrState) {
		t.Fatalf("unknown reward authorized for new number %v", err)
	}
	if _, err = s.ReferralRefundPreview(ctx, s.pool, o.ID, "first", 97); !errors.Is(err, ErrConflict) {
		t.Fatalf("replay amount changed %v", err)
	}
	if err = s.ResolveRefund(ctx, r.ID, "unknown"); err != nil {
		t.Fatal(err)
	}
	preview, err = s.ReferralRefundPreview(ctx, s.pool, o.ID, "first", 98)
	if err != nil || preview.ClawbackState != "unknown" || len(preview.ResultHash) != 32 {
		t.Fatalf("lost unknown proof %+v %v", preview, err)
	}
	if err = s.ResolveRefund(ctx, r.ID, "failed"); err != nil {
		t.Fatal(err)
	}
	preview, err = s.ReferralRefundPreview(ctx, s.pool, o.ID, "second", 1)
	if err != nil || preview.Amount != 5 || preview.ClawbackState != "eligible" || preview.ClawbackRefundID != r.ID || len(preview.ResultHash) != 32 {
		t.Fatalf("fresh authorization failed to bind last restored debt %+v %v", preview, err)
	}
	r, err = s.ReserveRefund(ctx, 902, o.OutTradeNo, "second", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ResolveRefund(ctx, r.ID, "succeeded"); err != nil {
		t.Fatal(err)
	}
	preview, err = s.ReferralRefundPreview(ctx, s.pool, o.ID, "rest", 99)
	if err != nil || preview.Amount != 0 || preview.FeeContribution != 0 || preview.RefundFee != 10 || preview.ClawbackRefundID != r.ID || preview.ClawbackState != "succeeded" || len(preview.ResultHash) != 32 {
		t.Fatalf("voided reward authorized twice %+v %v", preview, err)
	}
	rest, err := s.ReserveRefund(ctx, 902, o.OutTradeNo, "rest", 99)
	if err != nil {
		t.Fatal(err)
	}
	preview, err = s.ReferralRefundPreview(ctx, s.pool, o.ID, "rest", 99)
	if err != nil || preview.Amount != 0 || preview.ExistingRefundID != rest.ID || preview.ClawbackRefundID != r.ID || preview.ClawbackState != "succeeded" {
		t.Fatalf("later ordinary refund replay became new clawback %+v %v", preview, err)
	}
}
