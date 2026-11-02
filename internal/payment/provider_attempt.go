package payment

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

// ProviderAttempt holds only customer payment materials and routing identifiers.
// Provider credentials, authorization headers, and raw error bodies are never stored.
// A processing attempt with a crashed owner requires provider reconciliation, never
// a lease-based retry: the provider may already have accepted the original request.
type ProviderAttempt struct {
	ID              int64     `json:"id,string"`
	OrderID         int64     `json:"order_id,string"`
	TradeNo         string    `json:"trade_no"`
	ChannelID       int64     `json:"channel_id,string"`
	Method          string    `json:"method"`
	State           string    `json:"state"`
	MaterialType    string    `json:"material_type"`
	Material        string    `json:"material"`
	ProviderTradeNo string    `json:"provider_trade_no"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

var providerMethodPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

const providerAttemptColumns = "a.id,a.order_id,o.trade_no,a.channel_id,a.method,a.state,a.material_type,a.material,a.provider_trade_no,a.created_at,a.updated_at"

func scanProviderAttempt(row pgx.Row) (ProviderAttempt, error) {
	var a ProviderAttempt
	e := row.Scan(&a.ID, &a.OrderID, &a.TradeNo, &a.ChannelID, &a.Method, &a.State, &a.MaterialType, &a.Material, &a.ProviderTradeNo, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return a, e
}

// BeginProviderAttempt grants ownership exactly once. Existing attempts always
// return owner=false, even after failure or unknown outcome. A different channel
// or method conflicts rather than silently switching an externally visible order.
func (s *Store) BeginProviderAttempt(ctx context.Context, tradeNo string, channelID int64, method string) (ProviderAttempt, bool, error) {
	return s.beginProviderAttempt(ctx, tradeNo, channelID, method, nil, "")
}
func (s *Store) beginProviderAttempt(ctx context.Context, tradeNo string, channelID int64, method string, identityHash []byte, accountScope string, terms ...*sharingPaymentIntent) (ProviderAttempt, bool, error) {
	var sharing *sharingPaymentIntent
	if len(terms) > 0 {
		sharing = terms[0]
	}
	return s.beginProviderAttemptTerms(ctx, tradeNo, channelID, method, identityHash, accountScope, sharing, nil)
}
func (s *Store) beginProviderAttemptTerms(ctx context.Context, tradeNo string, channelID int64, method string, identityHash []byte, accountScope string, sharing *sharingPaymentIntent, recovery *scanRecoveryIntent) (ProviderAttempt, bool, error) {
	if tradeNo == "" || channelID <= 0 || !providerMethodPattern.MatchString(method) {
		return ProviderAttempt{}, false, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ProviderAttempt{}, false, e
	}
	defer tx.Rollback(ctx)
	if e = CheckPaymentDefend206Tx(ctx, tx); e != nil {
		return ProviderAttempt{}, false, e
	}
	var orderID int64
	var status string
	e = tx.QueryRow(ctx, "SELECT id,status FROM orders WHERE trade_no=$1 FOR UPDATE", tradeNo).Scan(&orderID, &status)
	if errors.Is(e, pgx.ErrNoRows) {
		return ProviderAttempt{}, false, ErrNotFound
	}
	if e != nil {
		return ProviderAttempt{}, false, e
	}
	if sharing != nil && sharing.runtimeChannel != nil && sharing.runtimeChannel.parent != nil {
		if e = parentChannelLock(ctx, tx, channelID); e != nil {
			return ProviderAttempt{}, false, e
		}
	}
	appID := ""
	if sharing != nil {
		appID = sharing.AppID
	}
	if e = s.validateReferralProfitAttempt(ctx, tx, orderID, channelID, method, appID, accountScope); e != nil {
		return ProviderAttempt{}, false, e
	}
	pricedOrder, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", orderID))
	if e != nil {
		return ProviderAttempt{}, false, e
	}
	if _, _, e = s.authenticatedOrderCharge104(ctx, tx, pricedOrder); e != nil {
		return ProviderAttempt{}, false, e
	}
	a, e := scanProviderAttempt(tx.QueryRow(ctx, "SELECT "+providerAttemptColumns+" FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE a.order_id=$1", orderID))
	if e == nil {
		if a.ChannelID != channelID || a.Method != method {
			return ProviderAttempt{}, false, ErrConflict
		}
		if e = bindProviderIdentity(ctx, tx, a.ID, identityHash, false); e != nil {
			return ProviderAttempt{}, false, e
		}
		if e = s.bindAttemptRecoveryAccount(ctx, tx, a.ID, accountScope, false, recovery); e != nil {
			return ProviderAttempt{}, false, e
		}
		if e = s.bindSharingPayment(ctx, tx, a.ID, orderID, channelID, method, accountScope, false, sharing); e != nil {
			return ProviderAttempt{}, false, e
		}
		return a, false, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return ProviderAttempt{}, false, e
	}
	if status != "created" {
		return ProviderAttempt{}, false, ErrState
	}
	var bound *RuntimeChannel
	if sharing != nil {
		bound = sharing.runtimeChannel
	}
	if e = s.validateRouteFeeAttempt(ctx, tx, pricedOrder, channelID, method, accountScope, bound); e != nil {
		return ProviderAttempt{}, false, e
	}
	// Review admission applies only to a first external payment intent. An
	// existing original attempt above remains recoverable, never re-owned.
	var admitted bool
	if e = tx.QueryRow(ctx, `SELECT m.enabled AND COALESCE((to_jsonb(m)->>'pay')::int,1)=1 FROM merchants m JOIN orders o ON o.merchant_id=m.id WHERE o.id=$1 FOR SHARE OF m`, orderID).Scan(&admitted); e != nil {
		return ProviderAttempt{}, false, e
	}
	if !admitted {
		return ProviderAttempt{}, false, ErrState
	}
	// Recheck first-send admission for orders created before the operator
	// enabled certification. Existing attempts above remain recoverable.
	var recharge bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM recharge_intents WHERE order_id=$1)`, orderID).Scan(&recharge); e != nil {
		return ProviderAttempt{}, false, e
	}
	if !recharge {
		if e = s.requireCertification233Tx(ctx, tx, pricedOrder.MerchantID); e != nil {
			return ProviderAttempt{}, false, e
		}
	}
	riskOrigin, e := s.prepareCoreRiskOrigin(ctx, tx, pricedOrder, channelID, method)
	if e != nil {
		return ProviderAttempt{}, false, e
	}
	var id int64
	if e = tx.QueryRow(ctx, "INSERT INTO provider_attempts(order_id,channel_id,method) VALUES($1,$2,$3) RETURNING id", orderID, channelID, method).Scan(&id); e != nil {
		return ProviderAttempt{}, false, e
	}
	if e = bindProviderIdentity(ctx, tx, id, identityHash, true); e != nil {
		return ProviderAttempt{}, false, e
	}
	if e = s.bindAttemptRecoveryAccount(ctx, tx, id, accountScope, true, recovery); e != nil {
		return ProviderAttempt{}, false, e
	}
	if e = s.bindSharingPayment(ctx, tx, id, orderID, channelID, method, accountScope, true, sharing); e != nil {
		return ProviderAttempt{}, false, e
	}
	if _, e = tx.Exec(ctx, "UPDATE orders SET status='submitting' WHERE id=$1", orderID); e != nil {
		return ProviderAttempt{}, false, e
	}
	// Queue before external I/O so a crashed owner is also reconciled. The
	// delayed task performs Query only; it can never resubmit an attempt.
	if _, e = tx.Exec(ctx, "INSERT INTO jobs(event_key,kind,payload,run_at) VALUES($1,'payment.reconcile',jsonb_build_object('trade_no',$2::text),now()+interval '30 seconds') ON CONFLICT(event_key) DO NOTHING", "payment-reconcile:"+tradeNo, tradeNo); e != nil {
		return ProviderAttempt{}, false, e
	}
	a, e = scanProviderAttempt(tx.QueryRow(ctx, "SELECT "+providerAttemptColumns+" FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE a.id=$1", id))
	if e != nil {
		return ProviderAttempt{}, false, e
	}
	if e = s.bindCoreRiskOrigin(ctx, tx, riskOrigin, id); e != nil {
		return ProviderAttempt{}, false, e
	}
	if bound != nil && bound.child != nil {
		if e = s.checkChildFirstSendTx(ctx, tx, pricedOrder, bound); e != nil {
			return ProviderAttempt{}, false, e
		}
	}
	if e = s.parentAttemptFinal(ctx, tx, pricedOrder, bound); e != nil {
		return ProviderAttempt{}, false, e
	}
	if e = s.finalCoreRiskClock(ctx, tx, riskOrigin); e != nil {
		return ProviderAttempt{}, false, e
	}
	if e = s.merchantScanFirstSend106(ctx, tx, pricedOrder); e != nil {
		return ProviderAttempt{}, false, e
	}

	if e = tx.Commit(ctx); e != nil {
		return ProviderAttempt{}, false, e
	}
	return a, true, nil
}

