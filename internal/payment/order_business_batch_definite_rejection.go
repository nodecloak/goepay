package payment

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
)

// A definite business rejection may terminate only while the original order
// lock proves that this fixed request has no committed intent or money move.
// Database/transport/commit errors and any original intent stay recoverable.
func (s *Store) failUnstartedOrderBusinessBatch185(ctx context.Context, a OrderBusinessBatchActor, id int64, source orderBusinessBatchSource, action string, cause error) (orderBusinessBatchEventFacts, bool) {
	var uncertain *adapter.UnknownOutcome
	if errors.As(cause, &uncertain) || !(errors.Is(cause, ErrState) || errors.Is(cause, ErrInvalid) || errors.Is(cause, ErrInsufficient) || errors.Is(cause, ErrConflict) || errors.Is(cause, adapter.ErrUnsupported) || errors.Is(cause, ErrLiveDisabled) || errors.Is(cause, identity.ErrFundsAuthorization) || errors.Is(cause, identity.ErrAdminSession)) {
		return orderBusinessBatchEventFacts{}, false
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return orderBusinessBatchEventFacts{}, false
	}
	defer tx.Rollback(ctx)
	r, e := s.lockOrderBusinessBatch185(ctx, tx, a, id)
	if e != nil || r.Action != action || source.Position < 1 || source.Position > len(r.Items) {
		return orderBusinessBatchEventFacts{}, false
	}
	item := r.Items[source.Position-1]
	if item.State != "running" && item.State != "unknown" {
		return orderBusinessBatchEventFacts{}, false
	}
	frozen := r.Sources[source.Position-1]
	if frozen.OrderID != source.OrderID || frozen.MerchantID != source.MerchantID || frozen.TradeNo != source.TradeNo {
		return orderBusinessBatchEventFacts{}, false
	}
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1 FOR UPDATE", source.OrderID))
	if e != nil || o.MerchantID != source.MerchantID || o.TradeNo != source.TradeNo || o.Amount != source.Amount || o.MerchantAmount != source.MerchantAmount || o.PlatformFee != source.PlatformFee {
		return orderBusinessBatchEventFacts{}, false
	}
	quote, e := s.validateOrderRouteFeeTerms(ctx, tx, o)
	if e != nil {
		return orderBusinessBatchEventFacts{}, false
	}
	if s.authenticateFrozenCharge104(ctx, tx, o, source.CustomerCharge104) != nil {
		return orderBusinessBatchEventFacts{}, false
	}
	var proof []byte
	if quote != nil {
		proof = quote.proof
	}
	if !hmac.Equal(proof, source.RouteFeeProof) {
		return orderBusinessBatchEventFacts{}, false
	}
	if e = s.verifyOrderRefundFundingHistory(ctx, tx, o.ID); e != nil {
		return orderBusinessBatchEventFacts{}, false
	}
	var present bool
	no := fmt.Sprintf("batch185-%s:%d:%d", action, id, source.Position)
	switch action {
	case "refund":
		// All original refund reservations and send claims take this order lock.
		// A result anchor with a missing row is not proof of an unstarted request.
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM refunds WHERE merchant_id=$1 AND refund_no=$2) OR EXISTS(SELECT 1 FROM provider_refund_result_evidence WHERE order_id=$3 AND refund_no=$2)`, source.MerchantID, no, o.ID).Scan(&present)
	case "freeze-income":
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM order_income_holds WHERE merchant_id=$1 AND request_no=$2) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='orders' AND resource_id=$3 AND operation='freeze-income' AND changes->>'external_no'=$2) OR EXISTS(SELECT 1 FROM order_income_hold_fundings WHERE merchant_id=$1 AND external_no=$2)`, source.MerchantID, no, o.ID).Scan(&present)
	case "unfreeze-income":
		h, err := scanIncomeHold(tx.QueryRow(ctx, "SELECT "+incomeHoldColumns+" FROM order_income_holds h JOIN orders o ON o.id=h.order_id WHERE h.id=$1 AND h.order_id=$2 FOR UPDATE OF h", source.HoldID, o.ID))
		if err != nil {
			return orderBusinessBatchEventFacts{}, false
		}
		h, _, _, err = s.readIncomeHold(ctx, tx, h)
		if err != nil || h.State != "held" {
			return orderBusinessBatchEventFacts{}, false
		}
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM order_income_hold_releases WHERE hold_id=$1) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='orders' AND resource_id=$2 AND operation='unfreeze-income' AND changes->>'hold_id'=$1::text)`, source.HoldID, o.ID).Scan(&present)
	default:
		return orderBusinessBatchEventFacts{}, false
	}
	if e != nil || present {
		return orderBusinessBatchEventFacts{}, false
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return orderBusinessBatchEventFacts{}, false
	}
	facts := orderBusinessBatchEventFacts{State: "failed", Code: "original_" + action + "_rejected_before_intent", OrderStatus: o.Status}
	if e = s.appendOrderBusinessBatchEvent(ctx, tx, r, source.Position, "result", facts); e != nil {
		return orderBusinessBatchEventFacts{}, false
	}
	if e = s.checkOrderBusinessBatchActor(ctx, tx, a); e != nil {
		return orderBusinessBatchEventFacts{}, false
	}
	if e = tx.Commit(ctx); e != nil {
		return orderBusinessBatchEventFacts{}, false
	}
	return facts, true
}
