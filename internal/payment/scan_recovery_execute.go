package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	adapter "goepay/internal/adapters/payment"
)

// ExecuteScanRecovery handles at most one original query, followed only by the
// independently claimed cancellation after the sixth pending observation.
// Positive delay asks the caller to reschedule; zero means this chain is done.
// It never holds a database transaction while waiting for an upstream.
func (g ProviderFlow) ExecuteScanRecovery(ctx context.Context, jobID int64, token string) (time.Duration, error) {
	if !g.AllowLive || !g.ScanRecoveryEnabled {
		return 0, ErrLiveDisabled
	}
	if g.Store == nil || ctx == nil || ctx.Err() != nil {
		return 0, ErrInvalid
	}
	plan, e := g.Store.scanRecoveryExecutionPlan(ctx, jobID, token)
	if e != nil || plan.done {
		return 0, e
	}
	source := plan.source
	p := g.Providers[source.ChannelID]
	if p == nil || p.ID() != source.Provider {
		return 0, adapter.ErrUnsupported
	}
	if g.MerchantChannels != nil {
		allowed := false
		for _, id := range g.MerchantChannels[source.MerchantID] {
			allowed = allowed || id == source.ChannelID
		}
		if !allowed {
			return 0, ErrConflict
		}
	}
	scope, e := adapter.TransferAccountScope(p)
	if e != nil {
		return 0, e
	}
	if e = g.Store.checkProviderAccountScope(ctx, g.Store.pool, source.AttemptID, scope, true); e != nil {
		return 0, e
	}
	if plan.delay > 0 {
		return plan.delay, nil
	}
	if plan.cancel {
		return 0, g.executeScanCancellation(ctx, jobID, token, scope, source, p)
	}
	observer, ok := p.(adapter.ScanObservationProvider)
	if !ok {
		return 0, adapter.ErrUnsupported
	}
	claim, owner, e := g.Store.ClaimScanQuery(ctx, jobID, token, scope)
	if errors.Is(e, ErrScanQueryNotDue) {
		return time.Duration(source.QueryIntervalSeconds) * time.Second, nil
	}
	if e != nil || !owner {
		return 0, e
	}
	queryCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	result, queryErr := observer.ObserveScan(queryCtx, adapter.QueryRequest{TradeNo: claim.TradeNo, ProviderTradeNo: claim.ProviderTradeNo})
	stop()
	if e = g.Store.RecordScanQueryResult(ctx, claim.ID, token, result, queryErr); e != nil {
		return 0, e
	}
	if queryErr != nil {
		return 0, nil
	}
	switch result.State {
	case "SUCCESS", "TRADE_SUCCESS", "TRADE_FINISHED":
		if result.ProviderTradeNo == "" {
			return 0, adapter.ErrAuthentication
		}
		return 0, g.Store.markPaid(ctx, source.TradeNo, fmt.Sprintf("provider-scan-recovery:%d:%s", source.ChannelID, result.ProviderTradeNo), true, PaidReceipt{ChannelID: source.ChannelID, ProviderTradeNo: result.ProviderTradeNo, BuyerID: result.BuyerID})
	default:
		if !scanQueryPending(source.Provider, result.State) {
			return 0, nil
		}
		if claim.Sequence < source.MaximumQueries {
			return time.Duration(source.QueryIntervalSeconds) * time.Second, nil
		}
		return 0, g.executeScanCancellation(ctx, jobID, token, scope, source, p)
	}
}

func (g ProviderFlow) executeScanCancellation(ctx context.Context, jobID int64, token, scope string, source scanRecoveryOrigin, p adapter.Provider) error {
	canceller, ok := p.(adapter.ScanCancellationProvider)
	if !ok || !canceller.CanCancelScan() {
		return adapter.ErrUnsupported
	}
	if e := g.Store.checkChannelWrite(ctx, source.ChannelID); e != nil {
		return e
	}
	// Pure validation must precede claiming the one financial send. The claim
	// itself supplies and authenticates the final original provider number.
	if e := canceller.ValidateScanCancellation(adapter.ScanCancellationRequest{TradeNo: source.TradeNo, Amount: source.Amount}); e != nil {
		return e
	}
	claim, owner, e := g.Store.ClaimScanCancellation(ctx, jobID, token, scope)
	if e != nil || !owner {
		return e
	}
	request := adapter.ScanCancellationRequest{TradeNo: claim.TradeNo, ProviderTradeNo: claim.ProviderTradeNo, Amount: claim.Amount}
	cancelCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	result, cancelErr := canceller.CancelScan(cancelCtx, request)
	stop()
	// An already attempted cancellation must retain its original response even
	// if the HTTP caller or worker deadline was cancelled during network I/O.
	saveCtx, saveStop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer saveStop()
	return g.Store.RecordScanCancellation(saveCtx, claim.AttemptID, token, result, cancelErr)
}
