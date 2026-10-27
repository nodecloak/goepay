package payment

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"math/big"
	"time"
)

type ParentChannelDay struct {
	Day          string  `json:"day"`
	RealMoney    string  `json:"realmoney"`
	RefundMoney  string  `json:"refundmoney"`
	ProfitMoney  *string `json:"profitmoney"`
	OrderCount   string  `json:"orderCount"`
	SuccessCount string  `json:"successCount"`
	SuccessRate  *string `json:"successRate"`
}

func parentMoney(value *big.Int) string {
	whole, part := new(big.Int), new(big.Int)
	whole.QuoRem(value, big.NewInt(100), part)
	return fmt.Sprintf("%s.%02d", whole.String(), part.Int64())
}

func (s *Store) ParentChannelStatistics(ctx context.Context, actor ParentChannelActor, id int64) ([]ParentChannelDay, error) {
	if id <= 0 {
		return nil, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if e = actor.check(ctx, tx); e != nil {
		return nil, e
	}
	var clock time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&clock); e != nil {
		return nil, e
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	now := clock.In(zone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	out := []ParentChannelDay{}
	for offset := -6; offset <= 0; offset++ {
		start := today.AddDate(0, 0, offset)
		end := start.AddDate(0, 0, 1)
		day := start.Format("2006-01-02")
		_, money, e := s.globalSuccessfulTotal(ctx, tx, id, day, start, end)
		if e != nil {
			return nil, e
		}
		var count, success int64
		var refundRaw string
		if e = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE o.status='paid') FROM orders o JOIN provider_attempts a ON a.order_id=o.id WHERE a.channel_id=$1 AND o.created_at>=$2 AND o.created_at<$3`, id, start, end).Scan(&count, &success); e != nil {
			return nil, e
		}
		if e = tx.QueryRow(ctx, `SELECT COALESCE(sum(r.amount::numeric),0)::text FROM refunds r JOIN provider_attempts a ON a.order_id=r.order_id WHERE a.channel_id=$1 AND r.status='succeeded' AND r.created_at>=$2 AND r.created_at<$3`, id, start, end).Scan(&refundRaw); e != nil {
			return nil, e
		}
		refund, ok := new(big.Int).SetString(refundRaw, 10)
		if !ok || refund.Sign() < 0 {
			return nil, ErrConflict
		}
		row := ParentChannelDay{Day: day, RealMoney: parentMoney(money), RefundMoney: parentMoney(refund), OrderCount: fmt.Sprint(count), SuccessCount: fmt.Sprint(success)}
		if count > 0 {
			ratio := new(big.Int).Quo(new(big.Int).Mul(big.NewInt(success), big.NewInt(10000)), big.NewInt(count))
			text := fmt.Sprintf("%d.%02d", ratio.Int64()/100, ratio.Int64()%100)
			row.SuccessRate = &text
		}
		out = append(out, row)
	}
	if e = actor.check(ctx, tx); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
