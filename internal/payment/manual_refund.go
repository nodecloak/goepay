package payment

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

func (s *Store) ManualRefund(ctx context.Context, merchantID int64, tradeNo, refundNo string, amount int64, actor IncomeHoldActor, reason string, offlineConfirmed bool) (Refund, error) {
	if actor.ID <= 0 || actor.Name == "" || actor.Class != "super" || !offlineConfirmed || strings.TrimSpace(reason) == "" || len(reason) > 240 || strings.ContainsAny(reason, "\x00\r\n") || amount <= 0 || refundNo == "" || len(refundNo) > 128 {
		return Refund{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Refund{}, e
	}
	defer tx.Rollback(ctx)
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE trade_no=$1 AND merchant_id=$2 FOR UPDATE", tradeNo, merchantID))
	if e != nil {
		return Refund{}, e
	}
	prior, e := scanRefund(tx.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE merchant_id=$1 AND refund_no=$2", merchantID, refundNo))
	if e == nil {
		var original string
		if e = tx.QueryRow(ctx, `SELECT changes->>'reason' FROM backoffice_audit WHERE resource='orders' AND resource_id=$1 AND operation='manual-refund' AND changes->>'external_no'=$2 AND changes->>'refund_id'=$3 ORDER BY id LIMIT 1`, o.ID, refundNo, strconv.FormatInt(prior.ID, 10)).Scan(&original); e != nil {
			return Refund{}, ErrConflict
		}
		if prior.OrderID != o.ID || prior.Amount != amount || original != reason || prior.Status != "succeeded" {
			return Refund{}, ErrConflict
		}
		if e = s.verifyReferralRefundEvidence(ctx, tx, prior); e != nil {
			return Refund{}, e
		}
		return prior, tx.Commit(ctx)
	}
	if !errors.Is(e, ErrNotFound) {
		return Refund{}, e
	}
	r, e := s.reserveRefundTx(ctx, tx, merchantID, o.OutTradeNo, refundNo, amount)
	if e != nil {
		return r, e
	}
	if e = s.resolveRefundTx(ctx, tx, r.ID, "succeeded"); e != nil {
		return r, e
	}
	p := providerRefundResultEvidence{RefundID: r.ID, OrderID: o.ID, Amount: r.Amount, RefundNo: r.RefundNo, PriorRefundState: r.Status, State: "succeeded", Source: "internal"}
	if e = s.insertRefundResultEvidence(ctx, tx, r, p); e != nil {
		return r, e
	}
	raw, e := json.Marshal(map[string]any{"external_no": refundNo, "refund_id": strconv.FormatInt(r.ID, 10), "amount_cents": strconv.FormatInt(amount, 10), "reason": reason, "offline_confirmed": true, "provider_refund_sent": false})
	if e != nil {
		return r, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'orders',$3,'manual-refund',$4)`, actor.ID, actor.Name, o.ID, raw); e != nil {
		return r, e
	}
	r.Status = "succeeded"
	return r, tx.Commit(ctx)
}
