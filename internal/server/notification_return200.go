package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/notification"
	"goepay/internal/payment"
)

func notificationReturnActor200(ctx context.Context, tx pgx.Tx, role string, session identity.Session, token string) error {
	if session.Issuer != nil || session.User.Role != role {
		return identity.ErrOriginalMerchantSession
	}
	if role == "merchant" {
		return identity.CheckOriginalMerchantSessionTx(ctx, tx, session.User, token, session.CSRF)
	}
	if role != "admin" {
		return identity.ErrAdminSession
	}
	if err := identity.CheckOriginalAdminSessionTx(ctx, tx, session.User, token, false); err != nil {
		return err
	}
	// The common HTTP guard has checked the request header; retain the same
	// persisted CSRF identity after any row-lock or audit wait as well.
	digest := sha256.Sum256([]byte(token))
	var csrf string
	err := tx.QueryRow(ctx, "SELECT csrf FROM console_sessions WHERE token_hash=$1 AND role='admin' FOR SHARE", digest[:]).Scan(&csrf)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && subtle.ConstantTimeCompare([]byte(csrf), []byte(session.CSRF)) != 1 {
		return identity.ErrAdminSession
	}
	return err
}

func (s *Server) notificationReturnSource200(ctx context.Context, tx pgx.Tx, role string, session identity.Session, token string, original payment.Order) (payment.Order, payment.Merchant, error) {
	var order payment.Order
	if original.ID <= 0 || original.MerchantID <= 0 || role != "admin" && original.MerchantID != session.User.ID {
		return order, payment.Merchant{}, payment.ErrNotFound
	}
	// Keep the merchant -> account -> session lock order used by credential
	// changes. The signing key is decrypted on this transaction connection.
	merchant, err := s.Store.MerchantTx(ctx, tx, original.MerchantID)
	if err != nil {
		return order, merchant, err
	}
	if role == "merchant" && !merchant.Enabled {
		return order, merchant, identity.ErrOriginalMerchantSession
	}
	if err = notificationReturnActor200(ctx, tx, role, session, token); err != nil {
		return order, merchant, err
	}
	err = tx.QueryRow(ctx, `SELECT id,merchant_id,trade_no,out_trade_no,name,type,amount,notify_url,return_url,param,status,version,created_at,paid_at FROM orders WHERE id=$1 AND merchant_id=$2 AND trade_no=$3 FOR SHARE`, original.ID, original.MerchantID, original.TradeNo).Scan(
		&order.ID, &order.MerchantID, &order.TradeNo, &order.OutTradeNo, &order.Name, &order.Type, &order.Amount, &order.NotifyURL, &order.ReturnURL, &order.Param, &order.Status, &order.Version, &order.CreatedAt, &order.PaidAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = payment.ErrNotFound
	}
	if err != nil {
		return order, merchant, err
	}
	if order.Status != "paid" || order.ReturnURL == "" || len(order.ReturnURL) > 8192 || !validNotificationReturnURL200(order.ReturnURL) {
		return order, merchant, payment.ErrState
	}
	if order.Version == "v2" {
		if s.PlatformKey == nil || s.PlatformKey.N == nil || s.PlatformKey.D == nil || s.PlatformKey.N.BitLen() < 2048 || s.PlatformKey.Validate() != nil {
			return order, merchant, payment.ErrInvalid
		}
	} else if (order.Version != "" && order.Version != "v1") || merchant.APIKey == "" {
		return order, merchant, payment.ErrInvalid
	}
	return order, merchant, nil
}

// A detail flag is only a current observation. The POST reconstructs and locks
// the same authority and original order again before returning signed material.
func (s *Server) canReturnNotification200(r *http.Request, role string, session identity.Session, order payment.Order) bool {
	if s.Store == nil || s.Pool == nil || s.Sessions == nil || !s.Sessions.SupportsDelegation() || session.Issuer != nil || order.Status != "paid" || order.ReturnURL == "" {
		return false
	}
	cookie, err := r.Cookie("goepay_" + role)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false
	}
	defer tx.Rollback(ctx)
	current, merchant, err := s.notificationReturnSource200(ctx, tx, role, session, cookie.Value, order)
	if err != nil {
		return false
	}
	target, err := notification.URL(current.ReturnURL, current, merchant, s.PlatformKey, time.Now())
	return err == nil && len(target) <= 16384 && notificationReturnActor200(ctx, tx, role, session, cookie.Value) == nil
}

func notificationReturnError200(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrOriginalMerchantSession), errors.Is(err, identity.ErrAdminSession):
		response(w, 403, -1, "original authorized session required", nil)
	case errors.Is(err, payment.ErrNotFound):
		response(w, 404, -1, "order not found", nil)
	case errors.Is(err, payment.ErrState):
		response(w, 409, -1, "paid order with a valid stored return address required", nil)
	default:
		// Never include a signing error, destination or bearer URL in diagnostics.
		response(w, 503, -1, "synchronous notification could not be confirmed", nil)
	}
}

func (s *Server) notificationReturnConsole200(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) != 4 || parts[1] != "orders" || parts[3] != "return-notification" {
		return false
	}
	identityHeaders(w)
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		response(w, 405, -1, "POST required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	var body map[string]json.RawMessage
	if !decodeFundsJSON(w, r, &body) {
		return true
	}
	if body == nil || len(body) != 0 {
		response(w, 400, -1, "empty JSON object required", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil || s.Sessions == nil || !s.Sessions.SupportsDelegation() {
		response(w, 503, -1, "persistent sessions required", nil)
		return true
	}
	if session.Issuer != nil {
		notificationReturnError200(w, identity.ErrOriginalMerchantSession)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	original, err := s.Store.OrderByTradeNo(ctx, parts[2])
	if err != nil || role != "admin" && original.MerchantID != session.User.ID {
		notificationReturnError200(w, payment.ErrNotFound)
		return true
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		notificationReturnError200(w, err)
		return true
	}
	defer tx.Rollback(ctx)
	order, merchant, err := s.notificationReturnSource200(ctx, tx, role, session, token, original)
	if err != nil {
		notificationReturnError200(w, err)
		return true
	}
	// This audit deliberately records order identity only. It neither updates
	// notification status nor creates any delivery/provider/financial operation.
	_, err = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'orders',$4,'return-notification',jsonb_build_object('trade_no',$5::text,'merchant_id',$6::text,'version',$7::text))`, role, session.User.ID, session.User.Username, order.ID, order.TradeNo, strconv.FormatInt(order.MerchantID, 10), order.Version)
	if err != nil {
		notificationReturnError200(w, err)
		return true
	}
	target, err := notification.URL(order.ReturnURL, order, merchant, s.PlatformKey, time.Now())
	if err != nil || len(target) > 16384 {
		notificationReturnError200(w, payment.ErrInvalid)
		return true
	}
	if err = notificationReturnActor200(ctx, tx, role, session, token); err != nil {
		notificationReturnError200(w, err)
		return true
	}
	if err = tx.Commit(ctx); err != nil {
		notificationReturnError200(w, err)
		return true
	}
	response(w, 200, 0, "", map[string]string{"url": target})
	return true
}
