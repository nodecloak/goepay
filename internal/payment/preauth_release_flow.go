package payment

import (
	"context"
	"errors"
	"fmt"
	provider "goepay/internal/adapters/payment"
)

func authorizationTradeNotFound(e error) bool {
	var rejection *provider.ProviderError
	return errors.As(e, &rejection) && rejection.Code == "40004:ACQ.TRADE_NOT_EXIST"
}
func (g ProviderFlow) releasePending(ctx context.Context, merchantID int64, tradeNo, state string, cause error) (AuthorizationRelease, error) {
	if e := g.Store.ResolveAuthorizationRelease(ctx, tradeNo, state); e != nil {
		return AuthorizationRelease{}, errors.Join(cause, e)
	}
	r, e := g.Store.AuthorizationRelease(ctx, merchantID, tradeNo)
	if e != nil {
		return r, e
	}
	return r, &provider.UnknownOutcome{Operation: "authorization release", Cause: cause}
}
func (g ProviderFlow) verifiedReleasePayment(ctx context.Context, o Order, a Preauthorization, q provider.QueryResult) error {
	if q.TradeNo != o.TradeNo || q.Amount != o.Amount || q.Currency != "CNY" || q.ProviderTradeNo == "" {
		return provider.ErrAuthentication
	}
	return g.Store.markPaid(ctx, o.TradeNo, fmt.Sprintf("authorization-release-query:%d:%s", a.ChannelID, q.ProviderTradeNo), true, PaidReceipt{ChannelID: a.ChannelID, ProviderTradeNo: q.ProviderTradeNo, BuyerID: a.BuyerID})
}

// ReleaseAuthorization can send one full unfreeze only after independent signed
// queries prove the entire original authorization remains unused. A request to
// release immediately blocks future automatic capture, even while evidence is
// incomplete. It never returns buyer funds through a merchant ledger credit.
func (g ProviderFlow) ReleaseAuthorization(ctx context.Context, merchantID int64, tradeNo string) (AuthorizationRelease, error) {
	if !g.AllowLive {
		return AuthorizationRelease{}, ErrLiveDisabled
	}
	o, attempt, p, e := g.owned(ctx, merchantID, tradeNo)
	if e != nil {
		return AuthorizationRelease{}, e
	}
	if attempt.Method != "preauth" {
		return AuthorizationRelease{}, provider.ErrUnsupported
	}
	authProvider, ok := p.(provider.PreauthorizationProvider)
	if !ok {
		return AuthorizationRelease{}, provider.ErrUnsupported
	}
	r, owner, e := g.Store.BeginAuthorizationRelease(ctx, merchantID, tradeNo)
	if e != nil {
		return r, e
	}
	if !owner {
		return g.QueryAuthorizationRelease(ctx, merchantID, tradeNo)
	}
	a, e := g.Store.Preauthorization(ctx, tradeNo)
	if e != nil {
		return r, e
	}
	// A not-found payment alone cannot resolve an in-flight unknown capture.
	// Only a verified closed payment plus the full remaining authorization can
	// make an originally unknown capture eligible for the first unfreeze send.
	payment, queryErr := p.Query(ctx, provider.QueryRequest{TradeNo: tradeNo})
	if queryErr == nil && (payment.State == "SUCCESS" || payment.State == "TRADE_SUCCESS" || payment.State == "TRADE_FINISHED") {
		if e = g.verifiedReleasePayment(ctx, o, a, payment); e != nil {
			return g.releasePending(ctx, merchantID, tradeNo, "reversal_pending", e)
		}
		if e = g.Store.ResolveAuthorizationRelease(ctx, tradeNo, "failed"); e != nil {
			return r, e
		}
		r, e = g.Store.AuthorizationRelease(ctx, merchantID, tradeNo)
		if e != nil {
			return r, e
		}
		return r, ErrState
	}
	closed := queryErr == nil && payment.State == "TRADE_CLOSED" && payment.TradeNo == tradeNo && payment.Amount == o.Amount && payment.Currency == "CNY"
	if !closed && !authorizationTradeNotFound(queryErr) {
		return g.releasePending(ctx, merchantID, tradeNo, "reversal_pending", queryErr)
	}
	frozen, e := authProvider.QueryAuthorization(ctx, provider.AuthorizationRequest{TradeNo: tradeNo, RequestNo: tradeNo, AuthNo: a.AuthNo, Amount: a.Amount})
	if e != nil || frozen.TradeNo != tradeNo || frozen.RequestNo != tradeNo || frozen.AuthNo != a.AuthNo || frozen.BuyerID != a.BuyerID || frozen.Amount != a.Amount || !frozen.RestAmountKnown || frozen.RestAmount != a.Amount || frozen.State != "SUCCESS" || frozen.OrderStatus != "AUTHORIZED" || frozen.OperationType != "FREEZE" {
		return g.releasePending(ctx, merchantID, tradeNo, "reversal_pending", errors.Join(e, provider.ErrAuthentication))
	}
	if a.CaptureState == "unknown" || a.CaptureState == "processing" {
		if !closed {
			return g.releasePending(ctx, merchantID, tradeNo, "reversal_pending", ErrState)
		}
		if e = g.Store.RecordPreauthorizationCaptureState(ctx, tradeNo, "failed"); e != nil {
			return g.releasePending(ctx, merchantID, tradeNo, "reversal_pending", e)
		}
	}
	if e = g.Store.MarkAuthorizationReleaseSent(ctx, tradeNo); e != nil {
		return g.releasePending(ctx, merchantID, tradeNo, "reversal_pending", e)
	}
	_, e = authProvider.UnfreezeAuthorization(ctx, provider.AuthorizationRequest{TradeNo: tradeNo, RequestNo: r.RequestNo, AuthNo: r.AuthNo, Amount: r.Amount})
	if e != nil {
		var rejection *provider.ProviderError
		if errors.As(e, &rejection) && definiteProviderRejection(rejection) {
			if save := g.Store.ResolveAuthorizationRelease(ctx, tradeNo, "failed"); save != nil {
				return r, errors.Join(e, save)
			}
			r, _ = g.Store.AuthorizationRelease(ctx, merchantID, tradeNo)
			return r, e
		}
		return g.releasePending(ctx, merchantID, tradeNo, "unknown", e)
	}
	// A successful operation response alone does not prove the remaining frozen
	// balance. Confirm the exact UA operation and explicit zero remainder.
	if e = g.Store.ResolveAuthorizationRelease(ctx, tradeNo, "unknown"); e != nil {
		return r, e
	}
	return g.QueryAuthorizationRelease(ctx, merchantID, tradeNo)
}

