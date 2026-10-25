package server

import (
	"context"
	"crypto/subtle"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/backoffice"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"strings"
)

// This independent read capability never authorizes capture, release or any
// payment mutation. Every check uses local state and performs no provider I/O.
func (s *Server) preauthorizationObservationPermission(ctx context.Context, o payment.Order) error {
	if !s.AuthorizationObservationEnabled || s.Store == nil || s.Pool == nil || s.ProviderFlow == nil || s.Sandbox || s.ProviderFlow.LocalSimulation || !validOAuthOrigin(s.ProviderFlow.CallbackBaseURL) {
		return payment.ErrLiveDisabled
	}
	current, e := s.Store.OrderByTradeNo(ctx, o.TradeNo)
	if e != nil || current.ID != o.ID || current.MerchantID != o.MerchantID || current.Type != "alipay" {
		return payment.ErrNotFound
	}
	attempt, e := s.Store.ProviderAttempt(ctx, o.TradeNo)
	if e != nil || attempt.Method != "preauth" {
		return payment.ErrState
	}
	auth, e := s.Store.Preauthorization(ctx, o.TradeNo)
	if e != nil || auth.ChannelID != attempt.ChannelID || auth.Amount != current.Amount {
		return payment.ErrState
	}
	p := s.ProviderFlow.Providers[attempt.ChannelID]
	if p == nil || p.ID() != "alipay" || !p.Capabilities().Query {
		return adapter.ErrUnsupported
	}
	if _, ok := p.(adapter.PreauthorizationProvider); !ok {
		return adapter.ErrUnsupported
	}
	app, ok := p.(adapter.ApplicationIdentity)
	if !ok || app.ApplicationID() == "" {
		return adapter.ErrUnsupported
	}
	account, ok := p.(adapter.TransferAccountIdentity)
	if !ok || !strings.HasPrefix(account.TransferAccountIdentity(), "seller:") || len(account.TransferAccountIdentity()) <= len("seller:") {
		return adapter.ErrUnsupported
	}
	preauth := false
	for _, method := range p.Capabilities().Methods {
		if method == "preauth" {
			preauth = true
		}
	}
	if !preauth {
		return adapter.ErrUnsupported
	}
	allowed := false
	for _, channel := range s.MerchantChannels[current.MerchantID] {
		if channel == attempt.ChannelID {
			allowed = true
		}
	}
	if !allowed {
		return backoffice.ErrForbidden
	}
	var enabled bool
	if e = s.Pool.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", current.MerchantID).Scan(&enabled); e != nil || !enabled {
		return backoffice.ErrForbidden
	}
	return s.ProviderFlow.ValidatePaymentAccount(ctx, current.MerchantID, current.TradeNo)
}

func (s *Server) CanObservePreauthorization(ctx context.Context, o payment.Order) bool {
	return s.preauthorizationObservationPermission(ctx, o) == nil
}

// The shared console middleware has already checked role, current session,
// administrator permissions and CSRF before dispatching this route.
func (s *Server) preauthorizationObservationConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 4 || parts[1] != "orders" || parts[3] != "authorization-observations" {
		return false
	}
	identityHeaders(w)
	if len(parts) > 5 || r.Method != "POST" && r.Method != "GET" || r.Method == "POST" && len(parts) != 4 || r.Method == "GET" && len(parts) != 5 {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	cookie, ok := identityCookie(r, "goepay_"+role)
	if !ok || subtle.ConstantTimeCompare([]byte(cookie), []byte(token)) != 1 || session.Issuer != nil {
		response(w, 403, -1, "original console session required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters are not accepted", nil)
		return true
	}
	if s.Store == nil || s.ProviderFlow == nil || s.Pool == nil {
		response(w, 503, -1, "authorization observation unavailable", nil)
		return true
	}
	o, e := s.Store.OrderByTradeNo(r.Context(), parts[2])
	if e != nil || role != "admin" && o.MerchantID != session.User.ID {
		response(w, 404, -1, "order not found", nil)
		return true
	}
	check := func(ctx context.Context) error { return s.preauthorizationObservationPermission(ctx, o) }
	if e = check(r.Context()); e != nil {
		observationHTTPError(w, e)
		return true
	}
	var q payment.PreauthorizationObservation
	if r.Method == "POST" {
		in, valid := identityJSON(w, r, "client_request_no")
		if !valid {
			return true
		}
		if len(in) != 1 || !identityHex(in["client_request_no"]) {
			response(w, 400, -1, "client_request_no must be 64 lowercase hexadecimal characters", nil)
			return true
		}
		q, e = s.ProviderFlow.ObservePreauthorization(r.Context(), payment.PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: in["client_request_no"]}, token, role, check)
	} else {
		if !identityHex(parts[4]) {
			response(w, 404, -1, "observation not found", nil)
			return true
		}
		q, e = s.ProviderFlow.ReadPreauthorizationObservation(r.Context(), parts[4], token, role, check)
	}
	if e != nil {
		observationHTTPError(w, e)
		return true
	}
	if q.TradeNo != o.TradeNo || q.MerchantID != o.MerchantID || q.OrderID != o.ID {
		response(w, 404, -1, "observation not found", nil)
		return true
	}
	status := http.StatusOK
	if q.Status != "verified" {
		status = http.StatusAccepted
	}
	response(w, status, 0, "historical read-only authorization observation", q)
	return true
}

func observationHTTPError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, payment.ErrLiveDisabled), errors.Is(e, adapter.ErrUnsupported):
		response(w, 503, -1, "authorization observation not enabled", nil)
	case errors.Is(e, payment.ErrNotFound):
		response(w, 404, -1, "observation not found", nil)
	case errors.Is(e, backoffice.ErrForbidden):
		response(w, 403, -1, "authorization observation permission denied", nil)
	default:
		response(w, 409, -1, "authorization observation unavailable or binding changed", nil)
	}
}
