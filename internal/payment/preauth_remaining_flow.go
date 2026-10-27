package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

// ReleaseRemainingAuthorization consumes a separate confirmation for an exact
// recent observation. The observation alone is never a funds authorization.
func (g ProviderFlow) ReleaseRemainingAuthorization(ctx context.Context, mid int64, trade string, in PreauthRemainingReleaseInput, token, role string, check func(context.Context) error, checks ...func(context.Context, pgx.Tx) error) (AuthorizationRelease, error) {
	var r AuthorizationRelease
	if !g.paymentWritesEnabled() {
		return r, ErrLiveDisabled
	}
	checkTx, checkErr := observationCheckTx(checks)
	if g.Store == nil || check == nil || checkErr != nil {
		return r, ErrInvalid
	}
	q, e := g.ReadPreauthorizationObservation(ctx, in.ExpectedObservationID, token, role, check, checkTx)
	if e != nil || q.MerchantID != mid || q.TradeNo != trade {
		return r, ErrPreauthObservation
	}
	if e = g.observationGuard(ctx, q, check, checkTx); e != nil {
		return r, e
	}
	o, attempt, p, e := g.owned(ctx, mid, trade)
	if e != nil {
		return r, e
	}
	if attempt.ID != q.AttemptID || attempt.ChannelID != q.ChannelID {
		return r, ErrConflict
	}
	v, _, _, e := observationProvider(p)
	if e != nil {
		return r, e
	}
	// This source, its audit and recovery job commit before the first query.
	r, e = g.Store.bindPreauthRemainingRelease(ctx, mid, trade, in, token, role)
	if e != nil {
		return r, e
	}
	a, e := g.Store.Preauthorization(ctx, trade)
	if e != nil {
		return r, e
	}
	if e = g.observationGuard(ctx, q, check, checkTx); e != nil {
		return g.releasePending(ctx, mid, trade, "reversal_pending", e)
	}
	payment, queryErr := p.Query(ctx, adapter.QueryRequest{TradeNo: trade})
	if queryErr == nil && (payment.State == "SUCCESS" || payment.State == "TRADE_SUCCESS" || payment.State == "TRADE_FINISHED") {
		// A smaller original payment is not a full order receipt and cannot be
		// credited as one. It remains blocked for explicit reconciliation.
		if e = g.verifiedReleasePayment(ctx, o, a, payment); e != nil {
			return g.releasePending(ctx, mid, trade, "reversal_pending", e)
		}
		if e = g.Store.ResolveAuthorizationRelease(ctx, trade, "failed"); e != nil {
			return r, e
		}
		r, e = g.Store.AuthorizationRelease(ctx, mid, trade)
		if e != nil {
			return r, e
		}
		return r, ErrState
	}
	charge104, e := g.Store.AuthenticatedOrderCharge104(ctx, o)
	if e != nil {
		return r, e
	}
	closed := queryErr == nil && payment.State == "TRADE_CLOSED" && payment.TradeNo == trade && payment.Amount == charge104 && payment.Currency == "CNY"
	if !closed && !authorizationTradeNotFound(queryErr) {
		return g.releasePending(ctx, mid, trade, "reversal_pending", queryErr)
	}
	if e = g.observationGuard(ctx, q, check, checkTx); e != nil {
		return g.releasePending(ctx, mid, trade, "reversal_pending", e)
	}
	frozen, e := v.QueryAuthorization(ctx, adapter.AuthorizationRequest{TradeNo: trade, RequestNo: trade, AuthNo: a.AuthNo, Amount: a.Amount})
	if e != nil || frozen.TradeNo != trade || frozen.RequestNo != trade || frozen.AuthNo != a.AuthNo || frozen.BuyerID != a.BuyerID || frozen.Amount != a.Amount || !frozen.RestAmountKnown || frozen.RestAmount != r.Amount || frozen.State != "SUCCESS" || frozen.OrderStatus != "AUTHORIZED" || frozen.OperationType != "FREEZE" {
		return g.releasePending(ctx, mid, trade, "reversal_pending", errors.Join(e, adapter.ErrAuthentication))
	}
	if a.CaptureState == "unknown" || a.CaptureState == "processing" {
		if !closed {
			return g.releasePending(ctx, mid, trade, "reversal_pending", ErrState)
		}
		if e = g.Store.RecordPreauthorizationCaptureState(ctx, trade, "failed"); e != nil {
			return g.releasePending(ctx, mid, trade, "reversal_pending", e)
		}
	}
	guardTx := func(c context.Context, tx pgx.Tx) error { return g.observationGuardTx(c, tx, q, checkTx) }
	r, e = g.Store.markPreauthRemainingSent(ctx, trade, r.SourceRevision, guardTx)
	if e != nil {
		return g.releasePending(ctx, mid, trade, "reversal_pending", e)
	}
	// Losing permission after claiming does not reopen the send boundary.
	if e = g.observationGuard(ctx, q, check, checkTx); e != nil {
		return g.releasePending(ctx, mid, trade, "unknown", e)
	}
	_, e = v.UnfreezeAuthorization(ctx, adapter.AuthorizationRequest{TradeNo: trade, RequestNo: r.RequestNo, AuthNo: r.AuthNo, Amount: r.Amount})
	if e != nil {
		var rejection *adapter.ProviderError
		if errors.As(e, &rejection) && definiteProviderRejection(rejection) {
			if save := g.Store.ResolveAuthorizationRelease(ctx, trade, "failed"); save != nil {
				return r, errors.Join(e, save)
			}
			r, save := g.Store.AuthorizationRelease(ctx, mid, trade)
			return r, errors.Join(e, save)
		}
		return g.releasePending(ctx, mid, trade, "unknown", e)
	}
	if e = g.Store.ResolveAuthorizationRelease(ctx, trade, "unknown"); e != nil {
		return r, e
	}
	return g.QueryAuthorizationRelease(ctx, mid, trade)
}
