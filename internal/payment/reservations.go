package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

const refundColumns = "id,merchant_id,order_id,refund_no,amount,status,merchant_amount,fee_amount"

type refundAccess179 uint8

const (
	refundInternal179 refundAccess179 = iota
	refundAPI179
	refundMerchant179
)

type refundAdmission179 struct {
	account merchantAPIAccount
	access  refundAccess179
}

func (s *Store) checkRefundAdmission179(ctx context.Context, tx pgx.Tx, a refundAdmission179) error {
	if a.access == refundAPI179 {
		return s.checkMerchantAPIAdmissionTx(ctx, tx, a.account, "refund")
	}
	if a.access != refundMerchant179 {
		return ErrInvalid
	}
	if !a.account.Enabled {
		return errors.Join(ErrMerchantAPIAdmission, ErrMerchantAPIDisabled)
	}
	platform, e := s.platformAccessTx(ctx, tx, false)
	if e != nil {
		return errors.Join(ErrMerchantAPIAdmission, e)
	}
	if platform.UserRefund != "1" {
		return errors.Join(ErrMerchantAPIAdmission, ErrMerchantAPIDisabled)
	}
	return nil
}

func scanRefund(row pgx.Row) (Refund, error) {
	var r Refund
	e := row.Scan(&r.ID, &r.MerchantID, &r.OrderID, &r.RefundNo, &r.Amount, &r.Status, &r.MerchantAmount, &r.FeeAmount)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrNotFound
	}
	return r, e
}
func (s *Store) ReserveRefund(ctx context.Context, merchantID int64, outTradeNo, refundNo string, amount int64) (Refund, error) {
	return s.reserveRefund(ctx, merchantID, outTradeNo, refundNo, amount, refundInternal179)
}
func (s *Store) ReserveAPIRefund(ctx context.Context, merchantID int64, outTradeNo, refundNo string, amount int64) (Refund, error) {
	if refundNo == "" {
		return s.reserveAutomaticAPIRefund(ctx, merchantID, outTradeNo, amount, time.Now)
	}
	return s.reserveRefund(ctx, merchantID, outTradeNo, refundNo, amount, refundAPI179)
}
func (s *Store) ReserveMerchantRefund(ctx context.Context, merchantID int64, outTradeNo, refundNo string, amount int64) (Refund, error) {
	return s.reserveRefund(ctx, merchantID, outTradeNo, refundNo, amount, refundMerchant179)
}
func (s *Store) reserveRefund(ctx context.Context, merchantID int64, outTradeNo, refundNo string, amount int64, access refundAccess179) (Refund, error) {
	if amount <= 0 || refundNo == "" || len(refundNo) > 128 {
		return Refund{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Refund{}, e
	}
	defer tx.Rollback(ctx)
	var apiAccounts []refundAdmission179
	if access != refundInternal179 {
		m, e := s.merchantAPIAccountTx(ctx, tx, merchantID, false)
		if e != nil {
			return Refund{}, e
		}
		apiAccounts = append(apiAccounts, refundAdmission179{m, access})
	}
	r, e := s.reserveRefundTx(ctx, tx, merchantID, outTradeNo, refundNo, amount, apiAccounts...)
	if e != nil {
		return r, e
	}
	return r, tx.Commit(ctx)
}

func (s *Store) reserveRefundTx(ctx context.Context, tx pgx.Tx, merchantID int64, outTradeNo, refundNo string, amount int64, apiAccounts ...refundAdmission179) (Refund, error) {
	var r Refund
	if amount <= 0 || refundNo == "" || len(refundNo) > 128 {
		return r, ErrInvalid
	}
	if len(apiAccounts) > 1 || (len(apiAccounts) == 1 && apiAccounts[0].account.ID != merchantID) {
		return r, ErrInvalid
	}
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE merchant_id=$1 AND out_trade_no=$2 FOR UPDATE", merchantID, outTradeNo))
	if e != nil {
		return r, e
	}
	charge104, _, e := s.authenticatedOrderCharge104(ctx, tx, o)
	if e != nil {
		return r, e
	}
	r, e = scanRefund(tx.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE merchant_id=$1 AND refund_no=$2", merchantID, refundNo))
	if e == nil {
		if r.Amount != amount || r.OrderID != o.ID {
			return r, ErrConflict
		}
		if e = s.verifyReferralRefundEvidence(ctx, tx, r); e != nil {
			return r, e
		}
		if e = s.verifyRefundOrigin201(ctx, tx, r); e != nil {
			return r, e
		}
		if len(apiAccounts) == 1 && r.Status != "succeeded" && r.Status != "failed" {
			// A reservation without an original provider operation is not an
			// already-sent API refund. Closing API access must not let callers
			// dispatch a merely reserved internal/offline refund for the first time.
			var sentIntent bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_operations WHERE kind='refund' AND refund_id=$1)`, r.ID).Scan(&sentIntent); e != nil {
				return r, e
			}
			if !sentIntent {
				if e = s.checkRefundAdmission179(ctx, tx, apiAccounts[0]); e != nil {
					return r, e
				}
			}
		}
		return r, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return r, e
	}
	if len(apiAccounts) > 1 {
		return r, ErrInvalid
	}
	if len(apiAccounts) == 1 {
		if apiAccounts[0].account.ID != merchantID {
			return r, ErrInvalid
		}
		if e = s.checkRefundAdmission179(ctx, tx, apiAccounts[0]); e != nil {
			return r, e
		}
	}
	if o.Status != "paid" {
		return r, ErrState
	}
	if e = s.orderIncomeRefundAllowed(ctx, tx, o.ID); e != nil {
		return r, e
	}
	fundingQuote, e := s.RefundFundingPreview(ctx, tx, o.ID, refundNo, amount)
	if e != nil {
		return r, e
	}
	var usedGross, usedMerchant, usedFee int64
	e = tx.QueryRow(ctx, "SELECT COALESCE(sum(amount),0),COALESCE(sum(merchant_amount),0),COALESCE(sum(fee_amount),0) FROM refunds WHERE order_id=$1 AND status<>'failed'", o.ID).Scan(&usedGross, &usedMerchant, &usedFee)
	if e != nil {
		return r, e
	}
	if usedGross > charge104 || amount > charge104-usedGross {
		return r, ErrInvalid
	}
	merchantAmount, feeAmount := amount, int64(0)
	if o.RefundFeePayer == "platform" {
		if usedMerchant > o.MerchantAmount || usedFee > o.PlatformFee {
			return r, ErrState
		}
		remaining := o.MerchantAmount - usedMerchant
		if merchantAmount > remaining {
			merchantAmount = remaining
		}
		feeAmount = amount - merchantAmount
		if feeAmount > o.PlatformFee-usedFee {
			return r, ErrState
		}
	}
	if e = requireFundsAction(ctx, tx, "refund", fmt.Sprintf("%d:%s", merchantID, refundNo)); e != nil {
		return r, e
	}
	r, e = s.insertRefundWithOrigin201(ctx, tx, merchantID, o.ID, refundNo, amount, merchantAmount, feeAmount, apiAccounts)
	if e != nil {
		return r, e
	}
	funding, e := s.prepareRefundFunding(ctx, tx, o, r, fundingQuote)
	if e != nil {
		return r, e
	}
	clawback, e := s.prepareReferralClawback(ctx, tx, o, r, usedFee)
	if e != nil {
		return r, e
	}
	if e = lockRefundLedgerAccounts(ctx, tx, r, clawback, "reserve", &funding); e != nil {
		return r, e
	}
	if e = s.reserveReferralClawback(ctx, tx, clawback, r); e != nil {
		return r, e
	}
	if e = s.reserveRefundFunding(ctx, tx, o, r, &funding); e != nil {
		return r, e
	}
	if feeAmount > 0 {
		if e = post(ctx, tx, fmt.Sprintf("refund-fee:%d:reserve", r.ID), "refund platform fee reserve", 0, "fees", 0, "fee_reserved", feeAmount); e != nil {
			return r, e
		}
	}
	if _, e = tx.Exec(ctx, "INSERT INTO jobs(event_key,kind,payload,run_at) VALUES($1,'refund.reconcile',jsonb_build_object('refund_id',$2::bigint),now()+interval '30 seconds')", fmt.Sprintf("refund:%d", r.ID), r.ID); e != nil {
		return r, e
	}
	return r, nil
}
func (s *Store) ResolveRefund(ctx context.Context, id int64, state string) error {
	if state != "unknown" && state != "succeeded" && state != "failed" {
		return ErrInvalid
	}
	return s.resolveInternalRefund(ctx, id, state)
}
func (s *Store) resolveRefundTx(ctx context.Context, tx pgx.Tx, id int64, state string) error {
	var e error
	var orderID int64
	if e = tx.QueryRow(ctx, "SELECT order_id FROM refunds WHERE id=$1", id).Scan(&orderID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "SELECT id FROM orders WHERE id=$1 FOR UPDATE", orderID); e != nil {
		return e
	}
	r, e := scanRefund(tx.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1 FOR UPDATE", id))
	if e != nil {
		return e
	}
	if e = s.verifyReferralRefundEvidence(ctx, tx, r); e != nil {
		return e
	}
	m, amount, feeAmount, current := r.MerchantID, r.MerchantAmount, r.FeeAmount, r.Status
	if current == state {
		return nil
	}
	if current == "succeeded" || current == "failed" {
		return ErrState
	}
	var clawback *referralClawbackRecord
	c, e := s.loadReferralClawbackForRefund(ctx, tx, id)
	if e == nil {
		clawback = &c
	} else if !errors.Is(e, ErrNotFound) {
		return e
	}
	var funding *refundFundingRecord
	fr, fe := s.scanRefundFunding(ctx, tx, r)
	if fe == nil {
		funding = &fr
	} else if !errors.Is(fe, ErrNotFound) {
		return fe
	}
	// operation_audit references the original payer merchant. Its FK check
	// must not first acquire that row after any financial account lock.
	if state != "unknown" {
		if e = lockMerchantForeignKeySet(ctx, tx, []int64{m}); e != nil {
			return e
		}
	}
	if e = lockRefundLedgerAccounts(ctx, tx, r, clawback, state, funding); e != nil {
		return e
	}
	if state != "unknown" {
		owner, kind := int64(0), "clearing"
		if state == "failed" {
			owner, kind = m, "available"
		}
		if funding != nil {
			if e = s.resolveRefundFunding(ctx, tx, r, *funding, state); e != nil {
				return e
			}
		} else if amount > 0 {
			if e = post(ctx, tx, fmt.Sprintf("refund:%d:resolve", id), "legacy refund "+state, m, "reserved", owner, kind, amount); e != nil {
				return e
			}
		}
		if feeAmount > 0 {
			feeTarget := "clearing"
			if state == "failed" {
				feeTarget = "fees"
			}
			if e = post(ctx, tx, fmt.Sprintf("refund-fee:%d:resolve", id), "refund platform fee "+state, 0, "fee_reserved", 0, feeTarget, feeAmount); e != nil {
				return e
			}
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE refunds SET status=$2,updated_at=now() WHERE id=$1", id, state); e != nil {
		return e
	}
	if e = s.resolveReferralClawback(ctx, tx, clawback, state); e != nil {
		return e
	}
	return nil
}
func (s *Store) Refund(ctx context.Context, merchantID int64, refundNo string) (Refund, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return Refund{}, e
	}
	defer tx.Rollback(ctx)
	r, e := scanRefund(tx.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE merchant_id=$1 AND refund_no=$2", merchantID, refundNo))
	if e != nil {
		return r, e
	}
	if e = s.verifyReferralRefundEvidence(ctx, tx, r); e != nil {
		return r, e
	}
	return r, tx.Commit(ctx)
}
func (s *Store) ReserveTransfer(ctx context.Context, merchantID int64, no string, amount int64) (Transfer, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Transfer{}, e
	}
	defer tx.Rollback(ctx)
	t, e := s.reserveTransferTx(ctx, tx, merchantID, no, amount, amount, 0, 0, "")
	if e != nil {
		return t, e
	}
	return t, tx.Commit(ctx)
}
func (s *Store) reserveTransferTx(ctx context.Context, tx pgx.Tx, merchantID int64, no string, amount, cost, fee, rate int64, bizNo string) (Transfer, error) {
	if merchantID <= 0 || amount <= 0 || cost < amount || fee != cost-amount || rate < 0 || rate > 10000 || no == "" || len(no) > 128 {
		return Transfer{}, ErrInvalid
	}
	var enabled bool
	e := tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR NO KEY UPDATE", merchantID).Scan(&enabled)
	if errors.Is(e, pgx.ErrNoRows) {
		return Transfer{}, ErrNotFound
	}
	if e != nil {
		return Transfer{}, e
	}
	if !enabled {
		return Transfer{}, ErrState
	}
	t, e := s.scanTransfer(tx.QueryRow(ctx, "SELECT "+transferColumns+transferFrom+" WHERE t.merchant_id=$1 AND t.transfer_no=$2", merchantID, no))
	if e == nil {
		if t.Amount != amount || t.CostAmount != cost || t.FeeRateBPS != rate {
			return t, ErrConflict
		}
		return t, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return Transfer{}, e
	}
	selectedCycle, e := s.requireOutgoingFundsCycle(ctx, tx, merchantID, cost)
	if e != nil {
		return Transfer{}, e
	}
	if e = requireFundsAction(ctx, tx, "transfer", fmt.Sprintf("%d:%s", merchantID, no)); e != nil {
		return Transfer{}, e
	}
	if e = selectedCycle.checkDeadline(ctx, tx); e != nil {
		return Transfer{}, e
	}
	var id int64
	e = tx.QueryRow(ctx, "INSERT INTO transfers(merchant_id,transfer_no,amount,cost_amount,fee_amount,fee_rate_bps,biz_no) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7::text,'')) RETURNING id", merchantID, no, amount, cost, fee, rate, bizNo).Scan(&id)
	if e != nil {
		return Transfer{}, e
	}
	if e = post(ctx, tx, fmt.Sprintf("transfer:%d:reserve", id), "transfer reserve", merchantID, "available", merchantID, "reserved", cost); e != nil {
		return Transfer{}, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO jobs(event_key,kind,payload,run_at) VALUES($1,'transfer.reconcile',jsonb_build_object('transfer_id',$2::bigint),now()+interval '30 seconds')", fmt.Sprintf("transfer:%d", id), id); e != nil {
		return Transfer{}, e
	}
	return s.scanTransfer(tx.QueryRow(ctx, "SELECT "+transferColumns+transferFrom+" WHERE t.id=$1", id))
}
func (s *Store) ResolveTransfer(ctx context.Context, id int64, state string) error {
	if state != "unknown" && state != "succeeded" && state != "failed" {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = s.resolveTransferTx(ctx, tx, id, state); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) resolveTransferTx(ctx context.Context, tx pgx.Tx, id int64, state string) error {
	if state != "pending" && state != "unknown" && state != "succeeded" && state != "failed" {
		return ErrInvalid
	}
	if handled, e := s.resolveSettlementTransferTx(ctx, tx, id, state); handled || e != nil {
		return e
	}
	var merchant, amount, cost, fee int64
	var current string
	e := tx.QueryRow(ctx, "SELECT merchant_id,amount,cost_amount,fee_amount,status FROM transfers WHERE id=$1 FOR UPDATE", id).Scan(&merchant, &amount, &cost, &fee, &current)
	if e != nil {
		return e
	}
	if current == state {
		return nil
	}
	if current == "succeeded" || current == "failed" {
		return ErrState
	}
	// This transaction can post principal and fee journals. Take their whole
	// account set before the first post, including legacy no-reward transfers.
	if state == "succeeded" || state == "failed" {
		if e = lockMerchantForeignKeySet(ctx, tx, []int64{merchant}); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT id FROM accounts WHERE scope_id=0 AND ((owner_id=$1 AND kind='reserved') OR ($2='failed' AND owner_id=$1 AND kind='available') OR ($2='succeeded' AND owner_id=0 AND (kind='clearing' OR ($3::bigint>0 AND kind='fees'))))`, merchant, state, fee)
		if e != nil {
			return e
		}
		var accountIDs []int64
		for rows.Next() {
			var accountID int64
			if e = rows.Scan(&accountID); e != nil {
				rows.Close()
				return e
			}
			accountIDs = append(accountIDs, accountID)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		count := 2
		if state == "succeeded" && fee > 0 {
			count++
		}
		if len(accountIDs) != count {
			return ErrNotFound
		}
		if e = lockLedgerAccountSet(ctx, tx, accountIDs); e != nil {
			return e
		}
	}
	switch state {
	case "failed":
		if e = post(ctx, tx, fmt.Sprintf("transfer:%d:resolve", id), "transfer failed", merchant, "reserved", merchant, "available", cost); e != nil {
			return e
		}
	case "succeeded":
		if e = post(ctx, tx, fmt.Sprintf("transfer:%d:resolve", id), "transfer succeeded", merchant, "reserved", 0, "clearing", amount); e != nil {
			return e
		}
		if fee > 0 {
			if e = post(ctx, tx, fmt.Sprintf("transfer-fee:%d:resolve", id), "transfer platform fee", merchant, "reserved", 0, "fees", fee); e != nil {
				return e
			}
		}
	}
	_, e = tx.Exec(ctx, "UPDATE transfers SET status=$2,updated_at=now() WHERE id=$1", id, state)
	return e
}
func (s *Store) Transfer(ctx context.Context, merchantID int64, no string) (Transfer, error) {
	return s.scanTransfer(s.pool.QueryRow(ctx, "SELECT "+transferColumns+transferFrom+" WHERE t.merchant_id=$1 AND t.transfer_no=$2", merchantID, no))
}
