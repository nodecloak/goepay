package payment

import (
	"context"
	"errors"
	"fmt"
	provider "goepay/internal/adapters/payment"
)

// CapturePreauthorization is an explicit first-capture action. Its durable
// processing state is a send barrier, including a crash before actual I/O.
func (g ProviderFlow) CapturePreauthorization(ctx context.Context, merchantID int64, tradeNo string) (Preauthorization, error) {
	return g.CapturePreauthorizationChecked(ctx, merchantID, tradeNo, nil)
}

// CapturePreauthorizationChecked lets the authenticated console recheck its
// current channel/policy permissions immediately before the financial send.
func (g ProviderFlow) CapturePreauthorizationChecked(ctx context.Context, merchantID int64, tradeNo string, check func(context.Context) error) (Preauthorization, error) {
	if !g.paymentWritesEnabled() {
		return Preauthorization{}, ErrLiveDisabled
	}
	if g.Store == nil || !validProviderOrigin(g.CallbackBaseURL, g.LocalSimulation) {
		return Preauthorization{}, ErrInvalid
	}
	o, attempt, p, e := g.owned(ctx, merchantID, tradeNo)
	if e != nil {
		return Preauthorization{}, e
	}
	if o.Type != "alipay" || attempt.Method != "preauth" {
		return Preauthorization{}, provider.ErrUnsupported
	}
	v, ok := p.(provider.PreauthorizationProvider)
	if !ok {
		return Preauthorization{}, provider.ErrUnsupported
	}
	capable := false
	for _, method := range p.Capabilities().Methods {
		if method == "preauth" {
			capable = true
		}
	}
	if !capable || !p.Capabilities().Query {
		return Preauthorization{}, provider.ErrUnsupported
	}
	a, e := g.Store.Preauthorization(ctx, tradeNo)
	if e != nil {
		return a, e
	}
	charge104, e := g.Store.AuthenticatedOrderCharge104(ctx, o)
	if e != nil {
		return a, e
	}
	if a.CaptureState != "none" || a.ReleaseState != "none" || a.ChannelID != attempt.ChannelID || a.Amount != charge104 || o.Status == "paid" || o.Status == "closed" || o.Status == "failed" {
		return a, ErrState
	}
	enabled := func() error {
		if e := g.checkParentOrderWrite(ctx, merchantID, o.ID, attempt.ChannelID); e != nil {
			return e
		}
		if e := g.Store.checkChannelWrite(ctx, attempt.ChannelID); e != nil {
			return e
		}
		var active bool
		e := g.Store.pool.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", merchantID).Scan(&active)
		if e != nil {
			return e
		}
		if !active {
			return ErrState
		}
		if check != nil {
			return check(ctx)
		}
		return nil
	}
	if e = enabled(); e != nil {
		return a, e
	}
	payment, queryErr := p.Query(ctx, provider.QueryRequest{TradeNo: tradeNo})
	if queryErr == nil && (payment.State == "SUCCESS" || payment.State == "TRADE_SUCCESS" || payment.State == "TRADE_FINISHED") {
		if payment.TradeNo != tradeNo || payment.Amount != charge104 || payment.Currency != "CNY" || payment.ProviderTradeNo == "" || payment.BuyerID != a.BuyerID {
			return a, provider.ErrAuthentication
		}
		if e = g.Store.markPaid(ctx, tradeNo, fmt.Sprintf("manual-preauth-query:%d:%s", a.ChannelID, payment.ProviderTradeNo), true, PaidReceipt{RealMoney: payment.Amount, ChannelID: a.ChannelID, ProviderTradeNo: payment.ProviderTradeNo, BuyerID: payment.BuyerID}); e != nil {
			return a, e
		}
		return g.Store.Preauthorization(ctx, tradeNo)
	}
	// Neither a timeout nor an ordinary unpaid/closed trade proves this stable
	// payment number has never been used. Only authenticated exact not-found does.
	if !authorizationTradeNotFound(queryErr) {
		if queryErr != nil {
			return a, queryErr
		}
		return a, ErrState
	}
	frozen, e := v.QueryAuthorization(ctx, provider.AuthorizationRequest{TradeNo: tradeNo, RequestNo: tradeNo, AuthNo: a.AuthNo, Amount: a.Amount})
	if e != nil {
		return a, e
	}
	if frozen.TradeNo != tradeNo || frozen.RequestNo != tradeNo || frozen.AuthNo != a.AuthNo || frozen.BuyerID != a.BuyerID || frozen.Amount != a.Amount || !frozen.RestAmountKnown || frozen.RestAmount != a.Amount || frozen.State != "SUCCESS" || frozen.OrderStatus != "AUTHORIZED" || frozen.OperationType != "FREEZE" {
		return a, provider.ErrAuthentication
	}
	if e = enabled(); e != nil {
		return a, e
	}
	claimed, owner, e := g.Store.BeginPreauthorizationCapture(ctx, tradeNo, g.parentOrderWriteGuard(merchantID, o.ID, attempt.ChannelID))
	if e != nil {
		return claimed, e
	}
	if !owner {
		return claimed, ErrState
	}
	if e = enabled(); e != nil {
		save := g.Store.RecordPreauthorizationCaptureState(ctx, tradeNo, "failed")
		current, readErr := g.Store.Preauthorization(ctx, tradeNo)
		return current, errors.Join(e, save, readErr)
	}
	result, save := g.captureClaimedPreauthorization(ctx, o, claimed, v)
	return result.Authorization, errors.Join(result.ExternalError, save)
}
