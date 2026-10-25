package payment

import (
	"context"
	"errors"
	"strconv"
	"testing"
)

func TestReferralConsoleFlowsRemainScopedAndDistinguishPendingRecovery(t *testing.T) {
	s, promoter, mid, _ := referralRewardFixture(t)
	ctx := context.Background()
	actor := TransferBatchActor{Role: "merchant", ID: promoter, Name: "local-referral-reader"}
	a := referralCreateTestOrder(t, s, mid, 100, "console-a")
	referralPayTestOrder(t, s, a)
	b := referralCreateTestOrder(t, s, mid, 100, "console-b")
	referralPayTestOrder(t, s, b)
	r, err := s.ReserveRefund(ctx, mid, a.OutTradeNo, "console-refund-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ResolveRefund(ctx, r.ID, "succeeded"); err != nil {
		t.Fatal(err)
	}
	r, err = s.ReserveRefund(ctx, mid, b.OutTradeNo, "console-refund-b", 10)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ResolveRefund(ctx, r.ID, "unknown"); err != nil {
		t.Fatal(err)
	}
	view, err := s.ReferralOverview(ctx, promoter, actor)
	if err != nil || view.InvitedCount != 1 || view.IssuedAmount != 10 || view.VoidedAmount != 5 || view.RecoveryPendingAmount != 5 || view.TodayIssued != 10 || view.YesterdayIssued != 0 {
		t.Fatal(view, err)
	}
	page, err := s.ListReferralRewards(ctx, promoter, 0, 1, actor)
	if err != nil || len(page.Items) != 1 || page.Items[0].State != "voided" || page.NextCursor == "" {
		t.Fatal(page, err)
	}
	cursor, err := strconv.ParseInt(page.NextCursor, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ListReferralRewards(ctx, promoter, cursor, 1, actor)
	if err != nil || len(second.Items) != 1 || second.Items[0].State != "recovery_pending" || second.NextCursor != "" || second.Items[0].ID == page.Items[0].ID {
		t.Fatal(second, err)
	}
	if _, err = s.ReferralOverview(ctx, mid, actor); !errors.Is(err, ErrInvalid) {
		t.Fatal("cross-owner overview", err)
	}
	if _, err = s.ListReferralRewards(ctx, promoter, 0, 101, actor); !errors.Is(err, ErrInvalid) {
		t.Fatal("unbounded reward list", err)
	}
	if err = s.ResolveRefund(ctx, r.ID, "failed"); err != nil {
		t.Fatal(err)
	}
	view, err = s.ReferralOverview(ctx, promoter, actor)
	if err != nil || view.IssuedAmount != 10 || view.VoidedAmount != 5 || view.RecoveryPendingAmount != 0 {
		t.Fatal(view, err)
	}
	balance, err := s.Balance(ctx, promoter)
	if err != nil || balance.Available != 5 {
		t.Fatal(balance, err)
	}
	report, err := s.CheckLedger(ctx)
	if err != nil || report.InvalidReferralTerms != 0 || report.InvalidReferralRewards != 0 || report.InvalidReferralClawbacks != 0 || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
		t.Fatal(report, err)
	}
}
