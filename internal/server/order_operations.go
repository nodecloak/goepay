package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Server) orderOperations(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 3 || parts[1] != "orders" || len(parts) > 4 {
		return false
	}
	if r.Method != "GET" && r.Method != "POST" {
		return false
	}
	if r.Method == "GET" && len(parts) != 3 {
		return false
	}
	if r.Method == "POST" && (len(parts) != 4 || (parts[3] != "query" && parts[3] != "close" && parts[3] != "refund")) {
		return false
	}
	order, err := s.Store.OrderByTradeNo(r.Context(), parts[2])
	if err != nil || (role != "admin" && order.MerchantID != session.User.ID) {
		response(w, 404, -1, "order not found", nil)
		return true
	}
	if r.Method == "GET" {
		charge104, chargeError104 := s.Store.AuthenticatedOrderCharge104(r.Context(), order)
		if chargeError104 == nil {
			order.RealMoney = &charge104
		}
		data := map[string]any{"order": order}
		data["can_edit_api_trade_alias"] = s.canEditOrderAPITradeAlias120(r, role, session, order, s.OrderAPITradeAlias120)
		combined, combinedError := s.combinedOrderDetailCapability204(r.Context(), role, session, order)
		if combinedError != nil {
			response(w, 409, -1, "historical suborder source could not be confirmed", nil)
			return true
		}
		combine := 0
		if combined {
			combine = 1
		}
		data["can_view_suborders"] = combined
		data["order"] = struct {
			payment.Order
			Combine int `json:"combine"`
		}{Order: order, Combine: combine}
		data["charge_verified104"] = chargeError104 == nil
		if role == "admin" {
			control, e := readOrderAdminControl(r.Context(), s.Pool, order.ID)
			if e != nil {
				s.fail(w, e)
				return true
			}
			data["admin_control"] = control
			data["can_manual_refund"] = chargeError104 == nil && session.Issuer == nil && session.User.AdminClass == "super" && order.Status == "paid" && !control.Archived
		}
		reporting, e := s.orderReportingProjection(r.Context(), order.ID, order.MerchantID)
		if e != nil {
			s.fail(w, e)
			return true
		}
		data["reporting"] = reporting
		for k, v := range s.OrderIncomeHoldView(r.Context(), order, role) {
			data[k] = v
		}
		if role == "admin" && session.User.AdminClass == "super" && session.Issuer == nil {
			if h, ok := data["income_hold"].(payment.OrderIncomeHold); ok {
				data["can_adopt_income_hold"] = h.State == "held" && h.SourceMode == "legacy_unprepared"
			}
		}
		data["can_capture_authorization"] = s.CanCapturePreauthorization(r.Context(), order)
		data["can_observe_authorization"] = session.Issuer == nil && s.CanObservePreauthorization(r.Context(), order)
		data["can_resend_notification"] = order.Status == "paid" && order.NotifyURL != ""
		data["can_return_notification"] = s.canReturnNotification200(r, role, session, order)
		var notificationID int64
		var notificationState string
		var notificationAttempts int
		var notificationRunAt, notificationCreatedAt time.Time
		if e := s.Pool.QueryRow(r.Context(), "SELECT id,state,attempts,run_at,created_at FROM jobs WHERE kind='payment.notify' AND payload->>'trade_no'=$1 ORDER BY created_at DESC,id DESC LIMIT 1", order.TradeNo).Scan(&notificationID, &notificationState, &notificationAttempts, &notificationRunAt, &notificationCreatedAt); e == nil {
			data["notification"] = map[string]any{"job_id": fmt.Sprint(notificationID), "state": notificationState, "attempts": notificationAttempts, "run_at": notificationRunAt, "created_at": notificationCreatedAt}
		} else if !errors.Is(e, pgx.ErrNoRows) {
			s.fail(w, e)
			return true
		}
		if attempt, e := s.Store.ProviderAttempt(r.Context(), order.TradeNo); e == nil {
			data["provider"] = map[string]any{"channel_id": fmt.Sprint(attempt.ChannelID), "method": attempt.Method, "state": attempt.State, "material_type": attempt.MaterialType, "can_close": attempt.Method != "preauth"}
			context, e := s.Store.OriginalSubchannelContext(r.Context(), order.MerchantID, order.OutTradeNo, attempt.ChannelID)
			if e != nil {
				data["payment_subchannel_unconfirmed"] = true
				data["provider"].(map[string]any)["can_close"] = false
			} else if context != nil {
				data["payment_subchannel"] = context
			}
			if attempt.Method == "scan" {
				recovery, e := s.Store.ScanRecoveryStatus(r.Context(), order.MerchantID, order.TradeNo)
				if e != nil {
					s.fail(w, e)
					return true
				}
				if recovery != nil {
					data["scan_recovery"] = recovery
					data["provider"].(map[string]any)["can_close"] = false
				}
			}
		}
		if authorization, e := s.Store.Preauthorization(r.Context(), order.TradeNo); e == nil {
			data["authorization"] = authorization
			canRelease := order.Status != "paid" && authorization.ReleaseState == "none" && s.ProviderFlow != nil && s.ProviderFlow.AllowLive
			canRemaining := session.Issuer == nil && (role == "merchant" || session.User.AdminClass == "super") && order.Status != "paid" && authorization.CaptureState != "succeeded" && s.ProviderFlow != nil && s.ProviderFlow.AllowLive && s.CanObservePreauthorization(r.Context(), order)
			if release, re := s.Store.AuthorizationRelease(r.Context(), order.MerchantID, order.TradeNo); re == nil {
				data["authorization_release"] = release
				canRelease = release.SourceRevision == 0 && order.Status != "paid" && release.State == "reversal_pending" && !release.Sent && s.ProviderFlow != nil && s.ProviderFlow.AllowLive
				canRemaining = canRemaining && !release.Sent && (release.State == "checking" || release.State == "reversal_pending") && release.SourceRevision < 100
			} else if !errors.Is(re, payment.ErrNotFound) {
				s.fail(w, re)
				return true
			} else {
				canRemaining = canRemaining && authorization.ReleaseState == "none"
			}
			data["can_release_remaining_authorization"] = canRemaining
			data["can_release_authorization"] = canRelease
		}
		if data["payment_subchannel_unconfirmed"] == true {
			data["can_capture_authorization"] = false
			data["can_observe_authorization"] = false
			data["can_release_authorization"] = false
			data["can_release_remaining_authorization"] = false
		}
		if role == "merchant" {
			views := []map[string]any{{"id": fmt.Sprint(order.ID)}}
			if e := s.merchantOrderActions(r.Context(), session, views); e != nil {
				s.fail(w, e)
				return true
			}
			caps := views[0]["merchant_actions"].(map[string]bool)
			if views[0]["income_status"] == "held" && data["can_refund_held_income"] != true {
				caps["refund"] = false
			}
			data["merchant_actions"] = caps
			if _, present := data["can_refund_held_income"]; present {
				data["can_refund_held_income"] = data["can_refund_held_income"] == true && caps["refund"]
			}
		}
		response(w, 200, 0, "", data)
		return true
	}
	if !adapter.CatalogPayType(order.Type) {
		return false
	}
	if s.ProviderFlow == nil {
		response(w, 503, -1, "payment provider unavailable", nil)
		return true
	}
	changes := map[string]any{"trade_no": order.TradeNo, "merchant_id": fmt.Sprint(order.MerchantID)}
	intentWritten := false
	outcome := "unknown"
	writeAudit := func(ctx context.Context, phase string) error {
		raw, e := json.Marshal(changes)
		if e != nil {
			return e
		}
		_, e = s.Pool.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'orders',$4,$5,$6)", role, session.User.ID, session.User.Username, order.ID, parts[3]+"-"+phase, raw)
		return e
	}
	intent := func() bool {
		if e := writeAudit(r.Context(), "intent"); e != nil {
			s.fail(w, e)
			return false
		}
		intentWritten = true
		return true
	}
	defer func() {
		if intentWritten {
			changes["state"] = outcome
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if e := writeAudit(ctx, "result"); e != nil {
				slog.Error("console provider outcome audit failed", "order_id", order.ID)
			}
		}
	}()
	switch parts[3] {
	case "query":
		if !intent() {
			return true
		}
		_, err = s.ProviderFlow.Query(r.Context(), order.MerchantID, order.TradeNo)
		if err == nil {
			order, err = s.Store.OrderByTradeNo(r.Context(), order.TradeNo)
			if err == nil {
				outcome = order.Status
				response(w, 200, 0, "verified provider query", order)
				return true
			}
		}
	case "close":
		if !intent() {
			return true
		}
		err = s.ProviderFlow.Close(r.Context(), order.MerchantID, order.TradeNo)
		if err == nil {
			outcome = "closed"
			response(w, 200, 0, "provider order closed", nil)
			return true
		}
	case "refund":
		var in struct {
			Amount     string `json:"amount"`
			ExternalNo string `json:"external_no"`
		}
		if !decode(w, r, &in) {
			return true
		}
		amount, e := ledger.Parse(in.Amount)
		if e != nil || amount <= 0 || in.ExternalNo == "" || len(in.ExternalNo) > 128 {
			response(w, 422, -1, "valid amount and external_no required", nil)
			return true
		}
		changes["amount"] = ledger.Format(amount)
		changes["external_no"] = in.ExternalNo
		if !intent() {
			return true
		}
		var refund payment.Refund
		if role == "merchant" {
			refund, err = s.ProviderFlow.RefundFromMerchant(r.Context(), order.MerchantID, order.TradeNo, in.ExternalNo, amount)
		} else {
			refund, err = s.ProviderFlow.Refund(r.Context(), order.MerchantID, order.TradeNo, in.ExternalNo, amount)
		}
		if errors.Is(err, payment.ErrMerchantAPIDisabled) {
			response(w, 403, -1, "merchant refunds are disabled by platform policy", nil)
			return true
		}
		if isFundsAuthorizationError(err) {
			fundsHTTPError(w, err)
			return true
		}
		if refund.ID > 0 {
			changes["refund_id"] = fmt.Sprint(refund.ID)
			outcome = refund.Status
		}
		if err == nil {
			response(w, 200, 0, "provider refund result", refund)
			return true
		}
		var unknown *adapter.UnknownOutcome
		if errors.As(err, &unknown) && refund.ID > 0 {
			response(w, 202, 0, "refund submitted; outcome requires verified query", refund)
			return true
		}
	}
	if errors.Is(err, adapter.ErrUnsupported) || errors.Is(err, payment.ErrLiveDisabled) {
		outcome = "not_enabled"
		response(w, 503, -1, "provider operation not enabled", nil)
		return true
	}
	var unknown *adapter.UnknownOutcome
	if errors.As(err, &unknown) {
		response(w, 202, 0, "provider outcome requires query", nil)
		return true
	}
	s.fail(w, err)
	return true
}
