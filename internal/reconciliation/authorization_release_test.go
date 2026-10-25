package reconciliation

import (
	"context"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
	"net/http"
	"testing"
	"time"
)

type releaseQueryProvider struct {
	queryProvider
	reads, writes int
	failure       bool
	missingRest   bool
}

func (*releaseQueryProvider) ID() string { return "alipay" }
func (p *releaseQueryProvider) QueryAuthorization(_ context.Context, r adapter.AuthorizationRequest) (adapter.AuthorizationEvent, error) {
	p.reads++
	if p.failure {
		return adapter.AuthorizationEvent{}, errors.New("synthetic timeout")
	}
	return adapter.AuthorizationEvent{TradeNo: r.TradeNo, RequestNo: r.RequestNo, AuthNo: r.AuthNo, Amount: r.Amount, State: "SUCCESS", OperationType: "UNFREEZE", RestAmount: 0, RestAmountKnown: !p.missingRest}, nil
}
func (p *releaseQueryProvider) Freeze(context.Context, adapter.CreateRequest) (adapter.CreateResult, error) {
	p.writes++
	return adapter.CreateResult{}, adapter.ErrUnsupported
}
func (p *releaseQueryProvider) VerifyAuthorization(context.Context, http.Header, []byte, adapter.ExpectedPayment) (adapter.AuthorizationEvent, error) {
	return adapter.AuthorizationEvent{}, adapter.ErrUnsupported
}
func (p *releaseQueryProvider) CaptureAuthorization(context.Context, adapter.AuthorizationCaptureRequest) (adapter.PaymentEvent, error) {
	p.writes++
	return adapter.PaymentEvent{}, adapter.ErrUnsupported
}
func (p *releaseQueryProvider) UnfreezeAuthorization(context.Context, adapter.AuthorizationRequest) (adapter.AuthorizationEvent, error) {
	p.writes++
	return adapter.AuthorizationEvent{}, adapter.ErrUnsupported
}
func (p *releaseQueryProvider) CancelAuthorization(context.Context, adapter.AuthorizationRequest) (adapter.AuthorizationEvent, error) {
	p.writes++
	return adapter.AuthorizationEvent{}, adapter.ErrUnsupported
}

func TestAuthorizationReleaseWorkerFiltersChannelAndOnlyQueries(t *testing.T) {
	h, _, original := fixture(t)
	ctx := context.Background()
	store := h.Flow.Store
	o, e := store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "release-reconcile", Type: "alipay", Name: "test", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	a, _, e := store.BeginProviderAttempt(ctx, o.TradeNo, 7, "preauth")
	if e != nil {
		t.Fatal(e)
	}
	if e = store.RecordProviderAttemptResult(ctx, a.ID, "pending", "preauth", "synthetic-sdk", ""); e != nil {
		t.Fatal(e)
	}
	if e = store.RecordPreauthorization(ctx, 7, adapter.AuthorizationEvent{TradeNo: o.TradeNo, RequestNo: o.TradeNo, AuthNo: "auth-test", BuyerID: "2088000000000000", Amount: 100, State: "SUCCESS"}); e != nil {
		t.Fatal(e)
	}
	if _, owner, e := store.BeginAuthorizationRelease(ctx, 1, o.TradeNo); e != nil || !owner {
		t.Fatal(owner, e)
	}
	if e = store.MarkAuthorizationReleaseSent(ctx, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	p := &releaseQueryProvider{failure: true}
	h.Flow.Providers[7] = p
	h.Flow.AllowLive = false
	if _, e = h.Pool.Exec(ctx, "UPDATE jobs SET run_at=now() WHERE kind='authorization.release.reconcile'"); e != nil {
		t.Fatal(e)
	}
	queue := jobs.Queue{Pool: h.Pool}
	kinds := []string{"authorization.release.reconcile"}
	if _, e = queue.Claim(ctx, []string{"payment.notify"}, time.Minute); !jobs.IsEmpty(e) {
		t.Fatal("default notification worker claimed authorization task", e)
	}
	if _, e = queue.ClaimProviderChannels(ctx, kinds, time.Minute, []int64{8}); !jobs.IsEmpty(e) {
		t.Fatal("wrong channel claimed release task", e)
	}
	j, e := queue.ClaimProviderChannels(ctx, kinds, time.Minute, []int64{7})
	if e != nil {
		t.Fatal("configured channel could not claim release task", e)
	}
	if e = h.Handle(ctx, j); e == nil || p.reads != 1 {
		t.Fatal("unknown release acknowledged", p.reads, e)
	}
	if e = queue.Retry(ctx, j, "query unresolved", 0); e != nil {
		t.Fatal(e)
	}
	p.failure = false
	p.missingRest = true
	j, e = queue.ClaimProviderChannels(ctx, kinds, time.Minute, []int64{7})
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Handle(ctx, j); e == nil {
		t.Fatal("missing zero remainder accepted")
	}
	if e = queue.Retry(ctx, j, "query unresolved", 0); e != nil {
		t.Fatal(e)
	}
	p.missingRest = false
	j, e = queue.ClaimProviderChannels(ctx, kinds, time.Minute, []int64{7})
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Handle(ctx, j); e != nil {
		t.Fatal(e)
	}
	if e = queue.Complete(ctx, j); e != nil {
		t.Fatal(e)
	}
	if e = h.Handle(ctx, j); e != nil {
		t.Fatal("terminal task not idempotent", e)
	}
	if p.reads != 3 || p.writes != 0 || p.creates != 0 || p.refunds != 0 || p.transfers != 0 {
		t.Fatal("reconciliation wrote or repeated query", p)
	}
	current, _ := store.OrderByTradeNo(ctx, o.TradeNo)
	balance, _ := store.Balance(ctx, 1)
	if current.Status != "closed" || balance.Available != 0 || balance.Reserved != 0 {
		t.Fatal("buyer release created merchant funds", current.Status, balance)
	}
	if e = h.Handle(ctx, job("authorization.release.reconcile", map[string]any{"trade_no": original.TradeNo})); e == nil {
		t.Fatal("nonauthorization order acknowledged")
	}
}
