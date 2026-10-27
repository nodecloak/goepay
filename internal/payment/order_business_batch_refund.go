package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

func (g ProviderFlow) executeOrderBusinessBatchRefund185(ctx context.Context, a OrderBusinessBatchActor, id int64, source orderBusinessBatchSource) orderBusinessBatchEventFacts {
	no := fmt.Sprintf("batch185-refund:%d:%d", id, source.Position)
	guard := func(ctx context.Context, tx pgx.Tx, kind, key string) error {
		if kind != "refund" || key != fmt.Sprintf("%d:%s", source.MerchantID, no) {
			return identity.ErrFundsAuthorization
		}
		r, e := g.Store.readOrderBusinessBatchTx(ctx, tx, a, id)
		if e != nil {
			return e
		}
		if !r.authorized || r.Action != "refund" || source.Position > len(r.Items) || (r.Items[source.Position-1].State != "running" && r.Items[source.Position-1].State != "unknown") {
			return identity.ErrFundsAuthorization
		}
		return g.Store.checkOrderBusinessBatchAuthority185(ctx, tx, a, r)
	}
	local, e := g.Store.Refund(ctx, source.MerchantID, no)
	// Store.Refund authenticates the original result, funding and referral
	// evidence. An existing number for another immutable scope cannot prove
	// this selected refund succeeded, and must never be resent or released.
	if e == nil && (local.OrderID != source.OrderID || local.MerchantID != source.MerchantID || local.Amount != source.RefundAmount || local.RefundNo != no) {
		return orderBusinessBatchEventFacts{State: "failed", Code: "original_refund_request_conflict", OrderStatus: "paid"}
	}
	sendCtx := context.WithValue(WithFundsActionGuard(ctx, guard), orderBusinessBatch185SendGuardKey{}, FundsActionGuard(guard))
	var result Refund
	if e == nil {
		var claimed bool
		if e = g.Store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_operations WHERE kind='refund' AND refund_id=$1)`, local.ID).Scan(&claimed); e == nil {
			if claimed || local.Status == "succeeded" || local.Status == "failed" {
				result, e = g.QueryRefund(ctx, source.MerchantID, no)
			} else {
				result, e = g.Refund(sendCtx, source.MerchantID, source.TradeNo, no, source.RefundAmount)
			}
		}
	} else if errors.Is(e, ErrNotFound) {
		result, e = g.Refund(sendCtx, source.MerchantID, source.TradeNo, no, source.RefundAmount)
	}
	if e != nil {
		if known, err := g.Store.Refund(ctx, source.MerchantID, no); err == nil && known.Status == "failed" {
			return orderBusinessBatchEventFacts{State: "failed", Code: "original_refund_failed", OrderStatus: "paid"}
		}
		if facts, definite := g.Store.failUnstartedOrderBusinessBatch185(ctx, a, id, source, "refund", e); definite {
			return facts
		}
		return orderBusinessBatchEventFacts{State: "unknown", Code: "original_refund_unconfirmed"}
	}
	switch result.Status {
	case "succeeded":
		return orderBusinessBatchEventFacts{State: "succeeded", Code: "original_refund_succeeded", OrderStatus: "paid"}
	case "failed":
		return orderBusinessBatchEventFacts{State: "failed", Code: "original_refund_failed", OrderStatus: "paid"}
	}
	return orderBusinessBatchEventFacts{State: "unknown", Code: "original_refund_pending", OrderStatus: "paid"}
}
