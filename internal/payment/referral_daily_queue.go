package payment

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

type ReferralDailyQueueItem struct {
	OrderID        int64     `json:"order_id,string"`
	MerchantID     int64     `json:"merchant_id,string"`
	PromoterID     int64     `json:"promoter_id,string"`
	TradeNo        string    `json:"trade_no"`
	Amount         int64     `json:"amount,string"`
	PlatformFee    int64     `json:"platform_fee,string"`
	RewardAmount   int64     `json:"reward_amount,string"`
	PendingAmount  int64     `json:"pending_amount,string"`
	State          string    `json:"state"`
	ActivatedAt    time.Time `json:"activated_at"`
	BusinessDay    string    `json:"business_day"`
	EligibleDay    string    `json:"eligible_day"`
	SourceVersion  int       `json:"source_version"`
	SourceVerified bool      `json:"source_verified"`
}
type ReferralDailyQueuePage struct {
	Items      []ReferralDailyQueueItem `json:"items"`
	NextCursor string                   `json:"next_cursor"`
}

// merchantID is the original promoter/beneficiary, never a caller-selected
// payer. HTTP callers must build actor from their real authenticated session.
func (s *Store) ListReferralDailyQueue(ctx context.Context, merchantID, afterID int64, limit int, actor TransferBatchActor) (ReferralDailyQueuePage, error) {
	page := ReferralDailyQueuePage{Items: []ReferralDailyQueueItem{}}
	if merchantID <= 0 || afterID < 0 || limit < 1 || limit > 100 || !actor.valid(merchantID) {
		return page, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return page, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT d.order_id FROM referral_daily_terms d JOIN referral_daily_liabilities l ON l.order_id=d.order_id WHERE d.promoter_id=$1 AND d.order_id>$2 ORDER BY d.order_id LIMIT $3`, merchantID, afterID, limit)
	if e != nil {
		return page, e
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return page, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return page, e
	}
	for _, id := range ids {
		o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", id))
		if e != nil {
			return page, e
		}
		t, e := s.readReferralTerms(ctx, tx, o)
		if e != nil {
			return page, e
		}
		if t.PromoterID != merchantID {
			return page, ErrConflict
		}
		a, state, pending, e := s.readReferralDailyState(ctx, tx, o, t)
		if e != nil {
			return page, e
		}
		page.Items = append(page.Items, ReferralDailyQueueItem{OrderID: o.ID, MerchantID: o.MerchantID, PromoterID: t.PromoterID, TradeNo: o.TradeNo, Amount: o.Amount, PlatformFee: o.PlatformFee, RewardAmount: t.RewardAmount, PendingAmount: pending, State: state, ActivatedAt: a.ActivatedAt, BusinessDay: a.BusinessDay, EligibleDay: a.EligibleDay, SourceVersion: 2, SourceVerified: true})
		page.NextCursor = fmt.Sprint(id)
	}
	return page, tx.Commit(ctx)
}
