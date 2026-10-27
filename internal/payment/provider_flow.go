package payment

import (
	"context"
	"errors"
	"fmt"
	provider "goepay/internal/adapters/payment"
	"net"
	"net/http"
	"net/url"
	"strings"
)

var ErrLiveDisabled = errors.New("live financial operations disabled")

// ProviderFlow persists an attempt before external I/O. It deliberately never
// retries an attempt whose outcome is unknown. Operators must reconcile it.
type ProviderFlow struct {
	SubchannelFactories map[int64]provider.SubchannelFactory
	boundParents        map[int64]RuntimeChannel
	// ParentFactories are process-local trusted constructors, never request JSON.
	ParentFactories        map[int64]provider.ParentAccountFactory
	parentBindings         map[int64]*parentExecutionBinding
	parentLive             func() bool
	parentOperator         func(int64, int64) bool
	ChannelAutoStopEnabled bool
	// Original barcode recovery can cancel a payment. It requires its own
	// explicit execution policy in addition to the ordinary provider write gate.
	ScanRecoveryEnabled bool
	// Explicit runtime merchant admission is mandatory for unattended payouts.
	// Ordinary HTTP paths retain their existing server-side admission checks.
	MerchantChannels map[int64][]int64
	Store            *Store
	Providers        map[int64]provider.Provider
	AllowLive        bool
	CallbackBaseURL  string
	ReturnURLBuilder func(string) string
	SubjectResolver  func(context.Context, Order, int64) (string, error)
	LocalSimulation  bool
}

func (g ProviderFlow) Submit(ctx context.Context, tradeNo string, channelID int64, method, clientIP, openID string) (ProviderAttempt, error) {
	if method == "preauth" || method == "scan" || method == "applet" {
		return ProviderAttempt{}, provider.ErrUnsupported
	}
	if !g.paymentWritesEnabled() {
		return ProviderAttempt{}, ErrLiveDisabled
	}
	if g.Store == nil {
		return ProviderAttempt{}, ErrInvalid
	}
	var resolveErr error
	g, resolveErr = g.forPaymentTrade(ctx, tradeNo, channelID)
	if resolveErr != nil {
		return ProviderAttempt{}, resolveErr
	}
	if e := g.Store.checkChannelWrite(ctx, channelID); e != nil {
		return ProviderAttempt{}, e
	}
	p := g.Providers[channelID]
	if p == nil {
		return ProviderAttempt{}, provider.ErrUnsupported
	}
	supported := false
	for _, m := range p.Capabilities().Methods {
		if m == method {
			supported = true
		}
	}
	if !supported {
		return ProviderAttempt{}, provider.ErrUnsupported
	}
	base, e := url.Parse(g.CallbackBaseURL)
	if e != nil || !validProviderOrigin(g.CallbackBaseURL, g.LocalSimulation) {
		return ProviderAttempt{}, ErrInvalid
	}
	o, e := g.Store.OrderByTradeNo(ctx, tradeNo)
	if e != nil {
		return ProviderAttempt{}, e
	}
	if o.Type != p.ID() {
		return ProviderAttempt{}, ErrConflict
	}
	notify := strings.TrimRight(base.String(), "/") + fmt.Sprintf("/callbacks/providers/%d/%s", channelID, url.PathEscape(tradeNo))
	providerReturn := o.ReturnURL
	if g.ReturnURLBuilder != nil {
		providerReturn = g.ReturnURLBuilder(tradeNo)
	}
	subject, e := g.upstreamSubject(ctx, o, channelID)
	if e != nil {
		return ProviderAttempt{}, e
	}
	charge104, e := g.Store.AuthenticatedOrderCharge104(ctx, o)
	if e != nil {
		return ProviderAttempt{}, e
	}
	request := provider.CreateRequest{TradeNo: tradeNo, Subject: subject, Method: method, NotifyURL: notify, ReturnURL: providerReturn, ClientIP: clientIP, OpenID: openID, Amount: charge104}
	appID := ""
	if identity, ok := p.(provider.ApplicationIdentity); ok {
		appID = identity.ApplicationID()
	}
	accountScope, e := provider.TransferAccountScope(p)
	if e != nil {
		return ProviderAttempt{}, e
	}
	sharing, e := g.prepareSharingPayment(ctx, o, channelID, method, appID, accountScope)
	if e != nil {
		return ProviderAttempt{}, e
	}
	request.ProfitSharing = sharing.Declared
	if validator, ok := p.(provider.CreateValidator); ok {
		if e := validator.ValidateCreate(request); e != nil {
			return ProviderAttempt{}, e
		}
	}
	attempt, owner, e := g.Store.beginProviderAttemptWithIdentity(ctx, tradeNo, channelID, method, appID, openID, accountScope, &sharing)
	if e != nil || !owner {
		return attempt, e
	}
	material, e := p.Create(ctx, request)
	if e != nil {
		state := "unknown"
		var reject *provider.ProviderError
		if errors.As(e, &reject) && definiteProviderRejection(reject) {
			state = "failed"
		}
		if save := g.Store.RecordProviderAttemptResult(ctx, attempt.ID, state, "", "", ""); save != nil {
			return attempt, errors.Join(e, save)
		}
		return attempt, e
	}
	if material.MaterialType == "" || material.Material == "" {
		g.Store.RecordProviderAttemptResult(ctx, attempt.ID, "unknown", "", "", "")
		return attempt, &provider.UnknownOutcome{Operation: "create", Cause: provider.ErrAuthentication}
	}
	if e = g.Store.RecordProviderAttemptResult(ctx, attempt.ID, "pending", material.MaterialType, material.Material, material.ProviderTradeNo); e != nil {
		return attempt, &provider.UnknownOutcome{Operation: "persist create response", Cause: e}
	}
	return g.Store.ProviderAttempt(ctx, tradeNo)
}

