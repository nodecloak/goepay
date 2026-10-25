package payment

import (
	"context"
	"errors"
	"fmt"
	provider "goepay/internal/adapters/payment"
	"net/url"
	"strings"
)

var ErrScanPending = errors.New("scan outcome requires authenticated query")

func (g ProviderFlow) scanRequest(channelID int64, r provider.ScanRequest) (provider.ScanProvider, provider.ScanRequest, error) {
	if !g.AllowLive {
		return nil, r, ErrLiveDisabled
	}
	p := g.Providers[channelID]
	if p == nil {
		return nil, r, provider.ErrUnsupported
	}
	supported := false
	for _, method := range p.Capabilities().Methods {
		if method == "scan" {
			supported = true
		}
	}
	if !supported || !p.Capabilities().Query {
		return nil, r, provider.ErrUnsupported
	}
	scanner, ok := p.(provider.ScanProvider)
	if !ok {
		return nil, r, provider.ErrUnsupported
	}
	validator, ok := p.(provider.ScanValidator)
	if !ok {
		return nil, r, provider.ErrUnsupported
	}
	if !validProviderOrigin(g.CallbackBaseURL, g.LocalSimulation) {
		return nil, r, ErrInvalid
	}
	r.NotifyURL = strings.TrimRight(g.CallbackBaseURL, "/") + fmt.Sprintf("/callbacks/providers/%d/%s", channelID, url.PathEscape(r.TradeNo))
	if e := validator.ValidateScan(r); e != nil {
		return nil, r, e
	}
	if g.ScanRecoveryEnabled {
		canceller, ok := p.(provider.ScanCancellationProvider)
		if !ok || !canceller.CanCancelScan() {
			return nil, r, provider.ErrUnsupported
		}
		if _, ok := p.(provider.ScanObservationProvider); !ok {
			return nil, r, provider.ErrUnsupported
		}
		if e := canceller.ValidateScanCancellation(provider.ScanCancellationRequest{TradeNo: r.TradeNo, Amount: r.Amount}); e != nil {
			return nil, r, e
		}
	}
	return scanner, r, nil
}

// ValidateScanRequest performs pure validation before allocating an order or
// external intent. The request TradeNo may be a nonempty preflight placeholder.
func (g ProviderFlow) ValidateScanRequest(channelID int64, r provider.ScanRequest) error {
	_, _, e := g.scanRequest(channelID, r)
	return e
}

// SubmitScan grants one send opportunity per order, including after timeouts or
// process crashes. Only an HMAC of the bearer code is persisted by the existing
// immutable request-identity mechanism. Subsequent recovery calls Query only.
func (g ProviderFlow) SubmitScan(ctx context.Context, tradeNo string, channelID int64, clientIP, authCode string) (Order, error) {
	if g.Store == nil {
		return Order{}, ErrInvalid
	}
	if e := g.Store.checkChannelWrite(ctx, channelID); e != nil {
		return Order{}, e
	}
	o, e := g.Store.OrderByTradeNo(ctx, tradeNo)
	if e != nil {
		return o, e
	}
	p := g.Providers[channelID]
	if p == nil || p.ID() != o.Type {
		return o, provider.ErrUnsupported
	}
	subject, e := g.upstreamSubject(ctx, o, channelID)
	if e != nil {
		return o, e
	}
	appID := ""
	if identity, ok := p.(provider.ApplicationIdentity); ok {
		appID = identity.ApplicationID()
	}
	accountScope, e := provider.TransferAccountScope(p)
	if e != nil {
		return o, e
	}
	sharing, e := g.prepareSharingPayment(ctx, o, channelID, "scan", appID, accountScope)
	if e != nil {
		return o, e
	}
	scanner, request, e := g.scanRequest(channelID, provider.ScanRequest{TradeNo: tradeNo, Subject: subject, Amount: o.Amount, ClientIP: clientIP, AuthCode: authCode, ProfitSharing: sharing.Declared})
	if e != nil {
		return o, e
	}
	var attempt ProviderAttempt
	var owner bool
	recoverOriginal := g.ScanRecoveryEnabled
	if recoverOriginal {
		// Enabling this policy never upgrades an already submitted V0 order.
		// Its original identity/account checks still decide the idempotent replay;
		// a forged V1 downgrade cannot pass the legacy scope-only account MAC.
		var legacy bool
		e = g.Store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_attempts p LEFT JOIN provider_attempt_accounts a ON a.attempt_id=p.id WHERE p.order_id=$1 AND COALESCE((to_jsonb(a)->>'recovery_version')::int,0)=0)`, o.ID).Scan(&legacy)
		if e != nil {
			return o, e
		}
		recoverOriginal = !legacy
	}
	if recoverOriginal {
		attempt, owner, e = g.Store.beginRecoverableScanAttempt(ctx, tradeNo, channelID, appID, authCode, accountScope, &sharing)
	} else {
		attempt, owner, e = g.Store.beginProviderAttemptWithIdentity(ctx, tradeNo, channelID, "scan", appID, authCode, accountScope, &sharing)
	}
	if e != nil {
		return o, e
	}
	if !owner {
		current, e := g.Store.OrderByTradeNo(ctx, tradeNo)
		if e != nil {
			return current, e
		}
		if current.Status == "paid" {
			return current, nil
		}
		return current, ErrScanPending
	}
	event, e := scanner.Scan(ctx, request)
	if e != nil {
		state := "unknown"
		var rejection *provider.ProviderError
		if errors.As(e, &rejection) && definiteProviderRejection(rejection) {
			state = "failed"
		}
		if save := g.Store.RecordProviderAttemptResult(ctx, attempt.ID, state, "", "", ""); save != nil {
			return o, errors.Join(e, save)
		}
		if recoverOriginal {
			if _, eligible := provider.VerifiedScanPending(e); eligible {
				if save := g.Store.RecordScanPending(ctx, attempt.ID, appID, request, e); save != nil {
					return o, errors.Join(e, save)
				}
			}
		}
		current, _ := g.Store.OrderByTradeNo(ctx, tradeNo)
		return current, e
	}
	if event.State != "SUCCESS" || event.TradeNo != tradeNo || event.Amount != o.Amount || event.Currency != "CNY" || event.ProviderTradeNo == "" {
		save := g.Store.RecordProviderAttemptResult(ctx, attempt.ID, "unknown", "", "", "")
		return o, &provider.UnknownOutcome{Operation: "scan", Cause: errors.Join(provider.ErrAuthentication, save)}
	}
	if e = g.Store.RecordProviderAttemptResult(ctx, attempt.ID, "pending", "", "", event.ProviderTradeNo); e != nil {
		return o, &provider.UnknownOutcome{Operation: "persist scan result", Cause: e}
	}
	if e = g.Store.markPaid(ctx, tradeNo, fmt.Sprintf("provider-scan:%d:%s", channelID, event.ProviderTradeNo), true, PaidReceipt{ChannelID: channelID, ProviderTradeNo: event.ProviderTradeNo, BuyerID: event.BuyerID}); e != nil {
		return o, &provider.UnknownOutcome{Operation: "settle scan", Cause: e}
	}
	return g.Store.OrderByTradeNo(ctx, tradeNo)
}
