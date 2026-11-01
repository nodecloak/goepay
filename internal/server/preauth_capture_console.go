package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/backoffice"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"log/slog"
	"net/http"
	"time"
)

func (s *Server) preauthorizationCapturePermission(ctx context.Context, o payment.Order, allowClaimed bool) error {
	if s.Store == nil || s.Pool == nil || s.ProviderFlow == nil || !s.ProviderFlow.AllowLive || s.ProviderFlow.LocalSimulation || s.Sandbox || !validOAuthOrigin(s.ProviderFlow.CallbackBaseURL) {
		return payment.ErrLiveDisabled
	}
	current, e := s.Store.OrderByTradeNo(ctx, o.TradeNo)
	if e != nil || current.MerchantID != o.MerchantID {
		return payment.ErrNotFound
	}
	if current.Status == "paid" || current.Status == "closed" || current.Status == "failed" || current.Type != "alipay" {
		return payment.ErrState
	}
	attempt, e := s.Store.ProviderAttempt(ctx, o.TradeNo)
	if e != nil || attempt.Method != "preauth" {
		return payment.ErrState
	}
	a, e := s.Store.Preauthorization(ctx, o.TradeNo)
	if e != nil || a.ChannelID != attempt.ChannelID || a.Amount != current.Amount || a.ReleaseState != "none" || a.CaptureState != "none" && !(allowClaimed && a.CaptureState == "processing") {
		return payment.ErrState
	}
	resolved, e := s.paymentIntentServer(ctx, current.MerchantID, current.OutTradeNo, attempt.ChannelID)
	if e != nil {
		return e
	}
	s = resolved
	p := s.ProviderFlow.Providers[attempt.ChannelID]
	if p == nil || p.ID() != "alipay" {
		return adapter.ErrUnsupported
	}
	if _, ok := p.(adapter.PreauthorizationProvider); !ok {
		return adapter.ErrUnsupported
	}
	preauth := false
	for _, method := range p.Capabilities().Methods {
		if method == "preauth" {
			preauth = true
		}
	}
	if !preauth || !p.Capabilities().Query {
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
	if e = s.Pool.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1", current.MerchantID).Scan(&enabled); e != nil {
		return e
	}
	if !enabled {
		return backoffice.ErrForbidden
	}
	return nil
}

// CanCapturePreauthorization reads only local state and configuration. Fresh
// signed provider evidence is checked by the explicit POST, never by a GET.
func (s *Server) CanCapturePreauthorization(ctx context.Context, o payment.Order) bool {
	return s.preauthorizationCapturePermission(ctx, o, false) == nil
}

// The shared console middleware must authenticate role/CSRF before this hook.
func (s *Server) preauthorizationCaptureConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) != 4 || parts[1] != "orders" || parts[3] != "capture-authorization" {
		return false
	}
	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil || s.ProviderFlow == nil {
		response(w, 503, -1, "authorization capture unavailable", nil)
		return true
	}
	o, e := s.Store.OrderByTradeNo(r.Context(), parts[2])
	if e != nil || role != "admin" && o.MerchantID != session.User.ID {
		response(w, 404, -1, "order not found", nil)
		return true
	}
	var in struct{}
	if !decode(w, r, &in) {
		return true
	}
	if e = s.preauthorizationCapturePermission(r.Context(), o, false); e != nil {
		var data any
		if a, readErr := s.Store.Preauthorization(r.Context(), o.TradeNo); readErr == nil {
			data = a
		}
		if errors.Is(e, payment.ErrLiveDisabled) {
			response(w, 503, -1, "authorization capture not enabled", data)
		} else {
			response(w, 409, -1, "authorization is not eligible for first capture", data)
		}
		return true
	}
	changes := map[string]any{"trade_no": o.TradeNo, "merchant_id": fmt.Sprint(o.MerchantID), "amount": fmt.Sprint(o.Amount)}
	audit := func(ctx context.Context, phase string) error {
		raw, e := json.Marshal(changes)
		if e != nil {
			return e
		}
		_, e = s.Pool.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'orders',$4,$5,$6)", role, session.User.ID, session.User.Username, o.ID, "capture-authorization-"+phase, raw)
		return e
	}
	if e = audit(r.Context(), "intent"); e != nil {
		s.fail(w, e)
		return true
	}
	guard := func(ctx context.Context) error {
		if e := s.preauthorizationCapturePermission(ctx, o, true); e != nil {
			return e
		}
		a, e := s.Store.Preauthorization(ctx, o.TradeNo)
		if e != nil {
			return e
		}
		var ip string
		e = s.Pool.QueryRow(ctx, "SELECT client_ip FROM order_reporting WHERE order_id=$1", o.ID).Scan(&ip)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		return backoffice.New(s.Pool, s.Store).CheckPayment(ctx, backoffice.PaymentContext{MerchantID: o.MerchantID, ClientIP: ip, Buyer: a.BuyerID, NotifyURL: o.NotifyURL, ReturnURL: o.ReturnURL, OrderIntent: paymentPolicyOriginalIntent(o)})
	}
	a, operationErr := s.ProviderFlow.CapturePreauthorizationChecked(r.Context(), o.MerchantID, o.TradeNo, guard)
	state := "rejected"
	if a.TradeNo != "" {
		state = a.CaptureState
	}
	changes["capture_state"] = state
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e = audit(ctx, "result"); e != nil {
		slog.Error("manual authorization capture result audit failed", "order_id", o.ID)
		response(w, 503, -1, "capture result audit unavailable; query original order", a)
		return true
	}
	if operationErr == nil {
		response(w, 200, 0, "verified authorization capture result", a)
		return true
	}
	if isFundsAuthorizationError(operationErr) {
		fundsHTTPError(w, operationErr)
		return true
	}
	if a.CaptureState == "unknown" || a.CaptureState == "processing" {
		response(w, 202, 0, "capture outcome pending; query original order without resubmitting", a)
		return true
	}
	if a.CaptureState == "failed" {
		response(w, 409, -1, "capture rejected; original request cannot be resent", a)
		return true
	}
	if errors.Is(operationErr, payment.ErrState) || errors.Is(operationErr, payment.ErrConflict) {
		response(w, 409, -1, "authorization is no longer eligible for capture", a)
		return true
	}
	if errors.Is(operationErr, backoffice.ErrForbidden) {
		response(w, 403, -1, "capture policy rejected", a)
		return true
	}
	if errors.Is(operationErr, payment.ErrLiveDisabled) || errors.Is(operationErr, adapter.ErrUnsupported) {
		response(w, 503, -1, "authorization capture not enabled", a)
		return true
	}
	response(w, 503, -1, "fresh provider evidence unavailable; no new capture confirmed", a)
	return true
}
