package payment

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"

	provider "goepay/internal/adapters/payment"
)

type combineRefundDouble207 struct {
	provider.Provider
	sent, queried []provider.RefundRequest
	fail          map[string]error
	state         string
}

// sentOnlyRefund207 models an upstream where querying a refund that was never
// submitted returns an error. The second send is accepted but its reply lost.
type sentOnlyRefund207 struct {
	combineRefundDouble207
}

type blockingRefund207 struct {
	provider.Provider
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (d *blockingRefund207) Refund(_ context.Context, r provider.RefundRequest) (provider.RefundResult, error) {
	d.calls.Add(1)
	close(d.started)
	<-d.release
	return provider.RefundResult{RefundNo: r.RefundNo, Amount: r.Amount, State: "SUCCESS"}, nil
}
func (d *blockingRefund207) QueryRefund(_ context.Context, r provider.RefundRequest) (provider.RefundResult, error) {
	return provider.RefundResult{RefundNo: r.RefundNo, Amount: r.Amount, State: "PROCESSING"}, nil
}

func TestCombineRefund207ConcurrentResumeClaimsOnlyOnce(t *testing.T) {
	s, o := combineRefund207Paid(t)
	ctx := context.Background()
	r, err := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "cr-concurrent", 1000)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.planCombineRefund207(ctx, r.ID, o.ID, 1000, "RF-concurrent")
	if err != nil {
		t.Fatal(err)
	}
	d := &blockingRefund207{started: make(chan struct{}), release: make(chan struct{})}
	result := make(chan error, 1)
	go func() { _, e := (ProviderFlow{Store: s}).combineRefund207(ctx, d, r.ID, plan, true); result <- e }()
	<-d.started
	other, err := s.planCombineRefund207(ctx, r.ID, o.ID, 1000, "RF-concurrent")
	if err != nil {
		close(d.release)
		t.Fatal(err)
	}
	_, err = (ProviderFlow{Store: s}).combineRefund207(ctx, d, r.ID, other, true)
	close(d.release)
	if err != nil {
		t.Fatal(err)
	}
	if err = <-result; err != nil {
		t.Fatal(err)
	}
	if got := d.calls.Load(); got != 1 {
		t.Fatalf("provider received %d sends", got)
	}
	subs, err := s.CombineSubOrders(ctx, o.ID)
	if err != nil || subs[0].RefundedAmount != 1000 {
		t.Fatal(subs, err)
	}
}

func TestCombineRefund207ClaimSurvivesCrashWithoutResending(t *testing.T) {
	s, o := combineRefund207Paid(t)
	ctx := context.Background()
	r, err := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "cr-crash", 1000)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.planCombineRefund207(ctx, r.ID, o.ID, 1000, "RF-crash")
	if err != nil {
		t.Fatal(err)
	}
	// A crash can occur immediately after committing the claim, before or
	// after the provider receives the request. This ambiguity forbids resend.
	if _, err = s.pool.Exec(ctx, `INSERT INTO combine_refund_send_claims207(refund_id,position) VALUES($1,$2)`, r.ID, plan[0].Position); err != nil {
		t.Fatal(err)
	}
	d := &sentOnlyRefund207{combineRefundDouble207{state: "SUCCESS"}}
	state, err := (ProviderFlow{Store: s}).combineRefund207(ctx, d, r.ID, plan, true)
	if state != "unknown" || err == nil || len(d.sent) != 0 {
		t.Fatal("ambiguous claim was resent", state, err, d.sent)
	}
}

func TestCombineRefund207MigrationConservativelyClaimsHistoricalPlans(t *testing.T) {
	s, o := combineRefund207Paid(t)
	ctx := context.Background()
	// The store uses its own synthetic schema; reconstruct the state directly
	// before migration 000182, including an ambiguous historical pending row.
	if _, err := s.pool.Exec(ctx, `DROP TABLE combine_refund_send_claims207; DROP FUNCTION combine_refund_send_claims207_immutable()`); err != nil {
		t.Fatal(err)
	}
	r, err := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "cr-before-migration", 1000)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.planCombineRefund207(ctx, r.ID, o.ID, 1000, "RF-historical")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../db/migrations/000182_combine_refund_send_claims.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM combine_refund_send_claims207 WHERE refund_id=$1 AND position=$2)`, r.ID, plan[0].Position).Scan(&exists); err != nil || !exists {
		t.Fatal("historical claim not backfilled", err)
	}
	d := &sentOnlyRefund207{combineRefundDouble207{state: "SUCCESS"}}
	state, err := (ProviderFlow{Store: s}).combineRefund207(ctx, d, r.ID, plan, true)
	if state != "unknown" || err == nil || len(d.sent) != 0 {
		t.Fatal("historical ambiguous share resent", state, err, d.sent)
	}
}

func (d *sentOnlyRefund207) QueryRefund(ctx context.Context, r provider.RefundRequest) (provider.RefundResult, error) {
	for _, sent := range d.sent {
		if sent.RefundNo == r.RefundNo {
			return d.combineRefundDouble207.QueryRefund(ctx, r)
		}
	}
	return provider.RefundResult{}, &provider.ProviderError{Code: "REFUND_NOT_FOUND"}
}

func TestCombineRefund207ResumeSendsOnlyPreviouslyUnsentShares(t *testing.T) {
	s, o := combineRefund207Paid(t)
	ctx := context.Background()
	r, err := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "cr-resume", 7000)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.planCombineRefund207(ctx, r.ID, o.ID, 7000, "RF-resume")
	if err != nil {
		t.Fatal(err)
	}
	d := &sentOnlyRefund207{combineRefundDouble207{state: "SUCCESS", fail: map[string]error{"RF-resume-2": &provider.UnknownOutcome{Operation: "refund", Cause: provider.ErrAuthentication}}}}
	state, err := (ProviderFlow{Store: s}).combineRefund207(ctx, d, r.ID, plan, true)
	if state != "unknown" || err == nil || len(d.sent) != 2 {
		t.Fatal(state, err, d.sent)
	}
	plan, err = s.planCombineRefund207(ctx, r.ID, o.ID, 7000, "RF-resume")
	if err != nil {
		t.Fatal(err)
	}
	state, err = (ProviderFlow{Store: s}).combineRefund207(ctx, d, r.ID, plan, false)
	if err != nil || state != "pending" || len(d.sent) != 2 {
		t.Fatal("query must not send or query an unsent share", state, err, d.sent, d.queried)
	}
	plan, err = s.planCombineRefund207(ctx, r.ID, o.ID, 7000, "RF-resume")
	if err != nil {
		t.Fatal(err)
	}
	state, err = (ProviderFlow{Store: s}).combineRefund207(ctx, d, r.ID, plan, true)
	if err != nil || state != "succeeded" || len(d.sent) != 3 || d.sent[2].RefundNo != "RF-resume-3" {
		t.Fatal("unsent share was not resumed", state, err, d.sent, d.queried)
	}
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

func TestCombineRefund207UnknownStopsAndQuerySkipsUnsent(t *testing.T) {
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
	if err != nil || state != "pending" || len(d.queried) != 1 || d.queried[0].RefundNo != "RF9-2" {
		t.Fatal("query should only query submitted shares", state, err, d.queried)
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