func validProviderOrigin(raw string, local bool) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if !local || u.Scheme != "http" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
}
func (g ProviderFlow) HandleNotification(ctx context.Context, channelID int64, tradeNo string, headers http.Header, raw []byte) error {
	resolved, resolveErr := g.parentPaymentTrade(ctx, tradeNo)
	if resolveErr != nil {
		return resolveErr
	}
	g = resolved
	if g.Store == nil {
		return ErrInvalid
	}
	g, resolveErr = g.forPaymentTrade(ctx, tradeNo, channelID)
	if resolveErr != nil {
		return resolveErr
	}
	p := g.Providers[channelID]
	v, ok := p.(provider.NotificationVerifier)
	if !ok {
		return provider.ErrUnsupported
	}
	a, e := g.Store.ProviderAttempt(ctx, tradeNo)
	if e != nil {
		return e
	}
	if a.ChannelID != channelID {
		return ErrConflict
	}
	if e = g.Store.checkProviderAccount(ctx, g.Store.pool, a.ID, p); e != nil {
		return e
	}
	o, e := g.Store.OrderByTradeNo(ctx, tradeNo)
	if e != nil {
		return e
	}
	charge104, e := g.Store.AuthenticatedOrderCharge104(ctx, o)
	if e != nil {
		return e
	}
	event, e := v.VerifyNotification(ctx, headers, raw, provider.ExpectedPayment{TradeNo: tradeNo, Amount: charge104})
	if e != nil {
		return e
	}
	if event.State != "SUCCESS" || event.Currency != "CNY" || event.Amount != charge104 || event.TradeNo != tradeNo || event.EventID == "" {
		return provider.ErrAuthentication
	}
	return g.Store.markPaid(ctx, tradeNo, fmt.Sprintf("%d:%s", channelID, event.EventID), true, PaidReceipt{RealMoney: event.Amount, ChannelID: channelID, ProviderTradeNo: event.ProviderTradeNo, BuyerID: event.BuyerID})
}
