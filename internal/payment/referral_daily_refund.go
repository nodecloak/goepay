package payment

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

type referralDailyRefund struct {
	RefundID, OrderID, Amount, OriginalFee, RefundFee, Contribution                                        int64
	PendingID, EscrowID, FeesID, FeeReservedID                                                             int64
	ReserveJournal, ContributionJournal, RestoreContributionJournal, RestorePendingJournal, ReleaseJournal int64
	RequestHash, ResultHash, PreviousHash                                                                  []byte
	State                                                                                                  string
	At                                                                                                     time.Time
	ResultAt                                                                                               *time.Time
}

const dailyRefundColumns70 = `refund_id,order_id,amount,original_fee,refund_fee,fee_contribution,pending_account_id,escrow_account_id,fees_account_id,fee_reserved_account_id,reserve_journal_id,COALESCE(contribution_journal_id,0),COALESCE(restore_contribution_journal_id,0),COALESCE(restore_pending_journal_id,0),COALESCE(release_remainder_journal_id,0),request_hash,state,result_hash,previous_result_hash,created_at,result_at`

type referralDailyRefundHistory struct {
	Items               []referralDailyRefund
	Unresolved, Success *referralDailyRefund
}

func dailyTime70(at *time.Time) string {
	if at == nil {
		return ""
	}
	return at.UTC().Format(time.RFC3339Nano)
}
func (s *Store) dailyRefundRequestProof(c referralDailyRefund, r Refund, a referralDailyActivation) ([]byte, error) {
	return s.dailyDigest("pending-refund-request", []any{a.Proof, c.RefundID, c.OrderID, c.Amount, c.OriginalFee, c.RefundFee, c.Contribution, c.PendingID, c.EscrowID, c.FeesID, c.FeeReservedID, c.ReserveJournal, c.ContributionJournal, c.At.UTC().Format(time.RFC3339Nano), r.MerchantID, r.RefundNo, r.Amount, r.MerchantAmount, r.FeeAmount})
}
func (s *Store) dailyRefundResultProof(c referralDailyRefund) ([]byte, error) {
	return s.dailyDigest("pending-refund-result", []any{c.RefundID, c.OrderID, c.RequestHash, c.PreviousHash, c.State, c.RestoreContributionJournal, c.RestorePendingJournal, c.ReleaseJournal, dailyTime70(c.ResultAt)})
}
func (s *Store) scanDailyRefund(ctx context.Context, q referralReader, row pgx.Row, d referralDailyTerms, a referralDailyActivation) (referralDailyRefund, error) {
	var c referralDailyRefund
	e := row.Scan(&c.RefundID, &c.OrderID, &c.Amount, &c.OriginalFee, &c.RefundFee, &c.Contribution, &c.PendingID, &c.EscrowID, &c.FeesID, &c.FeeReservedID, &c.ReserveJournal, &c.ContributionJournal, &c.RestoreContributionJournal, &c.RestorePendingJournal, &c.ReleaseJournal, &c.RequestHash, &c.State, &c.ResultHash, &c.PreviousHash, &c.At, &c.ResultAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if e != nil {
		return c, e
	}
	r, e := scanRefund(q.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1", c.RefundID))
	if e != nil {
		return c, e
	}
	k, e := referralClawbackContribution(d.PlatformFee, d.Reward, 0, r.FeeAmount)
	if e != nil || c.OrderID != d.OrderID || r.OrderID != d.OrderID || r.MerchantID != d.MerchantID || c.Amount != d.Reward || c.OriginalFee != d.PlatformFee || c.RefundFee != r.FeeAmount || c.Contribution != k || c.PendingID != d.PendingAccountID || c.FeesID != a.FeesAccountID || c.State != r.Status {
		return c, ErrConflict
	}
	proof, e := s.dailyRefundRequestProof(c, r, a)
	if e != nil || !hmac.Equal(proof, c.RequestHash) {
		return c, ErrConflict
	}
	if e = s.verifyDailyEvent(ctx, q, c.OrderID, "refund-pending", c.RefundID, "refund-engine", c.RequestHash); e != nil {
		return c, e
	}
	if c.State == "pending" {
		if len(c.ResultHash) != 0 || len(c.PreviousHash) != 0 || c.ResultAt != nil {
			return c, ErrConflict
		}
	} else {
		proof, e = s.dailyRefundResultProof(c)
		if e != nil || !hmac.Equal(proof, c.ResultHash) || c.ResultAt == nil {
			return c, ErrConflict
		}
		if e = s.verifyDailyEvent(ctx, q, c.OrderID, "refund-"+c.State, c.RefundID, "refund-engine", c.ResultHash); e != nil {
			return c, e
		}
		if len(c.PreviousHash) != 0 {
			if c.State == "unknown" || len(c.PreviousHash) != 32 {
				return c, ErrConflict
			}
			if e = s.verifyDailyEvent(ctx, q, c.OrderID, "refund-unknown", c.RefundID, "refund-engine", c.PreviousHash); e != nil {
				return c, e
			}
		}
	}
	var valid bool
	e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM referral_daily_refund_invariants WHERE refund_id=$1 AND valid IS TRUE)`, c.RefundID).Scan(&valid)
	if e != nil || !valid {
		return c, ErrConflict
	}
	return c, nil
}
func (s *Store) readDailyRefundHistory(ctx context.Context, q referralReader, d referralDailyTerms, a referralDailyActivation) (referralDailyRefundHistory, error) {
	h := referralDailyRefundHistory{Items: []referralDailyRefund{}}
	for cursor := int64(0); ; {
		rows, e := q.Query(ctx, `SELECT refund_id FROM referral_daily_refund_reservations WHERE order_id=$1 AND refund_id>$2 ORDER BY refund_id LIMIT 100`, d.OrderID, cursor)
		if e != nil {
			return h, e
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return h, e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return h, e
		}
		for _, id := range ids {
			c, e := s.scanDailyRefund(ctx, q, q.QueryRow(ctx, "SELECT "+dailyRefundColumns70+" FROM referral_daily_refund_reservations WHERE refund_id=$1", id), d, a)
			if e != nil {
				return h, e
			}
			h.Items = append(h.Items, c)
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	for i := range h.Items {
		c := &h.Items[i]
		if c.State == "pending" || c.State == "unknown" {
			if h.Unresolved != nil {
				return h, ErrConflict
			}
			h.Unresolved = c
		}
		if c.State == "succeeded" {
			if h.Success != nil {
				return h, ErrConflict
			}
			h.Success = c
		}
	}
	if h.Success != nil && h.Unresolved != nil {
		return h, ErrConflict
	}
	return h, nil
}

// prepare builds a typed pending-family operation. No fictitious v1 reward is
// inserted for money that has not been credited to the promoter.
func (s *Store) prepareReferralDailyRefund(ctx context.Context, tx pgx.Tx, o Order, r Refund, priorFee int64) (*referralClawbackRecord, bool, error) {
	t, e := s.readReferralTerms(ctx, tx, o)
	if errors.Is(e, ErrNotFound) {
		return nil, false, nil
	}
	if e != nil {
		return nil, true, e
	}
	if t.daily == nil {
		return nil, false, nil
	}
	a, state, _, e := s.readReferralDailyStateForRefund(ctx, tx, o, t, r.ID)
	if e != nil {
		return nil, true, e
	}
	if state == "awarded" {
		return nil, false, nil
	}
	if state == "voided" {
		return nil, true, nil
	}
	if state != "pending" {
		return nil, true, ErrState
	}
	if priorFee != 0 {
		return nil, true, ErrConflict
	}
	k, e := referralClawbackContribution(t.PlatformFee, t.RewardAmount, 0, r.FeeAmount)
	if e != nil {
		return nil, true, e
	}
	if e = lockMerchantForeignKeySet(ctx, tx, []int64{o.MerchantID, t.PromoterID}); e != nil {
		return nil, true, e
	}
	c := &referralDailyRefund{RefundID: r.ID, OrderID: o.ID, Amount: t.RewardAmount, OriginalFee: t.PlatformFee, RefundFee: r.FeeAmount, Contribution: k, PendingID: t.daily.PendingAccountID, FeesID: a.FeesAccountID, State: "pending"}
	if e = tx.QueryRow(ctx, `INSERT INTO accounts(owner_id,kind,scope_id)VALUES(0,'referral_daily_refund_reserve',$1)RETURNING id`, r.ID).Scan(&c.EscrowID); e != nil {
		return nil, true, e
	}
	if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE owner_id=0 AND kind='fee_reserved' AND scope_id=0`).Scan(&c.FeeReservedID); e != nil {
		return nil, true, e
	}
	return dailyRefundWrapper70(*c, t), true, nil
}
func dailyRefundWrapper70(c referralDailyRefund, t referralOrderTerms) *referralClawbackRecord {
	return &referralClawbackRecord{daily: &c, RefundID: c.RefundID, OrderID: c.OrderID, MerchantID: t.MerchantID, PromoterID: t.PromoterID, Amount: c.Amount, OriginalFee: c.OriginalFee, RefundFee: c.RefundFee, FeeContribution: c.Contribution, EscrowAccountID: c.EscrowID, PromoterAccountID: c.PendingID, FeesAccountID: c.FeesID, FeeReservedAccountID: c.FeeReservedID, State: c.State}
}
func dailyRefundJournal70(ctx context.Context, tx pgx.Tx, c referralDailyRefund, suffix string, from, to, amount int64) (int64, error) {
	if amount == 0 {
		return 0, nil
	}
	key := fmt.Sprintf("referral-daily-refund:%d:%s", c.RefundID, suffix)
	if e := postByAccountIDs(ctx, tx, key, "original deferred referral "+suffix, from, to, amount); e != nil {
		return 0, e
	}
	var id int64
	e := tx.QueryRow(ctx, `SELECT id FROM journals WHERE event_key=$1`, key).Scan(&id)
	return id, e
}
func (s *Store) reserveReferralDailyRefund(ctx context.Context, tx pgx.Tx, c *referralDailyRefund, r Refund) error {
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", c.OrderID))
	if e != nil {
		return e
	}
	t, e := s.readReferralTerms(ctx, tx, o)
	if e != nil {
		return e
	}
	a, e := s.readReferralDailyActivation(ctx, tx, o, *t.daily)
	if e != nil {
		return e
	}
	c.At, e = dailyDBClock70(ctx, tx)
	if e != nil {
		return e
	}
	c.ReserveJournal, e = dailyRefundJournal70(ctx, tx, *c, "reserve", c.PendingID, c.EscrowID, c.Amount)
	if e != nil {
		return e
	}
	c.ContributionJournal, e = dailyRefundJournal70(ctx, tx, *c, "fee-contribution", c.EscrowID, c.FeesID, c.Contribution)
	if e != nil {
		return e
	}
	c.RequestHash, e = s.dailyRefundRequestProof(*c, r, a)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO referral_daily_refund_reservations(refund_id,order_id,amount,original_fee,refund_fee,fee_contribution,pending_account_id,escrow_account_id,fees_account_id,fee_reserved_account_id,reserve_journal_id,contribution_journal_id,request_hash,created_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULLIF($12::bigint,0),$13,$14)`, c.RefundID, c.OrderID, c.Amount, c.OriginalFee, c.RefundFee, c.Contribution, c.PendingID, c.EscrowID, c.FeesID, c.FeeReservedID, c.ReserveJournal, c.ContributionJournal, c.RequestHash, c.At)
	if e != nil {
		return e
	}
	return s.insertDailyEvent(ctx, tx, c.OrderID, "refund-pending", c.RefundID, "refund-engine", c.RequestHash)
}
func (s *Store) resolveReferralDailyRefund(ctx context.Context, tx pgx.Tx, c *referralDailyRefund, state string) error {
	var e error
	switch state {
	case "failed":
		c.RestoreContributionJournal, e = dailyRefundJournal70(ctx, tx, *c, "restore-contribution", c.FeesID, c.EscrowID, c.Contribution)
		if e != nil {
			return e
		}
		c.RestorePendingJournal, e = dailyRefundJournal70(ctx, tx, *c, "restore-pending", c.EscrowID, c.PendingID, c.Amount)
	case "succeeded":
		c.ReleaseJournal, e = dailyRefundJournal70(ctx, tx, *c, "release-remainder", c.EscrowID, c.FeesID, c.Amount-c.Contribution)
	case "unknown":
	default:
		return ErrInvalid
	}
	if e != nil {
		return e
	}
	c.PreviousHash = append([]byte(nil), c.ResultHash...)
	c.State = state
	at, e := dailyDBClock70(ctx, tx)
	if e != nil {
		return e
	}
	c.ResultAt = &at
	c.ResultHash, e = s.dailyRefundResultProof(*c)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE referral_daily_refund_reservations SET state=$2,restore_contribution_journal_id=NULLIF($3::bigint,0),restore_pending_journal_id=NULLIF($4::bigint,0),release_remainder_journal_id=NULLIF($5::bigint,0),result_hash=$6,previous_result_hash=$7,result_at=$8 WHERE refund_id=$1`, c.RefundID, c.State, c.RestoreContributionJournal, c.RestorePendingJournal, c.ReleaseJournal, c.ResultHash, c.PreviousHash, c.ResultAt)
	if e != nil {
		return e
	}
	return s.insertDailyEvent(ctx, tx, c.OrderID, "refund-"+state, c.RefundID, "refund-engine", c.ResultHash)
}

