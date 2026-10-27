package payment

import (
	"context"
	"errors"
	"fmt"
	provider "goepay/internal/adapters/payment"
	"strings"
)

type preauthorizationCaptureResult struct {
	Authorization Preauthorization
	ExternalError error
}

// captureClaimedPreauthorization is shared by automatic notifications and the
// explicit console action. Both callers must own BeginPreauthorizationCapture.
// A persisted provider failure is separate from a storage failure: the legacy
// automatic callback may ACK the former, but must never ACK the latter.
func (g ProviderFlow) captureClaimedPreauthorization(ctx context.Context, o Order, a Preauthorization, p provider.PreauthorizationProvider) (preauthorizationCaptureResult, error) {
	finish := func(state string, external error) (preauthorizationCaptureResult, error) {
		save := g.Store.RecordPreauthorizationCaptureState(ctx, o.TradeNo, state)
		current, readErr := g.Store.Preauthorization(ctx, o.TradeNo)
		return preauthorizationCaptureResult{Authorization: current, ExternalError: external}, errors.Join(save, readErr)
	}
	if e := g.Store.checkChannelWrite(ctx, a.ChannelID); e != nil {
		return finish("failed", e)
	}
	if e := g.checkParentOrderWrite(ctx, o.MerchantID, o.ID, a.ChannelID); e != nil {
		return finish("failed", e)
	}
	base, ok := p.(provider.Provider)
	if !ok {
		return finish("unknown", provider.ErrUnsupported)
	}
	attempt, e := g.Store.ProviderAttempt(ctx, o.TradeNo)
	if e != nil {
		return finish("unknown", e)
	}
	if e = g.Store.checkProviderAccount(ctx, g.Store.pool, attempt.ID, base); e != nil {
		return finish("unknown", e)
	}
	subject, e := g.upstreamSubject(ctx, o, a.ChannelID)
	if e != nil {
		return finish("unknown", e)
	}
	charge104, e := g.Store.AuthenticatedOrderCharge104(ctx, o)
	if e != nil {
		return finish("unknown", e)
	}
	event, e := p.CaptureAuthorization(ctx, provider.AuthorizationCaptureRequest{TradeNo: o.TradeNo, AuthNo: a.AuthNo, BuyerID: a.BuyerID, Subject: subject, Amount: charge104, NotifyURL: strings.TrimRight(g.CallbackBaseURL, "/") + fmt.Sprintf("/callbacks/providers/%d/%s", a.ChannelID, o.TradeNo)})
	if e != nil {
		state := "unknown"
		var rejection *provider.ProviderError
		if errors.As(e, &rejection) && definiteProviderRejection(rejection) {
			state = "failed"
		}
		return finish(state, e)
	}
	if event.TradeNo != o.TradeNo || event.Amount != charge104 || event.Currency != "CNY" || event.State != "SUCCESS" || event.EventID == "" || event.ProviderTradeNo == "" || event.BuyerID != a.BuyerID {
		return finish("unknown", &provider.UnknownOutcome{Operation: "authorization capture", Cause: provider.ErrAuthentication})
	}
	if e = g.Store.markPaid(ctx, o.TradeNo, fmt.Sprintf("preauth-capture:%d:%s", a.ChannelID, event.EventID), true, PaidReceipt{RealMoney: event.Amount, ChannelID: a.ChannelID, ProviderTradeNo: event.ProviderTradeNo, BuyerID: event.BuyerID}); e != nil {
		result, save := finish("unknown", nil)
		return result, errors.Join(e, save)
	}
	current, e := g.Store.Preauthorization(ctx, o.TradeNo)
	return preauthorizationCaptureResult{Authorization: current}, e
}
