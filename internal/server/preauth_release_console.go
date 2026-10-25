package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"log/slog"
	"net/http"
	"time"
)

// authorizationReleaseConsole runs only after the shared console session,
// administrator permission and CSRF checks. The merchant/order binding below
// prevents caller-selected merchant IDs or amounts from changing the release.
func (s *Server) authorizationReleaseConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) != 4 || parts[1] != "orders" || (parts[3] != "release-authorization" && parts[3] != "query-authorization-release") {
		return false
	}
	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil || s.ProviderFlow == nil {
		response(w, 503, -1, "authorization provider unavailable", nil)
		return true
	}
	order, e := s.Store.OrderByTradeNo(r.Context(), parts[2])
	if e != nil || role != "admin" && order.MerchantID != session.User.ID {
		response(w, 404, -1, "order not found", nil)
		return true
	}
	var in struct{}
	if !decode(w, r, &in) {
		return true
	}
	changes := map[string]any{"trade_no": order.TradeNo, "merchant_id": fmt.Sprint(order.MerchantID)}
	audit := func(ctx context.Context, phase string) error {
		raw, e := json.Marshal(changes)
		if e != nil {
			return e
		}
		_, e = s.Pool.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'orders',$4,$5,$6)", role, session.User.ID, session.User.Username, order.ID, parts[3]+"-"+phase, raw)
		return e
	}
	// This must precede both provider queries and the first unfreeze send.
	if e = audit(r.Context(), "intent"); e != nil {
		s.fail(w, e)
		return true
	}
	state := "rejected"
	var release payment.AuthorizationRelease
	defer func() {
		changes["state"] = state
		if release.OrderID > 0 {
			changes["request_no"] = release.RequestNo
			changes["sent"] = release.Sent
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := audit(ctx, "result"); e != nil {
			slog.Error("authorization release result audit failed", "order_id", order.ID)
		}
	}()
	if parts[3] == "release-authorization" {
		release, e = s.ProviderFlow.ReleaseAuthorization(r.Context(), order.MerchantID, order.TradeNo)
	} else {
		release, e = s.ProviderFlow.QueryAuthorizationRelease(r.Context(), order.MerchantID, order.TradeNo)
	}
	if isFundsAuthorizationError(e) {
		fundsHTTPError(w, e)
		return true
	}
	if release.OrderID > 0 {
		state = release.State
	}
	if e == nil {
		status := 200
		if release.State != "succeeded" && release.State != "failed" {
			status = 202
		}
		response(w, status, 0, "authorization release result", release)
		return true
	}
	if errors.Is(e, payment.ErrLiveDisabled) || errors.Is(e, adapter.ErrUnsupported) {
		state = "not_enabled"
		response(w, 503, -1, "authorization release operation not enabled", nil)
		return true
	}
	var unknown *adapter.UnknownOutcome
	if errors.As(e, &unknown) && release.OrderID > 0 {
		response(w, 202, 0, "authorization release pending; query the original request", release)
		return true
	}
	if errors.Is(e, payment.ErrState) || errors.Is(e, payment.ErrConflict) {
		response(w, 409, -1, "authorization cannot be released in its current state", release)
		return true
	}
	if release.OrderID > 0 && release.State == "failed" {
		response(w, 200, 0, "authorization release definitively rejected", release)
		return true
	}
	s.fail(w, e)
	return true
}
