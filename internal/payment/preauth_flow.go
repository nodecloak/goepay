package payment

import (
	"context"
	"errors"
	provider "goepay/internal/adapters/payment"
	"net/http"
	"strings"
)

// SubmitPreauthorization creates signed buyer authorization material. It never
// credits an account and is separate from ordinary payment submission.
func (g ProviderFlow) SubmitPreauthorization(ctx context.Context, tradeNo string, channelID int64, clientIP, openID string) (ProviderAttempt, error) {
	if !g.AllowLive {
		return ProviderAttempt{}, ErrLiveDisabled
	}
	if g.Store == nil || !validProviderOrigin(g.CallbackBaseURL, g.LocalSimulation) {
		return ProviderAttempt{}, ErrInvalid
	}
	if e := g.Store.checkChannelWrite(ctx, channelID); e != nil {
		return ProviderAttempt{}, e
	}
	p, ok := g.Providers[channelID].(provider.PreauthorizationProvider)
	if !ok {
		return ProviderAttempt{}, provider.ErrUnsupported
	}
	o, e := g.Store.OrderByTradeNo(ctx, tradeNo)
	if e != nil {
		return ProviderAttempt{}, e
	}
	if o.Type != "alipay" || g.Providers[channelID].ID() != o.Type {
		return ProviderAttempt{}, ErrConflict
	}
	subject, e := g.upstreamSubject(ctx, o, channelID)
	if e != nil {
		return ProviderAttempt{}, e
	}
	request := provider.CreateRequest{TradeNo: tradeNo, Subject: subject, Method: "preauth", Amount: o.Amount, ClientIP: clientIP, OpenID: openID, NotifyURL: strings.TrimRight(g.CallbackBaseURL, "/") + "/pay/preauthnotify/" + tradeNo + "/"}
	if v, ok := g.Providers[channelID].(provider.CreateValidator); ok {
		if e = v.ValidateCreate(request); e != nil {
			return ProviderAttempt{}, e
		}
	}
	appID := ""
	if identity, ok := g.Providers[channelID].(provider.ApplicationIdentity); ok {
		appID = identity.ApplicationID()
	}
	accountScope, e := provider.TransferAccountScope(g.Providers[channelID])
	if e != nil {
		return ProviderAttempt{}, e
	}
	a, owner, e := g.Store.BeginProviderAttemptWithAccount(ctx, tradeNo, channelID, "preauth", appID, openID, accountScope)
	if e != nil || !owner {
		return a, e
	}
	result, e := p.Freeze(ctx, request)
	if e != nil {
		state := "unknown"
		var rejection *provider.ProviderError
		if errors.As(e, &rejection) && definiteProviderRejection(rejection) {
			state = "failed"
		}
		save := g.Store.RecordProviderAttemptResult(ctx, a.ID, state, "", "", "")
		return a, errors.Join(e, save)
	}
	if (result.MaterialType != "preauth" && result.MaterialType != "qrcode") || result.Material == "" || result.ProviderTradeNo != "" {
		g.Store.RecordProviderAttemptResult(ctx, a.ID, "unknown", "", "", "")
		return a, &provider.UnknownOutcome{Operation: "freeze material", Cause: provider.ErrAuthentication}
	}
	if e = g.Store.RecordProviderAttemptResult(ctx, a.ID, "pending", result.MaterialType, result.Material, ""); e != nil {
		return a, e
	}
	return g.Store.ProviderAttempt(ctx, tradeNo)
}

// HandlePreauthorizationNotification stores authorization first and then claims
// exactly one capture intent. Duplicate callbacks acknowledge the original
// durable result; none may retry an unknown or failed external capture.
func (g ProviderFlow) HandlePreauthorizationNotification(ctx context.Context, channelID int64, tradeNo string, headers http.Header, raw []byte) error {
	if g.Store == nil {
		return ErrInvalid
	}
	p, ok := g.Providers[channelID].(provider.PreauthorizationProvider)
	if !ok {
		return provider.ErrUnsupported
	}
	a, e := g.Store.ProviderAttempt(ctx, tradeNo)
	if e != nil {
		return e
	}
	if a.ChannelID != channelID || a.Method != "preauth" {
		return ErrConflict
	}
	if e = g.Store.checkProviderAccount(ctx, g.Store.pool, a.ID, g.Providers[channelID]); e != nil {
		return e
	}
	o, e := g.Store.OrderByTradeNo(ctx, tradeNo)
	if e != nil {
		return e
	}
	event, e := p.VerifyAuthorization(ctx, headers, raw, provider.ExpectedPayment{TradeNo: tradeNo, Amount: o.Amount})
	if e != nil {
		return e
	}
	if event.TradeNo != tradeNo || event.Amount != o.Amount || event.State != "SUCCESS" {
		return provider.ErrAuthentication
	}
	if e = g.Store.RecordPreauthorization(ctx, channelID, event); e != nil {
		return e
	}
	// A stopped writer still preserves verified buyer authorization for status 4.
	if !g.AllowLive {
		return nil
	}
	if e = g.Store.checkChannelWrite(ctx, channelID); e != nil {
		if errors.Is(e, ErrChannelStopped) {
			return nil
		}
		return e
	}
	if !validProviderOrigin(g.CallbackBaseURL, g.LocalSimulation) {
		return ErrInvalid
	}
	authorization, owner, e := g.Store.BeginPreauthorizationCapture(ctx, tradeNo)
	if e != nil || !owner {
		return e
	}
	_, e = g.captureClaimedPreauthorization(ctx, o, authorization, p)
	return e
}

// RefreshPreauthorization recovers a lost freeze notification with the original
// stable operation number. Queries may persist evidence, but never capture.
func (g ProviderFlow) RefreshPreauthorization(ctx context.Context, tradeNo string, channelID int64) error {
	if g.Store == nil {
		return ErrInvalid
	}
	existing, e := g.Store.Preauthorization(ctx, tradeNo)
	if e == nil {
		if existing.ChannelID != channelID {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(e, ErrNotFound) {
		return e
	}
	attempt, e := g.Store.ProviderAttempt(ctx, tradeNo)
	if e != nil {
		return e
	}
	if attempt.ChannelID != channelID || attempt.Method != "preauth" {
		return ErrConflict
	}
	if e = g.Store.checkProviderAccount(ctx, g.Store.pool, attempt.ID, g.Providers[channelID]); e != nil {
		return e
	}
	p, ok := g.Providers[channelID].(provider.PreauthorizationProvider)
	if !ok {
		return provider.ErrUnsupported
	}
	o, e := g.Store.OrderByTradeNo(ctx, tradeNo)
	if e != nil {
		return e
	}
	event, e := p.QueryAuthorization(ctx, provider.AuthorizationRequest{TradeNo: tradeNo, RequestNo: tradeNo, Amount: o.Amount})
	if e != nil {
		return e
	}
	if event.TradeNo != tradeNo || event.RequestNo != tradeNo || event.Amount != o.Amount || event.State != "SUCCESS" || (event.OrderStatus != "AUTHORIZED" && event.OrderStatus != "FINISH") {
		return provider.ErrAuthentication
	}
	return g.Store.RecordPreauthorization(ctx, channelID, event)
}
