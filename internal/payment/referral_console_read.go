package payment

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type ReferralRewardView struct {
	ID        int64     `json:"id,string"`
	OrderID   int64     `json:"order_id,string"`
	InvitedID int64     `json:"invited_id,string"`
	TradeNo   string    `json:"trade_no"`
	Amount    int64     `json:"amount,string"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
}

type ReferralRewardPage struct {
	Items      []ReferralRewardView `json:"items"`
	NextCursor string               `json:"next_cursor"`
}

// Issued and voided are historical flows. None is the promoter's currently
// spendable wallet balance; other transfers can already have spent a reward.
type ReferralOverview struct {
	InvitedCount          int64 `json:"invited_count,string"`
	IssuedAmount          int64 `json:"issued_amount,string"`
	VoidedAmount          int64 `json:"voided_amount,string"`
	RecoveryPendingAmount int64 `json:"recovery_pending_amount,string"`
	TodayIssued           int64 `json:"today_issued,string"`
	YesterdayIssued       int64 `json:"yesterday_issued,string"`
}

func (s *Store) referralRewardView(ctx context.Context, q referralReader, orderID, promoterID int64) (ReferralRewardView, error) {
	var view ReferralRewardView
	r, err := s.readReferralRewardTx(ctx, q, orderID)
	if err != nil {
		return view, err
	}
	if r.PromoterID != promoterID {
		return view, ErrNotFound
	}
	view = ReferralRewardView{ID: r.ID, OrderID: r.OrderID, InvitedID: r.MerchantID, Amount: r.Amount, State: "active"}
	if err = q.QueryRow(ctx, "SELECT o.trade_no,r.created_at FROM referral_rewards r JOIN orders o ON o.id=r.order_id WHERE r.id=$1", r.ID).Scan(&view.TradeNo, &view.CreatedAt); err != nil {
		return view, err
	}
	history, err := s.referralClawbackHistory(ctx, q, orderID, 0)
	if err != nil {
		return view, err
	}
	if history.SuccessRefundID > 0 {
		view.State = "voided"
	} else if history.HasUnresolved {
		view.State = "recovery_pending"
	}
	return view, nil
}

func (s *Store) ListReferralRewards(ctx context.Context, mid, afterID int64, limit int, actor TransferBatchActor) (ReferralRewardPage, error) {
	page := ReferralRewardPage{Items: []ReferralRewardView{}}
	if !referralOwnerValid(mid, actor) || afterID < 0 || limit < 1 || limit > 100 {
		return page, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return page, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, "SELECT id,order_id FROM referral_rewards WHERE promoter_id=$1 AND id>$2 ORDER BY id LIMIT $3", mid, afterID, limit+1)
	if err != nil {
		return page, err
	}
	type ref struct{ id, order int64 }
	var refs []ref
	for rows.Next() {
		var r ref
		if err = rows.Scan(&r.id, &r.order); err != nil {
			rows.Close()
			return page, err
		}
		refs = append(refs, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(refs) > limit {
		page.NextCursor = strconv.FormatInt(refs[limit-1].id, 10)
		refs = refs[:limit]
	}
	for _, r := range refs {
		view, err := s.referralRewardView(ctx, tx, r.order, mid)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, view)
	}
	return page, tx.Commit(ctx)
}

func (s *Store) ReferralOverview(ctx context.Context, mid int64, actor TransferBatchActor) (ReferralOverview, error) {
	var result ReferralOverview
	if !referralOwnerValid(mid, actor) {
		return result, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT statement_timestamp()").Scan(&now); err != nil {
		return result, err
	}
	local := now.In(time.FixedZone("UTC+08:00", 8*3600))
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	yesterday := today.Add(-24 * time.Hour)
	for cursor := int64(0); ; {
		rows, err := tx.Query(ctx, "SELECT id,invited_id FROM referral_relations WHERE promoter_id=$1 AND id>$2 ORDER BY id LIMIT 100", mid, cursor)
		if err != nil {
			return result, err
		}
		type ref struct{ id, invited int64 }
		var refs []ref
		for rows.Next() {
			var r ref
			if err = rows.Scan(&r.id, &r.invited); err != nil {
				rows.Close()
				return result, err
			}
			refs = append(refs, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, err
		}
		for _, r := range refs {
			relation, err := s.ReferralRelationTx(ctx, tx, r.invited)
			if err != nil {
				return result, err
			}
			if relation.ID != r.id || relation.PromoterID != mid {
				return result, ErrConflict
			}
			result.InvitedCount++
			cursor = r.id
		}
		if len(refs) < 100 {
			break
		}
	}
	add := func(to *int64, n int64) error {
		if n < 0 || *to > math.MaxInt64-n {
			return ErrInvalid
		}
		*to += n
		return nil
	}
	for cursor := int64(0); ; {
		rows, err := tx.Query(ctx, "SELECT id,order_id FROM referral_rewards WHERE promoter_id=$1 AND id>$2 ORDER BY id LIMIT 100", mid, cursor)
		if err != nil {
			return result, err
		}
		type ref struct{ id, order int64 }
		var refs []ref
		for rows.Next() {
			var r ref
			if err = rows.Scan(&r.id, &r.order); err != nil {
				rows.Close()
				return result, err
			}
			refs = append(refs, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, err
		}
		for _, r := range refs {
			view, err := s.referralRewardView(ctx, tx, r.order, mid)
			if err != nil {
				return result, err
			}
			if err = add(&result.IssuedAmount, view.Amount); err != nil {
				return result, err
			}
			switch view.State {
			case "voided":
				err = add(&result.VoidedAmount, view.Amount)
			case "recovery_pending":
				err = add(&result.RecoveryPendingAmount, view.Amount)
			}
			if err != nil {
				return result, err
			}
			if !view.CreatedAt.Before(today) {
				err = add(&result.TodayIssued, view.Amount)
			} else if !view.CreatedAt.Before(yesterday) {
				err = add(&result.YesterdayIssued, view.Amount)
			}
			if err != nil {
				return result, err
			}
			cursor = r.id
		}
		if len(refs) < 100 {
			break
		}
	}
	return result, tx.Commit(ctx)
}
