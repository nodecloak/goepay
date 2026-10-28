package payment

import (
	"context"
	"errors"
	"testing"

	provider "goepay/internal/adapters/payment"
)

type combineRefundDouble207 struct {
	provider.Provider
	sent, queried []provider.RefundRequest
	fail          map[string]error
	state         string
}

func (d *combineRefundDouble207) Refund(_ context.Context, r provider.RefundRequest) (provider.RefundResult, error) {
	d.sent = append(d.sent, r)
	if e := d.fail[r.RefundNo]; e != nil {
		return provider.RefundResult{}, e
	}
	return provider.RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: "WXR-" + r.RefundNo, State: d.state, Amount: r.Amount}, nil
}
func (d *combineRefundDouble207) QueryRefund(_ context.Context, r provider.RefundRequest) (provider.RefundResult, error) {
	d.queried = append(d.queried, r)
	return provider.RefundResult{RefundNo: r.RefundNo, ProviderRefundNo: "WXR-" + r.RefundNo, State: "SUCCESS", Amount: r.Amount}, nil
}

func combineRefund207Paid(t *testing.T) (*Store, Order) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	o := combine207Order(t, s, 10000)
	if _, err := s.CreateCombineSubOrders(ctx, o.ID, o.TradeNo, []int64{3334, 3333, 3333}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkPaid(ctx, o.TradeNo, "combine207-paid"); err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkCombineSubOrdersPaidTx(ctx, tx, o.ID, []CombinePaid207{{o.TradeNo + "1", "WX1", 3334}, {o.TradeNo + "2", "WX2", 3333}, {o.TradeNo + "3", "WX3", 3333}}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return s, o
}

func TestCombineRefund207SplitsAcrossSharesWithFixedNumbers(t *testing.T) {
	s, o := combineRefund207Paid(t)
	ctx := context.Background()
	r, err := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "cr1", 5000)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.planCombineRefund207(ctx, r.ID, o.ID, 5000, "RF1")
	if err != nil || len(plan) != 2 || plan[0].Amount != 3334 || plan[1].Amount != 1666 || plan[0].ProviderRefundNo != "RF1-1" || plan[1].ProviderRefundNo != "RF1-2" {
		t.Fatal("refund not split in share order", plan, err)
	}
	again, err := s.planCombineRefund207(ctx, r.ID, o.ID, 5000, "RF1")
	if err != nil || len(again) != 2 || again[1].ProviderRefundNo != "RF1-2" {
		t.Fatal("re-planning changed the fixed plan", again, err)
	}
	d := &combineRefundDouble207{state: "SUCCESS"}
	state, err := ProviderFlow{Store: s}.combineRefund207(ctx, d, r.ID, plan, true)
	if err != nil || state != "succeeded" || len(d.sent) != 2 || d.sent[0].TradeNo != o.TradeNo+"1" || d.sent[0].ProviderTradeNo != "WX1" || d.sent[0].TotalAmount != 3334 || d.sent[1].Amount != 1666 {
		t.Fatal("shares not refunded against their own sub-orders", state, err, d.sent)
	}
	subs, _ := s.CombineSubOrders(ctx, o.ID)
	if subs[0].RefundedAmount != 3334 || subs[1].RefundedAmount != 1666 || subs[2].RefundedAmount != 0 {
		t.Fatal("sub-order refunded amounts wrong", subs)
	}
	// A second refund continues from the remaining room.
	r2, err := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "cr2", 3000)
	if err != nil {
		t.Fatal(err)
	}
	plan2, err := s.planCombineRefund207(ctx, r2.ID, o.ID, 3000, "RF2")
	if err != nil || len(plan2) != 2 || plan2[0].Position != 2 || plan2[0].Amount != 1667 || plan2[1].Position != 3 || plan2[1].Amount != 1333 {
		t.Fatal("second refund did not continue after earlier shares", plan2, err)
	}
}

func TestCombineRefund207UnknownStopsAndQueryResumesSameNumbers(t *testing.T) {
	s, o := combineRefund207Paid(t)
	ctx := context.Background()
	r, err := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "cr-unknown", 7000)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.planCombineRefund207(ctx, r.ID, o.ID, 7000, "RF9")
	if err != nil || len(plan) != 3 {
		t.Fatal(plan, err)
	}
	d := &combineRefundDouble207{state: "SUCCESS", fail: map[string]error{"RF9-2": &provider.UnknownOutcome{Operation: "refund", Cause: provider.ErrAuthentication}}}
	state, err := ProviderFlow{Store: s}.combineRefund207(ctx, d, r.ID, plan, true)
	var unknown *provider.UnknownOutcome
	if state != "unknown" || !errors.As(err, &unknown) || len(d.sent) != 2 {
		t.Fatal("unknown share must stop the run without sending later shares", state, err, len(d.sent))
	}
	plan, _ = s.planCombineRefund207(ctx, r.ID, o.ID, 7000, "RF9")
	state, err = ProviderFlow{Store: s}.combineRefund207(ctx, d, r.ID, plan, false)
	if err != nil || state != "succeeded" || len(d.queried) != 2 || d.queried[0].RefundNo != "RF9-2" || d.queried[1].RefundNo != "RF9-3" {
		t.Fatal("query did not resume the same fixed refund numbers", state, err, d.queried)
	}
	if len(d.sent) != 2 {
		t.Fatal("recovery re-sent a refund")
	}
}

func TestCombineRefund207CannotExceedPaidShares(t *testing.T) {
	s, o := combineRefund207Paid(t)
	ctx := context.Background()
	if _, err := s.planCombineRefund207(ctx, 999999, o.ID, 10001, "RFX"); err == nil {
		t.Fatal("over-refund plan accepted")
	}
	if combineRefundState207([]combineSubRefund207{{State: "succeeded"}, {State: "failed"}}) != "unknown" {
		t.Fatal("mixed succeeded/failed shares must need review")
	}
}