// The original resolver invokes this explicit loader before its existing
// complete account lock and fee release, then dispatches the typed operation.
func (s *Store) loadReferralClawbackForRefund(ctx context.Context, tx pgx.Tx, id int64) (referralClawbackRecord, error) {
	old, e := s.scanReferralClawback(ctx, tx, tx.QueryRow(ctx, "SELECT "+referralClawbackColumns+" FROM referral_refund_clawbacks WHERE refund_id=$1", id))
	if !errors.Is(e, ErrNotFound) {
		return old, e
	}
	r, e := scanRefund(tx.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1", id))
	if e != nil {
		return old, e
	}
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", r.OrderID))
	if e != nil {
		return old, e
	}
	t, e := s.readReferralTerms(ctx, tx, o)
	if e != nil {
		return old, e
	}
	if t.daily == nil {
		return old, ErrNotFound
	}
	a, e := s.readReferralDailyActivation(ctx, tx, o, *t.daily)
	if e != nil {
		return old, e
	}
	c, e := s.scanDailyRefund(ctx, tx, tx.QueryRow(ctx, "SELECT "+dailyRefundColumns70+" FROM referral_daily_refund_reservations WHERE refund_id=$1", id), *t.daily, a)
	if e != nil {
		return old, e
	}
	return *dailyRefundWrapper70(c, t), nil
}
func (s *Store) verifyDailyRefundEvidence(ctx context.Context, q referralReader, r Refund) (bool, error) {
	o, e := scanOrder(q.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", r.OrderID))
	if e != nil {
		return true, e
	}
	t, e := s.readReferralTerms(ctx, q, o)
	if errors.Is(e, ErrNotFound) {
		return false, nil
	}
	if e != nil {
		return true, e
	}
	if t.daily == nil {
		return false, nil
	}
	a, state, _, e := s.readReferralDailyState(ctx, q, o, t)
	if e != nil {
		return true, e
	}
	h, e := s.readDailyRefundHistory(ctx, q, *t.daily, a)
	if e != nil {
		return true, e
	}
	for _, c := range h.Items {
		if c.RefundID == r.ID {
			return true, nil
		}
	}
	if state == "awarded" {
		return false, nil
	}
	if state == "voided" && h.Success != nil && h.Success.RefundID < r.ID {
		return true, nil
	}
	return true, ErrConflict
}
func (s *Store) dailyRefundPreview(ctx context.Context, q referralReader, o Order, no string, amount int64) (ReferralRefundSnapshot, bool, error) {
	var p ReferralRefundSnapshot
	t, e := s.readReferralTerms(ctx, q, o)
	if errors.Is(e, ErrNotFound) {
		return p, false, nil
	}
	if e != nil {
		return p, true, e
	}
	if t.daily == nil {
		return p, false, nil
	}
	a, state, _, e := s.readReferralDailyState(ctx, q, o, t)
	if e != nil {
		return p, true, e
	}
	h, e := s.readDailyRefundHistory(ctx, q, *t.daily, a)
	if e != nil {
		return p, true, e
	}
	r, re := scanRefund(q.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE merchant_id=$1 AND refund_no=$2", o.MerchantID, no))
	if re != nil && !errors.Is(re, ErrNotFound) {
		return p, true, re
	}
	if re == nil && (r.OrderID != o.ID || r.Amount != amount) {
		return p, true, ErrConflict
	}
	p.PromoterID, p.OriginalFee, p.RewardHash = t.PromoterID, o.PlatformFee, a.Proof
	for _, c := range h.Items {
		if re == nil && c.RefundID == r.ID {
			p.ExistingRefundID, p.ClawbackRefundID, p.Amount, p.RefundFee, p.FeeContribution = r.ID, r.ID, c.Amount, c.RefundFee, c.Contribution
			p.ClawbackState = "daily_" + c.State
			p.RequestHash, p.ResultHash = c.RequestHash, c.ResultHash
			return p, true, nil
		}
	}
	if state == "awarded" {
		return p, false, nil
	}
	if re == nil {
		p.ExistingRefundID = r.ID
	}
	funding, e := s.RefundFundingPreview(ctx, q, o.ID, no, amount)
	if e != nil {
		return p, true, e
	}
	p.RefundFee = funding.FeeAmount
	if state == "voided" && h.Success != nil {
		p.ClawbackState = "daily_voided"
		p.ClawbackRefundID = h.Success.RefundID
		p.RequestHash, p.ResultHash = h.Success.RequestHash, h.Success.ResultHash
		return p, true, nil
	}
	if state != "pending" {
		return p, true, ErrState
	}
	if e = q.QueryRow(ctx, `SELECT COALESCE(sum(fee_amount),0) FROM refunds WHERE order_id=$1 AND status<>'failed'`, o.ID).Scan(&p.PriorFeeUsed); e != nil {
		return p, true, e
	}
	if p.PriorFeeUsed != 0 {
		return p, true, ErrConflict
	}
	p.Amount = t.RewardAmount
	p.ClawbackState = "daily_pending"
	// A restored pending liability is a new authorization state. Bind the
	// most recent keyed failure so a quote made before an unknown refund
	// cannot become valid again after its eventual failure.
	if len(h.Items) > 0 {
		last := h.Items[len(h.Items)-1]
		p.ClawbackRefundID = last.RefundID
		p.RequestHash, p.ResultHash = last.RequestHash, last.ResultHash
	}
	p.FeeContribution, e = referralClawbackContribution(p.OriginalFee, p.Amount, 0, p.RefundFee)
	return p, true, e
}
