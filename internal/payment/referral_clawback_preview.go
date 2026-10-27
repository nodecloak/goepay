package payment

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ReferralRefundSnapshot is a read-only authorization fact. Amount is the
// original award to recover for a new refund, or zero once already revoked.
// ExistingRefundID distinguishes a replay from a fresh recovery instruction.
type ReferralRefundSnapshot struct {
	RewardID, PromoterID, Amount, OriginalFee, PriorFeeUsed, RefundFee, FeeContribution int64
	ExistingRefundID, ClawbackRefundID                                                  int64
	ClawbackState                                                                       string
	RewardHash, RequestHash, ResultHash                                                 []byte
}

func (s *Store) ReferralRefundPreview(ctx context.Context, q referralReader, orderID int64, refundNo string, amount int64) (ReferralRefundSnapshot, error) {
	var result ReferralRefundSnapshot
	if orderID <= 0 || amount <= 0 || refundNo == "" || len(refundNo) > 128 {
		return result, ErrInvalid
	}
	o, err := scanOrder(q.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", orderID))
	if err != nil {
		return result, err
	}
	charge104, _, err := s.authenticatedOrderChargeReader104(ctx, q, o)
	if err != nil {
		return result, err
	}
	result.OriginalFee = o.PlatformFee
	if p, handled, err := s.dailyRefundPreview(ctx, q, o, refundNo, amount); handled || err != nil {
		return p, err
	}
	source, err := s.referralRefundSource(ctx, q, orderID)
	if err != nil {
		return result, err
	}
	if source != nil {
		result.RewardID, result.PromoterID, result.RewardHash = source.RewardID, source.PromoterID, source.RequestHash
	}
	r, err := scanRefund(q.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE merchant_id=$1 AND refund_no=$2", o.MerchantID, refundNo))
	if err == nil {
		if r.OrderID != orderID || r.Amount != amount {
			return result, ErrConflict
		}
		if err = s.verifyReferralRefundEvidence(ctx, q, r); err != nil {
			return result, err
		}
		result.ExistingRefundID, result.RefundFee = r.ID, r.FeeAmount
		c, err := s.scanReferralClawback(ctx, q, q.QueryRow(ctx, "SELECT "+referralClawbackColumns+" FROM referral_refund_clawbacks WHERE refund_id=$1", r.ID))
		if err == nil {
			return referralRefundSnapshotForClawback(result, c, true), nil
		}
		if !errors.Is(err, ErrNotFound) {
			return result, err
		}
		// This replay followed an already completed revocation. Bind the
		// original terminal proof, without requesting another recovery.
		history, err := s.referralClawbackHistory(ctx, q, orderID, r.ID)
		if err != nil {
			return result, err
		}
		if history.SuccessRefundID > 0 {
			c, err = s.scanReferralClawback(ctx, q, q.QueryRow(ctx, "SELECT "+referralClawbackColumns+" FROM referral_refund_clawbacks WHERE refund_id=$1", history.SuccessRefundID))
			if err != nil {
				return result, err
			}
			result = referralRefundSnapshotForClawback(result, c, false)
		}
		return result, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return result, err
	}
	if o.Status != "paid" {
		return result, ErrState
	}
	var usedGross, usedMerchant int64
	if err = q.QueryRow(ctx, `SELECT COALESCE(sum(amount),0),COALESCE(sum(merchant_amount),0),COALESCE(sum(fee_amount),0) FROM refunds WHERE order_id=$1 AND status<>'failed'`, orderID).Scan(&usedGross, &usedMerchant, &result.PriorFeeUsed); err != nil {
		return result, err
	}
	if usedGross > charge104 || amount > charge104-usedGross {
		return result, ErrInvalid
	}
	if o.RefundFeePayer == "platform" {
		if usedMerchant > o.MerchantAmount || result.PriorFeeUsed > o.PlatformFee {
			return result, ErrState
		}
		merchantAmount := amount
		if merchantAmount > o.MerchantAmount-usedMerchant {
			merchantAmount = o.MerchantAmount - usedMerchant
		}
		result.RefundFee = amount - merchantAmount
		if result.RefundFee > o.PlatformFee-result.PriorFeeUsed {
			return result, ErrState
		}
	}
	if source == nil {
		return result, nil
	}
	history, err := s.referralClawbackHistory(ctx, q, orderID, 0)
	if err != nil {
		return result, err
	}
	if history.SuccessRefundID > 0 {
		c, err := s.scanReferralClawback(ctx, q, q.QueryRow(ctx, "SELECT "+referralClawbackColumns+" FROM referral_refund_clawbacks WHERE refund_id=$1", history.SuccessRefundID))
		if err != nil {
			return result, err
		}
		return referralRefundSnapshotForClawback(result, c, false), nil
	}
	if history.HasUnresolved {
		return result, ErrState
	}
	var unexplained bool
	if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM refunds f WHERE f.order_id=$1 AND (f.status<>'failed' OR (NOT EXISTS(SELECT 1 FROM referral_refund_clawbacks c WHERE c.refund_id=f.id AND c.state='failed') AND NOT EXISTS(SELECT 1 FROM referral_daily_refund_reservations c WHERE c.refund_id=f.id AND c.order_id=f.order_id AND c.state='failed'))))`, orderID).Scan(&unexplained); err != nil {
		return result, err
	}
	if unexplained {
		return result, ErrConflict
	}
	result.Amount = source.Amount
	result.ClawbackState = "eligible"
	if history.LastRefundID > 0 {
		c, err := s.scanReferralClawback(ctx, q, q.QueryRow(ctx, "SELECT "+referralClawbackColumns+" FROM referral_refund_clawbacks WHERE refund_id=$1", history.LastRefundID))
		if err != nil {
			return result, err
		}
		result.ClawbackRefundID, result.RequestHash, result.ResultHash = c.RefundID, c.RequestHash, c.ResultHash
	}
	result.FeeContribution, err = referralClawbackContribution(result.OriginalFee, result.Amount, result.PriorFeeUsed, result.RefundFee)
	return result, err
}

func referralRefundSnapshotForClawback(result ReferralRefundSnapshot, c referralClawbackRecord, current bool) ReferralRefundSnapshot {
	result.ClawbackRefundID, result.ClawbackState, result.RequestHash, result.ResultHash = c.RefundID, c.State, c.RequestHash, c.ResultHash
	if current {
		result.Amount, result.OriginalFee, result.PriorFeeUsed, result.RefundFee, result.FeeContribution = c.Amount, c.OriginalFee, c.PriorFeeUsed, c.RefundFee, c.FeeContribution
	}
	return result
}

// Assert the reader boundary remains compatible with the actual transaction.
var _ referralReader = (pgx.Tx)(nil)