// RecordProviderAttemptResult persists a single request's outcome without ever
// crediting money. Only separately authenticated payment verification may MarkPaid.
// The order lock is acquired before the attempt lock to match Begin and callbacks.
func (s *Store) RecordProviderAttemptResult(ctx context.Context, attemptID int64, state, materialType, material, providerTradeNo string) error {
	if attemptID <= 0 || (state != "pending" && state != "unknown" && state != "failed") || len(material) > 1<<20 || len(providerTradeNo) > 256 {
		return ErrInvalid
	}
	if materialType != "" && !providerMethodPattern.MatchString(materialType) {
		return ErrInvalid
	}
	if (material == "") != (materialType == "") {
		return ErrInvalid
	}
	if state != "pending" && (material != "" || materialType != "") {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var orderID int64
	if e = tx.QueryRow(ctx, "SELECT order_id FROM provider_attempts WHERE id=$1", attemptID).Scan(&orderID); errors.Is(e, pgx.ErrNoRows) {
		return ErrNotFound
	} else if e != nil {
		return e
	}
	var status string
	if e = tx.QueryRow(ctx, "SELECT status FROM orders WHERE id=$1 FOR UPDATE", orderID).Scan(&status); e != nil {
		return e
	}
	a, e := scanProviderAttempt(tx.QueryRow(ctx, "SELECT "+providerAttemptColumns+" FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE a.id=$1 FOR UPDATE OF a", attemptID))
	if e != nil {
		return e
	}
	if a.State != "processing" {
		if a.State == state && a.MaterialType == materialType && a.Material == material && a.ProviderTradeNo == providerTradeNo {
			return nil
		}
		return ErrConflict
	}
	if _, e = tx.Exec(ctx, "UPDATE provider_attempts SET state=$2,material_type=$3,material=$4,provider_trade_no=$5,updated_at=now() WHERE id=$1", attemptID, state, materialType, material, providerTradeNo); e != nil {
		return e
	}
	// A fast callback can settle before the Create HTTP call returns. Preserve the
	// authoritative terminal order state while still retaining returned materials.
	if status == "submitting" {
		if _, e = tx.Exec(ctx, "UPDATE orders SET status=$2 WHERE id=$1 AND status='submitting'", orderID, state); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) ProviderAttempt(ctx context.Context, tradeNo string) (ProviderAttempt, error) {
	return scanProviderAttempt(s.pool.QueryRow(ctx, "SELECT "+providerAttemptColumns+" FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE o.trade_no=$1", tradeNo))
}
