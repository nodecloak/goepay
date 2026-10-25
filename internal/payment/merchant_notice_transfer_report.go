package payment

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	notice "goepay/internal/merchantnotice"
)

// The original transfer type is recovered from the authenticated recipient
// snapshot, never from today's mutable channel configuration. Only aggregates
// cross this domain boundary; recipient identities and keys remain private.
func (s *Store) NoticeTransferLines(ctx context.Context, tx pgx.Tx, mid int64, from, until time.Time) ([]notice.AmountLine, error) {
	if ctx == nil || tx == nil || mid <= 0 || !from.Before(until) {
		return nil, ErrInvalid
	}
	rows, err := tx.Query(ctx, "SELECT "+transferColumns+transferFrom+" WHERE t.merchant_id=$1 AND t.status='succeeded' AND t.created_at>=$2 AND t.created_at<$3 AND sp.request_id IS NULL ORDER BY t.id", mid, from, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sums := map[string]int64{}
	for rows.Next() {
		transfer, e := s.scanTransfer(rows)
		if e != nil {
			return nil, e
		}
		kind := transfer.Type
		if kind == "" {
			kind = "other"
		}
		if transfer.Amount < 0 || transfer.Amount > math.MaxInt64-sums[kind] {
			return nil, ErrState
		}
		sums[kind] += transfer.Amount
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	kinds := make([]string, 0, len(sums))
	for kind := range sums {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	out := make([]notice.AmountLine, 0, len(kinds))
	names := map[string]string{"wxpay": "微信", "alipay": "支付宝", "qqpay": "QQ钱包", "bank": "银行卡", "other": "其他"}
	for _, kind := range kinds {
		name := names[kind]
		if name == "" {
			return nil, ErrState
		}
		out = append(out, notice.AmountLine{Name: name, Amount: sums[kind]})
	}
	return out, nil
}
