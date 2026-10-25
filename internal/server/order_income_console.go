package server

import (
	"context"
	"encoding/json"
	"errors"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"strconv"
)

// OrderIncomeHoldView is a local ledger/configuration read. It never calls a
// payment provider and does not conflate retained-income holds with auth freezes.
func (s *Server) OrderIncomeHoldView(ctx context.Context, o payment.Order, role string) map[string]any {
	out := map[string]any{"income_hold": nil, "can_freeze_income": false, "can_unfreeze_income": false, "can_refund_held_income": false, "can_adopt_income_hold": false}
	if s.Store == nil {
		return out
	}
	h, e := s.Store.OrderIncomeHold(ctx, o.MerchantID, o.TradeNo)
	if e == nil {
		out["income_hold"] = h
		out["can_refund_held_income"] = h.State == "held" && (h.SourceMode == "fresh" || h.SourceMode == "legacy_adopt")
	} else if !errors.Is(e, payment.ErrNotFound) {
		return out
	}
	canRelease := false
	if e == nil && h.State == "held" {
		canRelease = h.RefundPendingCount == 0 && h.RefundReservedAmount == 0
		out["income_refund_pending"] = h.RefundPendingCount > 0
	}
	if role != "admin" {
		return out
	}
	if e == nil && h.State == "held" {
		out["can_unfreeze_income"] = canRelease
		return out
	}
	amount, e := s.Store.OrderIncomeFreezable(ctx, o.MerchantID, o.TradeNo)
	if e == nil {
		out["can_freeze_income"] = true
		out["income_freeze_amount"] = strconv.FormatInt(amount, 10)
	}
	return out
}

// Called only after shared console session/admin-class and CSRF middleware.
func (s *Server) orderIncomeConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) != 4 || parts[1] != "orders" || parts[3] != "freeze-income" && parts[3] != "unfreeze-income" && parts[3] != "adopt-income-hold" {
		return false
	}
	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if parts[3] == "adopt-income-hold" && r.URL.RawQuery != "" {
		response(w, 400, -1, "query parameters are not accepted for historical funding adoption", nil)
		return true
	}
	if role != "admin" || session.User.AdminClass != "super" && session.User.AdminClass != "operator" {
		response(w, 403, -1, "administrator required", nil)
		return true
	}
	if parts[3] == "adopt-income-hold" && (session.Issuer != nil || session.User.AdminClass != "super") {
		response(w, 403, -1, "original super administrator required for historical funding adoption", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "income ledger unavailable", nil)
		return true
	}
	o, e := s.Store.OrderByTradeNo(r.Context(), parts[2])
	if e != nil {
		s.fail(w, e)
		return true
	}
	actor := payment.IncomeHoldActor{ID: session.User.ID, Name: session.User.Username, Class: session.User.AdminClass}
	var h payment.OrderIncomeHold
	if parts[3] == "freeze-income" {
		var in struct {
			ExternalNo string `json:"external_no"`
		}
		if !decode(w, r, &in) {
			return true
		}
		h, e = s.Store.FreezeOrderIncome(r.Context(), o.MerchantID, o.TradeNo, in.ExternalNo, actor)
	} else if parts[3] == "adopt-income-hold" {
		var raw json.RawMessage
		if !decodeFundsJSON(w, r, &raw) {
			return true
		}
		definition, _ := fundsTarget(r.URL.Path)
		fields, _, err := normalizeFundsBody(raw, definition)
		if err != nil {
			response(w, 422, -1, "exact hold_id and external_no required", nil)
			return true
		}
		holdID, _ := positiveFundsID(fundsText(fields, "hold_id"))
		h, e = s.Store.AdoptOrderIncomeHold(r.Context(), o.MerchantID, o.TradeNo, holdID, fundsText(fields, "external_no"), actor)
	} else {
		var in struct {
			HoldID int64 `json:"hold_id,string"`
		}
		if !decode(w, r, &in) {
			return true
		}
		h, e = s.Store.UnfreezeOrderIncome(r.Context(), o.MerchantID, o.TradeNo, in.HoldID, actor)
	}
	if e != nil {
		if isFundsAuthorizationError(e) {
			fundsHTTPError(w, e)
			return true
		}
		s.fail(w, e)
		return true
	}
	response(w, 200, 0, "order income ledger operation complete", h)
	return true
}
