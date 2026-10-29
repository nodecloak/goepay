package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AdminAdjustBalance is the GOEPAY equivalent of epay_pro admin user/recharge and
// user/withdraw (epay_release changeUserMoney(...,'后台加款'/'后台扣款')). Deep audit
// a3 F4 / W3.
//
// Invariants kept:
//   - the money move is a NEW immutable journal (event_key 'admin-adjust:<id>'),
//     posted between the platform clearing account and the merchant's available
//     wallet — never an UPDATE/DELETE of an existing financial row;
//   - a debit can never drive the wallet below zero: postByAccountIDs debits the
//     merchant account only WHERE balance>=amount, returning ErrInsufficient
//     otherwise (and the accounts CHECK(owner_id=0 OR balance>=0) is a backstop);
//   - it is funds-authorized like every other admin money move
//     (requireFundsAction kind "admin-adjust"); when no guard is installed the
//     call is a no-op guard, exactly like deposit/membership;
//   - idempotent by (merchant_id, external_no): a replay returns the recorded
//     adjustment and posts nothing new (the journal event_key is also conflict-safe);
//   - the reason + actor + direction are recorded in admin_balance_adjustments,
//     the audit the immutable journal cannot carry on its own.
//
// direction is "credit" (后台加款) or "debit" (后台扣款). The projected ledger label
// for this event family is supplied by AdminAdjustRecordLabel so 资金明细 shows
// 后台加款 / 后台扣款 rather than the raw event key.
type AdminAdjustment struct {
	ID, MerchantID, Amount        int64
	Direction, Reason, ExternalNo string
	Actor                         string
	CreatedAt                     time.Time
}

const adminAdjustmentColumns = "id,merchant_id,direction,amount,reason,external_no,actor,created_at"

func scanAdminAdjustment(row pgx.Row) (AdminAdjustment, error) {
	var a AdminAdjustment
	e := row.Scan(&a.ID, &a.MerchantID, &a.Direction, &a.Amount, &a.Reason, &a.ExternalNo, &a.Actor, &a.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return a, e
}

func (s *Store) AdminAdjustBalance(ctx context.Context, merchantID int64, direction, externalNo, reason string, amount int64, actor string) (AdminAdjustment, error) {
	if merchantID <= 0 || (direction != "credit" && direction != "debit") || amount <= 0 ||
		externalNo == "" || len(externalNo) > 128 || reason == "" || len(reason) > 500 || actor == "" || len(actor) > 128 {
		return AdminAdjustment{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return AdminAdjustment{}, e
	}
	defer tx.Rollback(ctx)
	var enabled bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR UPDATE", merchantID).Scan(&enabled); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			e = ErrNotFound
		}
		return AdminAdjustment{}, e
	}
	// Idempotency: an identical replay returns the recorded adjustment; a
	// conflicting reuse of the same external_no is a hard conflict.
	old, e := scanAdminAdjustment(tx.QueryRow(ctx, "SELECT "+adminAdjustmentColumns+" FROM admin_balance_adjustments WHERE merchant_id=$1 AND external_no=$2", merchantID, externalNo))
	if e == nil {
		if old.Direction != direction || old.Amount != amount {
			return AdminAdjustment{}, ErrConflict
		}
		return old, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return AdminAdjustment{}, e
	}
	if e = requireFundsAction(ctx, tx, "admin-adjust", fmt.Sprintf("%d:%s", merchantID, externalNo)); e != nil {
		return AdminAdjustment{}, e
	}
	// Defensive: ensure the wallet accounts exist (created at merchant setup).
	if _, e = tx.Exec(ctx, "INSERT INTO accounts(owner_id,kind) VALUES($1,'available') ON CONFLICT DO NOTHING", merchantID); e != nil {
		return AdminAdjustment{}, e
	}
	a, e := scanAdminAdjustment(tx.QueryRow(ctx, "INSERT INTO admin_balance_adjustments(merchant_id,direction,amount,reason,external_no,actor) VALUES($1,$2,$3,$4,$5,$6) RETURNING "+adminAdjustmentColumns, merchantID, direction, amount, reason, externalNo, actor))
	if e != nil {
		return a, e
	}
	key := fmt.Sprintf("admin-adjust:%d", a.ID)
	if direction == "credit" {
		// 后台加款: platform clearing -> merchant available.
		e = post(ctx, tx, key, "后台加款", 0, "clearing", merchantID, "available", amount)
	} else {
		// 后台扣款: merchant available -> platform clearing (balance>=amount only).
		e = post(ctx, tx, key, "后台扣款", merchantID, "available", 0, "clearing", amount)
	}
	if e != nil {
		return a, e
	}
	return a, tx.Commit(ctx)
}

// AdminAdjustRecordLabel maps an 'admin-adjust:*' ledger event to its original
// 资金明细 type label by sign, so the console record view shows 后台加款/后台扣款
// instead of the raw event key. sign is the entry amount sign for the merchant's
// available account: a positive entry is a credit (加款), negative is a debit.
func AdminAdjustRecordLabel(positive bool) string {
	if positive {
		return "后台加款"
	}
	return "后台扣款"
}
