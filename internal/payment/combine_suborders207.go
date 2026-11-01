package payment

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	provider "goepay/internal/adapters/payment"
)

// CombineSubOrder207 is one share of a WeChat v3 combined order.
type CombineSubOrder207 struct {
	OrderID        int64
	Position       int
	SubTradeNo     string
	Amount         int64
	Status         string // pending, paid, closed
	TransactionID  string
	RefundedAmount int64
	PaidAt         *time.Time
}

// CombinePaid207 is the provider's verified record of one paid sub-order.
type CombinePaid207 struct {
	SubTradeNo, TransactionID string
	Amount                    int64
}

// CombineSubTradeNo207 is the original sub-order number: parent number plus
// its 1-based position.
func CombineSubTradeNo207(tradeNo string, position int) string {
	return tradeNo + strconv.Itoa(position)
}

func scanCombineSubOrders207(rows pgx.Rows) ([]CombineSubOrder207, error) {
	defer rows.Close()
	var out []CombineSubOrder207
	for rows.Next() {
		var s CombineSubOrder207
		var tx *string
		if err := rows.Scan(&s.OrderID, &s.Position, &s.SubTradeNo, &s.Amount, &s.Status, &tx, &s.RefundedAmount, &s.PaidAt); err != nil {
			return nil, err
		}
		if tx != nil {
			s.TransactionID = *tx
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

const combineSubOrderColumns207 = `order_id,position,sub_trade_no,amount,status,transaction_id,refunded_amount,paid_at`

func (s *Store) combineSubOrdersTx(ctx context.Context, tx pgx.Tx, orderID int64, lock bool) ([]CombineSubOrder207, error) {
	// A schema predating migration 000124 has no combined orders at all, so an
	// order on it never has sub-orders. Check tx-safely (to_regclass returns
	// NULL for a missing relation rather than aborting the transaction).
	if ok, err := combineSchema207(ctx, tx); err != nil || !ok {
		return nil, err
	}
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	rows, err := tx.Query(ctx, `SELECT `+combineSubOrderColumns207+` FROM combine_suborders207 WHERE order_id=$1 ORDER BY position`+suffix, orderID)
	if err != nil {
		return nil, err
	}
	return scanCombineSubOrders207(rows)
}

// combineSchema207 reports whether the combine_suborders207 table exists in the
// current schema. to_regclass never raises on a missing relation, so it is
// safe to call inside an open transaction.
func combineSchema207(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (bool, error) {
	var present bool
	err := q.QueryRow(ctx, "SELECT to_regclass('combine_suborders207') IS NOT NULL").Scan(&present)
	return present, err
}

// CreateCombineSubOrdersTx records the sub-orders of an order before the
// combined create is sent. Re-recording the identical split is idempotent; a
// different split for the same order is a conflict (never a silent re-split).
func (s *Store) CreateCombineSubOrdersTx(ctx context.Context, tx pgx.Tx, orderID int64, tradeNo string, amounts []int64) ([]CombineSubOrder207, error) {
	if tx == nil || orderID <= 0 || tradeNo == "" || len(amounts) < combineMinSubOrders207 || len(amounts) > combineMaxSubOrders207 {
		return nil, ErrInvalid
	}
	existing, err := s.combineSubOrdersTx(ctx, tx, orderID, true)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		if len(existing) != len(amounts) {
			return nil, ErrConflict
		}
		for i, sub := range existing {
			if sub.Amount != amounts[i] || sub.SubTradeNo != CombineSubTradeNo207(tradeNo, i+1) {
				return nil, ErrConflict
			}
		}
		return existing, nil
	}
	for i, amount := range amounts {
		if amount <= 0 {
			return nil, ErrInvalid
		}
		if _, err = tx.Exec(ctx, `INSERT INTO combine_suborders207(order_id,position,sub_trade_no,amount) VALUES($1,$2,$3,$4)`, orderID, i+1, CombineSubTradeNo207(tradeNo, i+1), amount); err != nil {
			return nil, err
		}
	}
	return s.combineSubOrdersTx(ctx, tx, orderID, false)
}

// CreateCombineSubOrders is CreateCombineSubOrdersTx in its own transaction.
func (s *Store) CreateCombineSubOrders(ctx context.Context, orderID int64, tradeNo string, amounts []int64) ([]CombineSubOrder207, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	out, err := s.CreateCombineSubOrdersTx(ctx, tx, orderID, tradeNo, amounts)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

// CombineSubOrders reads an order's sub-orders (none for an ordinary order).
func (s *Store) CombineSubOrders(ctx context.Context, orderID int64) ([]CombineSubOrder207, error) {
	if ok, err := combineSchema207(ctx, s.pool); err != nil || !ok {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+combineSubOrderColumns207+` FROM combine_suborders207 WHERE order_id=$1 ORDER BY position`, orderID)
	if err != nil {
		return nil, err
	}
	return scanCombineSubOrders207(rows)
}

var errCombineIncomplete207 = errors.New("combined payment does not cover every sub-order")

// MarkCombineSubOrdersPaidTx applies a verified combined payment: every
// sub-order must be reported exactly once with its exact amount. A repeat with
// the same transaction ids is idempotent; any other difference is a conflict.
func (s *Store) MarkCombineSubOrdersPaidTx(ctx context.Context, tx pgx.Tx, orderID int64, paid []CombinePaid207) error {
	subs, err := s.combineSubOrdersTx(ctx, tx, orderID, true)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return ErrNotFound
	}
	byNo := map[string]CombinePaid207{}
	for _, p := range paid {
		if _, dup := byNo[p.SubTradeNo]; dup || p.TransactionID == "" {
			return ErrConflict
		}
		byNo[p.SubTradeNo] = p
	}
	if len(byNo) != len(subs) {
		return errCombineIncomplete207
	}
	for _, sub := range subs {
		p, ok := byNo[sub.SubTradeNo]
		if !ok || p.Amount != sub.Amount {
			return ErrConflict
		}
		switch sub.Status {
		case "paid":
			if sub.TransactionID != p.TransactionID {
				return ErrConflict
			}
		case "pending":
			tag, err := tx.Exec(ctx, `UPDATE combine_suborders207 SET status='paid',transaction_id=$3,paid_at=clock_timestamp() WHERE order_id=$1 AND position=$2 AND status='pending'`, orderID, sub.Position, p.TransactionID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrConflict
			}
		default:
			return ErrState
		}
	}
	return nil
}

// combineQueryRequest207 builds the provider query/close request for an
// order, carrying its recorded combined sub-orders when it has any.
func (s *Store) combineQueryRequest207(ctx context.Context, tradeNo string) (provider.QueryRequest, error) {
	q := provider.QueryRequest{TradeNo: tradeNo}
	o, err := s.OrderByTradeNo(ctx, tradeNo)
	if err != nil {
		return q, err
	}
	subs, err := s.CombineSubOrders(ctx, o.ID)
	if err != nil {
		return q, err
	}
	for _, sub := range subs {
		q.CombineSubs = append(q.CombineSubs, provider.CombineSub{TradeNo: sub.SubTradeNo, Amount: sub.Amount})
	}
	return q, nil
}

// Combine207Config reads the original combine settings (wxcombine_open,
// wxcombine_minmoney, wxcombine_submoney). Missing settings mean off.
func (s *Store) Combine207Config(ctx context.Context) (Combine207, error) {
	var c Combine207
	rows, err := s.pool.Query(ctx, `SELECT body->>'key',body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1)`, []string{"wxcombine_open", "wxcombine_minmoney", "wxcombine_submoney"})
	if err != nil {
		return c, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var k string
		var v *string
		if err = rows.Scan(&k, &v); err != nil {
			return c, err
		}
		if seen[k] || v == nil {
			return Combine207{}, ErrConflict
		}
		seen[k] = true
		switch k {
		case "wxcombine_open":
			c.Open = *v
		case "wxcombine_minmoney":
			c.MinMoney = *v
		case "wxcombine_submoney":
			c.SubMoney = *v
		}
	}
	return c, rows.Err()
}

// createMaybeCombined207 sends the order as a WeChat v3 combined order when
// the provider supports it and the original split applies; the sub-orders
// are recorded durably before the single external send. Otherwise it is an
// ordinary create.
func (g ProviderFlow) createMaybeCombined207(ctx context.Context, p provider.Provider, o Order, request provider.CreateRequest) (provider.CreateResult, error) {
	combiner, ok := p.(provider.CombineCreator)
	if !ok {
		return p.Create(ctx, request)
	}
	subs, err := g.Store.prepareCombineSubOrders207(ctx, o, request.Amount)
	if err != nil {
		return provider.CreateResult{}, &provider.ProviderError{Code: "INVALID_REQUEST"} // local: nothing was sent
	}
	if len(subs) == 0 {
		return p.Create(ctx, request)
	}
	shares := make([]provider.CombineSub, 0, len(subs))
	for _, sub := range subs {
		shares = append(shares, provider.CombineSub{TradeNo: sub.SubTradeNo, Amount: sub.Amount})
	}
	return combiner.CombineCreate(ctx, request, shares)
}
