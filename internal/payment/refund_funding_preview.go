package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

// AuthorizationEvidence is only for the request-local funds fingerprint. Safe
// generic JSON of the public snapshot never exposes its keyed evidence bytes.
func (p RefundFundingSnapshot) AuthorizationEvidence() any {
	return struct {
		View                             RefundFundingSnapshot
		Source, History, Request, Result []byte
	}{p, p.SourceHash, p.HistoryHash, p.RequestHash, p.ResultHash}
}
func (s *Store) RefundFundingPreview(ctx context.Context, q referralReader, oid int64, no string, amount int64) (RefundFundingSnapshot, error) {
	var p RefundFundingSnapshot
	if q == nil || oid <= 0 || no == "" || len(no) > 128 || amount <= 0 {
		return p, ErrInvalid
	}
	o, e := scanOrder(q.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", oid))
	if e != nil {
		return p, e
	}
	charge104, _, e := s.authenticatedOrderChargeReader104(ctx, q, o)
	if e != nil {
		return p, e
	}
	p.OrderID, p.Gross, p.MerchantNet, p.OriginalFee = oid, charge104, o.MerchantAmount, o.PlatformFee
	if e = s.verifyOrderRefundFundingHistory(ctx, q, oid); e != nil {
		return p, e
	}
	r, e := scanRefund(q.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE merchant_id=$1 AND refund_no=$2", o.MerchantID, no))
	if e == nil {
		if r.OrderID != oid || r.Amount != amount {
			return p, ErrConflict
		}
		if e = s.verifyReferralRefundEvidence(ctx, q, r); e != nil {
			return p, e
		}
		p.ExistingRefundID, p.MerchantAmount, p.FeeAmount = r.ID, r.MerchantAmount, r.FeeAmount
		f, e := s.scanRefundFunding(ctx, q, r)
		if errors.Is(e, ErrNotFound) {
			p.FundingMode = "legacy_existing"
			p.AvailableAmount = r.MerchantAmount
			return p, nil
		}
		if e != nil {
			return p, e
		}
		p.HoldID, p.HoldFundingID, p.HeldAmount, p.AvailableAmount = f.HoldID, f.HoldID, f.Held, f.Available
		p.SourceHash, p.RequestHash, p.ResultHash = f.HoldHash, f.Proof, f.Result.Proof
		p.FundingMode = "available"
		if f.HoldID > 0 {
			p.FundingMode = "income_hold"
		}
		return p, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return p, e
	}
	if o.Status != "paid" {
		return p, ErrState
	}
	if e = s.verifyOrderSharingReturnEvidence(ctx, q, oid); e != nil {
		return p, e
	}
	if e = s.verifyOrderSharingFinishEvidence(ctx, q, oid); e != nil {
		return p, e
	}
	var unreleased bool
	if e = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=$1 AND NOT EXISTS(SELECT 1 FROM channel_sharing_fund_releases x WHERE x.order_id=f.order_id))", oid).Scan(&unreleased); e != nil {
		return p, e
	}
	if unreleased {
		return p, ErrState
	}
	h, e := scanIncomeHold(q.QueryRow(ctx, "SELECT "+incomeHoldColumns+" FROM order_income_holds h JOIN orders o ON o.id=h.order_id WHERE h.order_id=$1 AND h.state='held'", oid))
	if e == nil {
		var f incomeFundingRecord
		h, f, p.HistoryHash, e = s.readIncomeHold(ctx, q, h)
		if e != nil {
			return p, e
		}
		if f.HoldID == 0 {
			return p, ErrState
		}
		p.HoldID, p.HoldFundingID, p.HeldOriginal, p.HeldRemaining, p.HeldPending, p.HeldSucceeded = h.ID, f.HoldID, h.Amount, h.RemainingAmount, h.RefundReservedAmount, h.RefundSucceededAmount
		p.SourceHash = f.Proof
	} else if !errors.Is(e, ErrNotFound) {
		return p, e
	}
	var ug, um, uf int64
	if e = q.QueryRow(ctx, "SELECT COALESCE(sum(amount),0),COALESCE(sum(merchant_amount),0),COALESCE(sum(fee_amount),0) FROM refunds WHERE order_id=$1 AND status<>'failed'", oid).Scan(&ug, &um, &uf); e != nil {
		return p, e
	}
	p.MerchantAmount, p.FeeAmount, p.HeldAmount, p.AvailableAmount, e = allocateRefundFunding(charge104, o.MerchantAmount, o.PlatformFee, ug, um, uf, amount, p.HeldRemaining, o.RefundFeePayer)
	if e != nil {
		return p, e
	}
	p.FundingMode = "available"
	if p.HoldID > 0 {
		p.FundingMode = "income_hold"
	}
	return p, nil
}

var _ referralReader = (pgx.Tx)(nil)
