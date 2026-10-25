package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	provider "goepay/internal/adapters/payment"
	"strings"
)

type Preauthorization struct {
	TradeNo      string `json:"trade_no"`
	ChannelID    int64  `json:"channel_id,string"`
	AuthNo       string `json:"auth_no"`
	BuyerID      string `json:"buyer_id"`
	Amount       int64  `json:"amount,string"`
	CaptureState string `json:"capture_state"`
	ReleaseState string `json:"release_state"`
}

func scanPreauthorization(row pgx.Row) (Preauthorization, error) {
	var a Preauthorization
	e := row.Scan(&a.TradeNo, &a.ChannelID, &a.AuthNo, &a.BuyerID, &a.Amount, &a.CaptureState, &a.ReleaseState)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return a, e
}

const preauthorizationColumns = "o.trade_no,a.channel_id,a.auth_no,a.buyer_id,a.amount,a.capture_state,a.release_state"

func (s *Store) Preauthorization(ctx context.Context, tradeNo string) (Preauthorization, error) {
	return scanPreauthorization(s.pool.QueryRow(ctx, "SELECT "+preauthorizationColumns+" FROM payment_authorizations a JOIN orders o ON o.id=a.order_id WHERE o.trade_no=$1", tradeNo))
}
func (s *Store) RecordPreauthorization(ctx context.Context, channelID int64, event provider.AuthorizationEvent) error {
	if channelID <= 0 || event.TradeNo == "" || event.RequestNo != event.TradeNo || event.State != "SUCCESS" || event.Amount <= 0 || event.AuthNo == "" || len(event.AuthNo) > 256 || event.BuyerID == "" || len(event.BuyerID) > 256 || strings.HasPrefix(event.AuthNo, "@") || strings.HasPrefix(event.BuyerID, "@") {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var id, amount, actualChannel int64
	var method string
	if e = tx.QueryRow(ctx, "SELECT o.id,o.amount,p.channel_id,p.method FROM orders o JOIN provider_attempts p ON p.order_id=o.id WHERE o.trade_no=$1 FOR UPDATE OF o", event.TradeNo).Scan(&id, &amount, &actualChannel, &method); e != nil {
		return e
	}
	if amount != event.Amount || actualChannel != channelID || method != "preauth" {
		return ErrConflict
	}
	if _, e = tx.Exec(ctx, "INSERT INTO payment_authorizations(order_id,channel_id,auth_no,buyer_id,amount) VALUES($1,$2,$3,$4,$5) ON CONFLICT(order_id) DO NOTHING", id, channelID, event.AuthNo, event.BuyerID, event.Amount); e != nil {
		return e
	}
	a, e := scanPreauthorization(tx.QueryRow(ctx, "SELECT "+preauthorizationColumns+" FROM payment_authorizations a JOIN orders o ON o.id=a.order_id WHERE a.order_id=$1", id))
	if e != nil {
		return e
	}
	if a.ChannelID != channelID || a.AuthNo != event.AuthNo || a.BuyerID != event.BuyerID || a.Amount != event.Amount {
		return ErrConflict
	}
	return tx.Commit(ctx)
}
func (s *Store) BeginPreauthorizationCapture(ctx context.Context, tradeNo string) (Preauthorization, bool, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Preauthorization{}, false, e
	}
	defer tx.Rollback(ctx)
	var id int64
	var status string
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT o.id,o.status,m.enabled FROM orders o JOIN merchants m ON m.id=o.merchant_id WHERE o.trade_no=$1 FOR UPDATE OF o", tradeNo).Scan(&id, &status, &enabled); e != nil {
		return Preauthorization{}, false, e
	}
	if !enabled {
		return Preauthorization{}, false, ErrState
	}
	a, e := scanPreauthorization(tx.QueryRow(ctx, "SELECT "+preauthorizationColumns+" FROM payment_authorizations a JOIN orders o ON o.id=a.order_id WHERE a.order_id=$1 FOR UPDATE OF a", id))
	if e != nil {
		return a, false, e
	}
	if a.CaptureState != "none" || a.ReleaseState != "none" || status == "paid" {
		return a, false, nil
	}
	if status == "closed" || status == "failed" {
		return a, false, ErrState
	}
	if e = requireFundsAction(ctx, tx, "authorization-capture", tradeNo); e != nil {
		return a, false, e
	}
	if _, e = tx.Exec(ctx, "UPDATE payment_authorizations SET capture_state='processing',updated_at=now() WHERE order_id=$1", id); e != nil {
		return a, false, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO jobs(event_key,kind,payload,run_at) VALUES($1,'payment.reconcile',jsonb_build_object('trade_no',$2::text),now()+interval '30 seconds') ON CONFLICT(event_key) DO NOTHING", "preauth-capture-reconcile:"+tradeNo, tradeNo); e != nil {
		return a, false, e
	}
	if e = tx.Commit(ctx); e != nil {
		return a, false, e
	}
	a.CaptureState = "processing"
	return a, true, nil
}
func (s *Store) RecordPreauthorizationCaptureState(ctx context.Context, tradeNo, state string) error {
	if state != "unknown" && state != "failed" && state != "succeeded" {
		return ErrInvalid
	}
	tag, e := s.pool.Exec(ctx, "UPDATE payment_authorizations a SET capture_state=$2,updated_at=now() FROM orders o WHERE a.order_id=o.id AND o.trade_no=$1 AND (a.capture_state IN ('processing','unknown','failed') OR a.capture_state=$2)", tradeNo, state)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrState
	}
	return nil
}
