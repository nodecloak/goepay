package payment

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	provider "goepay/internal/adapters/payment"
)

// combineSubRefund207 is one planned provider refund of a sub-order share.
type combineSubRefund207 struct {
	Position         int
	Amount           int64
	ProviderRefundNo string
	State            string
	Sub              CombineSubOrder207
}

// planCombineRefund207 fixes (once) how a platform refund of amount is split
// over the order's paid sub-orders, in position order, never exceeding what
// each share has left after earlier non-failed refunds. Re-planning the same
// refund returns the stored plan.
func (s *Store) planCombineRefund207(ctx context.Context, refundID, orderID, amount int64, baseNo string) ([]combineSubRefund207, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	subs, err := s.combineSubOrdersTx(ctx, tx, orderID, true)
	if err != nil {
		return nil, err
	}
	plan, err := s.combineRefundPlanTx(ctx, tx, refundID, subs)
	if err != nil {
		return nil, err
	}
	if len(plan) > 0 {
		var sum int64
		for _, p := range plan {
			sum += p.Amount
		}
		if sum != amount {
			return nil, ErrConflict
		}
		return plan, tx.Commit(ctx)
	}
	used := map[int]int64{}
	rows, err := tx.Query(ctx, `SELECT position,sum(amount) FROM combine_subrefunds207 WHERE order_id=$1 AND state<>'failed' GROUP BY position`, orderID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pos int
		var v int64
		if err = rows.Scan(&pos, &v); err != nil {
			rows.Close()
			return nil, err
		}
		used[pos] = v
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	left := amount
	for _, sub := range subs {
		if left == 0 {
			break
		}
		if sub.Status != "paid" {
			return nil, ErrState
		}
		room := sub.Amount - used[sub.Position]
		if room <= 0 {
			continue
		}
		take := room
		if left < take {
			take = left
		}
		no := fmt.Sprintf("%s-%d", baseNo, sub.Position)
		if _, err = tx.Exec(ctx, `INSERT INTO combine_subrefunds207(refund_id,order_id,position,amount,provider_refund_no) VALUES($1,$2,$3,$4,$5)`, refundID, orderID, sub.Position, take, no); err != nil {
			return nil, err
		}
		left -= take
	}
	if left != 0 {
		return nil, ErrInvalid // exceeds what the paid shares can still refund
	}
	plan, err = s.combineRefundPlanTx(ctx, tx, refundID, subs)
	if err != nil {
		return nil, err
	}
	return plan, tx.Commit(ctx)
}

func (s *Store) combineRefundPlanTx(ctx context.Context, tx pgx.Tx, refundID int64, subs []CombineSubOrder207) ([]combineSubRefund207, error) {
	byPos := map[int]CombineSubOrder207{}
	for _, sub := range subs {
		byPos[sub.Position] = sub
	}
	rows, err := tx.Query(ctx, `SELECT position,amount,provider_refund_no,state FROM combine_subrefunds207 WHERE refund_id=$1 ORDER BY position`, refundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []combineSubRefund207
	for rows.Next() {
		var p combineSubRefund207
		if err = rows.Scan(&p.Position, &p.Amount, &p.ProviderRefundNo, &p.State); err != nil {
			return nil, err
		}
		sub, ok := byPos[p.Position]
		if !ok {
			return nil, ErrConflict
		}
		p.Sub = sub
		out = append(out, p)
	}
	return out, rows.Err()
}

// recordCombineSubRefund207 persists one share's provider outcome; a
// successful share also grows the sub-order's refunded amount.
func (s *Store) recordCombineSubRefund207(ctx context.Context, refundID int64, p combineSubRefund207, state string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE combine_subrefunds207 SET state=$3 WHERE refund_id=$1 AND position=$2 AND state NOT IN ('succeeded','failed')`, refundID, p.Position, state)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 && state == "succeeded" {
		if _, err = tx.Exec(ctx, `UPDATE combine_suborders207 SET refunded_amount=refunded_amount+$3 WHERE order_id=$1 AND position=$2`, p.Sub.OrderID, p.Position, p.Amount); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// combineRefundState207 folds share states into the platform refund state.
// A mix of succeeded and failed shares needs human review and stays unknown.
func combineRefundState207(plan []combineSubRefund207) string {
	counts := map[string]int{}
	for _, p := range plan {
		counts[p.State]++
	}
	switch {
	case counts["succeeded"] == len(plan):
		return "succeeded"
	case counts["failed"] == len(plan):
		return "failed"
	case counts["unknown"] > 0 || counts["failed"] > 0:
		return "unknown"
	default:
		return "pending"
	}
}

// combineRefund207 queries claimed shares, and when send is authorized claims
// and sends each previously unclaimed share once. The durable claim precedes
// network I/O; a crash after the claim requires querying, never another send.
func (g ProviderFlow) combineRefund207(ctx context.Context, p provider.Provider, refundID int64, plan []combineSubRefund207, send bool) (string, error) {
	for i := range plan {
		share := &plan[i]
		if share.State == "succeeded" || share.State == "failed" {
			continue
		}
		req := provider.RefundRequest{TradeNo: share.Sub.SubTradeNo, ProviderTradeNo: share.Sub.TransactionID, RefundNo: share.ProviderRefundNo, TotalAmount: share.Sub.Amount, Amount: share.Amount}
		var result provider.RefundResult
		var err error
		claimed := false
		if send && share.State == "pending" {
			tag, claimErr := g.Store.pool.Exec(ctx, `INSERT INTO combine_refund_send_claims207(refund_id,position) VALUES($1,$2) ON CONFLICT DO NOTHING`, refundID, share.Position)
			if claimErr != nil {
				return "unknown", claimErr
			}
			claimed = tag.RowsAffected() == 1
		} else if !send && share.State == "pending" {
			var exists bool
			if err := g.Store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM combine_refund_send_claims207 WHERE refund_id=$1 AND position=$2)`, refundID, share.Position).Scan(&exists); err != nil {
				return "unknown", err
			}
			if !exists {
				continue
			}
		}
		if claimed {
			result, err = p.Refund(ctx, req)
		} else {
			result, err = p.QueryRefund(ctx, req)
		}
		state := "unknown"
		if err != nil {
			var rejection *provider.ProviderError
			if claimed && errors.As(err, &rejection) && definiteProviderRejection(rejection) {
				state = "failed"
			}
		} else if result.RefundNo == share.ProviderRefundNo && result.Amount == share.Amount {
			state = refundProviderState(result.State)
		}
		if save := g.Store.recordCombineSubRefund207(ctx, refundID, *share, state); save != nil {
			return "unknown", errors.Join(err, save)
		}
		share.State = state
		if state == "unknown" {
			return "unknown", &provider.UnknownOutcome{Operation: "combine-refund", Cause: err}
		}
	}
	return combineRefundState207(plan), nil
}