// QueryAuthorizationRelease never calls an upstream write, including when the
// original process crashed before its recorded send boundary.
func (g ProviderFlow) QueryAuthorizationRelease(ctx context.Context, merchantID int64, tradeNo string) (AuthorizationRelease, error) {
	o, attempt, p, e := g.owned(ctx, merchantID, tradeNo)
	if e != nil {
		return AuthorizationRelease{}, e
	}
	if attempt.Method != "preauth" {
		return AuthorizationRelease{}, provider.ErrUnsupported
	}
	r, e := g.Store.AuthorizationRelease(ctx, merchantID, tradeNo)
	if e != nil {
		return r, e
	}
	if r.State == "succeeded" || r.State == "failed" {
		return r, nil
	}
	if !r.Sent {
		a, ae := g.Store.Preauthorization(ctx, tradeNo)
		if ae != nil {
			return r, ae
		}
		q, qe := p.Query(ctx, provider.QueryRequest{TradeNo: tradeNo})
		if qe == nil && (q.State == "SUCCESS" || q.State == "TRADE_SUCCESS" || q.State == "TRADE_FINISHED") {
			if e = g.verifiedReleasePayment(ctx, o, a, q); e != nil {
				return r, e
			}
			if e = g.Store.ResolveAuthorizationRelease(ctx, tradeNo, "failed"); e != nil {
				return r, e
			}
			return g.Store.AuthorizationRelease(ctx, merchantID, tradeNo)
		}
		return g.releasePending(ctx, merchantID, tradeNo, "reversal_pending", ErrState)
	}
	v, ok := p.(provider.PreauthorizationProvider)
	if !ok {
		return r, provider.ErrUnsupported
	}
	event, e := v.QueryAuthorization(ctx, provider.AuthorizationRequest{TradeNo: tradeNo, RequestNo: r.RequestNo, AuthNo: r.AuthNo, Amount: r.Amount})
	if e != nil {
		return g.releasePending(ctx, merchantID, tradeNo, "unknown", e)
	}
	if event.TradeNo != tradeNo || event.RequestNo != r.RequestNo || event.AuthNo != r.AuthNo || event.Amount != r.Amount || event.OperationType != "UNFREEZE" || !event.RestAmountKnown || event.RestAmount != 0 || event.State != "SUCCESS" {
		return g.releasePending(ctx, merchantID, tradeNo, "unknown", provider.ErrAuthentication)
	}
	if r.SourceRevision > 0 {
		a, ae := g.Store.Preauthorization(ctx, tradeNo)
		if ae != nil || event.BuyerID != a.BuyerID {
			return g.releasePending(ctx, merchantID, tradeNo, "unknown", errors.Join(ae, provider.ErrAuthentication))
		}
	}
	if e = g.Store.ResolveAuthorizationRelease(ctx, tradeNo, "succeeded"); e != nil {
		return r, e
	}
	return g.Store.AuthorizationRelease(ctx, merchantID, tradeNo)
}
