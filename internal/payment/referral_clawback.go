package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// referralClawbackContribution uses the original order fee rather than a
// pooled platform balance. Subtraction is ordered to avoid integer overflow.
func referralClawbackContribution(fee, reward, priorFee, refundFee int64) (int64, error) {
	if fee < 0 || reward < 0 || reward > fee || priorFee < 0 || priorFee > fee-reward || refundFee < 0 || refundFee > fee-priorFee {
		return 0, ErrState
	}
	unspent := fee - reward - priorFee
	if refundFee <= unspent {
		return 0, nil
	}
	return refundFee - unspent, nil
}

type referralClawbackRecord struct {
	daily                                                                           *referralDailyRefund
	RefundID, RewardID, OrderID, MerchantID, PromoterID                             int64
	Amount, OriginalFee, PriorFeeUsed, RefundFee, FeeContribution                   int64
	EscrowAccountID, PromoterAccountID, FeesAccountID, FeeReservedAccountID         int64
	ReserveJournalID, ContributionJournalID                                         int64
	RestoreContributionJournalID, RestoreRewardJournalID, ReleaseRemainderJournalID int64
	RewardHash, RequestHash, ResultHash                                             []byte
	State                                                                           string
}

const referralClawbackColumns = `refund_id,reward_id,order_id,merchant_id,promoter_id,amount,original_fee,prior_fee_used,refund_fee,fee_contribution,escrow_account_id,promoter_account_id,fees_account_id,fee_reserved_account_id,reserve_journal_id,COALESCE(contribution_journal_id,0),COALESCE(restore_contribution_journal_id,0),COALESCE(restore_reward_journal_id,0),COALESCE(release_remainder_journal_id,0),reward_hash,request_hash,result_hash,state`

func (s *Store) referralClawbackProof(c referralClawbackRecord, r Refund) ([]byte, error) {
	return s.referralDigest("refund-clawback-request", []any{c.RefundID, c.RewardID, c.OrderID, c.MerchantID, c.PromoterID, c.Amount, c.OriginalFee, c.PriorFeeUsed, c.RefundFee, c.FeeContribution, c.EscrowAccountID, c.PromoterAccountID, c.FeesAccountID, c.FeeReservedAccountID, c.ReserveJournalID, c.ContributionJournalID, hex.EncodeToString(c.RewardHash), r.RefundNo, r.Amount, r.MerchantAmount, r.FeeAmount, referralRefundPolicy})
}
func (s *Store) referralClawbackResultProof(c referralClawbackRecord) ([]byte, error) {
	return s.referralDigest("refund-clawback-result", []any{c.RefundID, fmt.Sprintf("RF%d", c.RefundID), hex.EncodeToString(c.RequestHash), c.State, c.RestoreContributionJournalID, c.RestoreRewardJournalID, c.ReleaseRemainderJournalID})
}

func (s *Store) scanReferralClawback(ctx context.Context, tx referralReader, row pgx.Row) (referralClawbackRecord, error) {
	var c referralClawbackRecord
	err := row.Scan(&c.RefundID, &c.RewardID, &c.OrderID, &c.MerchantID, &c.PromoterID, &c.Amount, &c.OriginalFee, &c.PriorFeeUsed, &c.RefundFee, &c.FeeContribution, &c.EscrowAccountID, &c.PromoterAccountID, &c.FeesAccountID, &c.FeeReservedAccountID, &c.ReserveJournalID, &c.ContributionJournalID, &c.RestoreContributionJournalID, &c.RestoreRewardJournalID, &c.ReleaseRemainderJournalID, &c.RewardHash, &c.RequestHash, &c.ResultHash, &c.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	r, err := scanRefund(tx.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1", c.RefundID))
	if err != nil {
		return c, err
	}
	source, err := s.referralRefundSource(ctx, tx, c.OrderID)
	if err != nil {
		return c, err
	}
	if source == nil || source.RewardID != c.RewardID || source.OrderID != r.OrderID || source.MerchantID != r.MerchantID || c.MerchantID != r.MerchantID || source.PromoterID != c.PromoterID || source.Amount != c.Amount || source.PlatformFee != c.OriginalFee || !hmac.Equal(source.RequestHash, c.RewardHash) || c.State != r.Status {
		return c, ErrConflict
	}
	k, err := referralClawbackContribution(c.OriginalFee, c.Amount, c.PriorFeeUsed, c.RefundFee)
	if err != nil || k != c.FeeContribution || r.FeeAmount != c.RefundFee {
		return c, ErrConflict
	}
	proof, err := s.referralClawbackProof(c, r)
	if err != nil || !hmac.Equal(proof, c.RequestHash) {
		return c, ErrConflict
	}
	if c.State == "pending" {
		if len(c.ResultHash) != 0 {
			return c, ErrConflict
		}
	} else {
		proof, err = s.referralClawbackResultProof(c)
		if err != nil || !hmac.Equal(proof, c.ResultHash) {
			return c, ErrConflict
		}
	}
	var valid bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM referral_clawback_invariants WHERE refund_id=$1 AND valid IS TRUE)", c.RefundID).Scan(&valid); err != nil {
		return c, err
	}
	if !valid {
		return c, ErrConflict
	}
	return c, nil
}

// Every historical failure must be keyed and restored. A fabricated terminal
// projection must never reopen an award for a new refund number.
type referralClawbackSummary struct {
	Count                       int64
	CurrentFound, HasUnresolved bool
	SuccessRefundID             int64
	LastRefundID                int64
}

func (s *Store) referralClawbackHistory(ctx context.Context, tx referralReader, orderID, currentRefundID int64) (referralClawbackSummary, error) {
	var result referralClawbackSummary
	var cursor int64
	for {
		rows, err := tx.Query(ctx, "SELECT refund_id FROM referral_refund_clawbacks WHERE order_id=$1 AND refund_id>$2 ORDER BY refund_id LIMIT 100", orderID, cursor)
		if err != nil {
			return result, err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return result, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, err
		}
		for _, id := range ids {
			c, err := s.scanReferralClawback(ctx, tx, tx.QueryRow(ctx, "SELECT "+referralClawbackColumns+" FROM referral_refund_clawbacks WHERE refund_id=$1", id))
			if err != nil {
				return result, err
			}
			result.Count++
			result.LastRefundID = id
			if c.RefundID == currentRefundID {
				result.CurrentFound = true
			}
			if c.State == "pending" || c.State == "unknown" {
				result.HasUnresolved = true
			}
			if c.State == "succeeded" {
				if result.SuccessRefundID != 0 {
					return result, ErrConflict
				}
				result.SuccessRefundID = c.RefundID
			}
			cursor = id
		}
		if len(ids) < 100 {
			return result, nil
		}
	}
}

func (s *Store) verifyReferralRefundEvidence(ctx context.Context, tx referralReader, r Refund) error {
	if err := s.verifyProviderRefundResult(ctx, tx, r); err != nil {
		return err
	}
	if err := s.verifyRefundFundingEvidence(ctx, tx, r); err != nil {
		return err
	}
	if handled, err := s.verifyDailyRefundEvidence(ctx, tx, r); handled || err != nil {
		return err
	}
	source, err := s.referralRefundSource(ctx, tx, r.OrderID)
	if err != nil {
		return err
	}
	history, err := s.referralClawbackHistory(ctx, tx, r.OrderID, r.ID)
	if err != nil {
		return err
	}
	if source == nil {
		if history.Count != 0 {
			return ErrConflict
		}
		return nil
	}
	if history.CurrentFound {
		return nil
	}
	// Later refunds on an already revoked reward have no second escrow. The
	// successful clawback must belong to a strictly earlier original refund.
	if history.SuccessRefundID > 0 && history.SuccessRefundID < r.ID {
		return nil
	}
	return ErrConflict
}

func (s *Store) prepareReferralClawback(ctx context.Context, tx pgx.Tx, o Order, r Refund, priorFee int64) (*referralClawbackRecord, error) {
	if c, handled, err := s.prepareReferralDailyRefund(ctx, tx, o, r, priorFee); handled || err != nil {
		return c, err
	}
	source, err := s.referralRefundSource(ctx, tx, o.ID)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, nil
	}
	history, err := s.referralClawbackHistory(ctx, tx, o.ID, r.ID)
	if err != nil {
		return nil, err
	}
	if history.SuccessRefundID > 0 {
		return nil, nil
	}
	if history.HasUnresolved {
		return nil, ErrState
	}
	var unexplained bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM refunds f WHERE f.order_id=$1 AND f.id<>$2 AND (f.status<>'failed' OR (NOT EXISTS(SELECT 1 FROM referral_refund_clawbacks c WHERE c.refund_id=f.id AND c.state='failed') AND NOT EXISTS(SELECT 1 FROM referral_daily_refund_reservations c WHERE c.refund_id=f.id AND c.order_id=f.order_id AND c.state='failed'))))`, o.ID, r.ID).Scan(&unexplained); err != nil {
		return nil, err
	}
	if unexplained {
		return nil, ErrConflict
	}
	k, err := referralClawbackContribution(source.PlatformFee, source.Amount, priorFee, r.FeeAmount)
	if err != nil {
		return nil, err
	}
	// The later clawback INSERT must take these merchant FK KEY SHARE locks.
	// Acquire them before any wallet lock so membership/deposit's existing
	// merchant -> wallet order cannot be reversed by an implicit FK check.
	if err = lockMerchantForeignKeySet(ctx, tx, []int64{o.MerchantID, source.PromoterID}); err != nil {
		return nil, err
	}
	c := &referralClawbackRecord{RefundID: r.ID, RewardID: source.RewardID, OrderID: o.ID, MerchantID: o.MerchantID, PromoterID: source.PromoterID, Amount: source.Amount, OriginalFee: source.PlatformFee, PriorFeeUsed: priorFee, RefundFee: r.FeeAmount, FeeContribution: k, RewardHash: source.RequestHash, State: "pending"}
	if err = tx.QueryRow(ctx, "INSERT INTO accounts(owner_id,kind,scope_id) VALUES(0,'referral_clawback_reserve',$1) RETURNING id", r.ID).Scan(&c.EscrowAccountID); err != nil {
		return nil, err
	}
	for _, a := range []struct {
		owner int64
		kind  string
		id    *int64
	}{{c.PromoterID, "available", &c.PromoterAccountID}, {0, "fees", &c.FeesAccountID}, {0, "fee_reserved", &c.FeeReservedAccountID}} {
		if err = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind=$2 AND scope_id=0", a.owner, a.kind).Scan(a.id); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func lockRefundLedgerAccounts(ctx context.Context, tx pgx.Tx, r Refund, c *referralClawbackRecord, state string, funding ...*refundFundingRecord) error {
	if state == "unknown" {
		return nil
	}
	if state != "reserve" && state != "succeeded" && state != "failed" {
		return ErrInvalid
	}
	var ids []int64
	add := func(owner int64, kind string) error {
		var id int64
		if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind=$2 AND scope_id=0", owner, kind).Scan(&id); e != nil {
			return e
		}
		ids = append(ids, id)
		return nil
	}
	var f *refundFundingRecord
	if len(funding) > 0 {
		f = funding[0]
	}
	if f != nil {
		ids = append(ids, f.AccountID)
		if f.Held > 0 {
			var id int64
			if e := tx.QueryRow(ctx, "SELECT account_id FROM order_income_hold_fundings WHERE hold_id=$1", f.HoldID).Scan(&id); e != nil {
				return e
			}
			ids = append(ids, id)
		}
		if state == "succeeded" {
			if r.MerchantAmount > 0 {
				if e := add(0, "clearing"); e != nil {
					return e
				}
			}
		} else if f.Available > 0 {
			if e := add(r.MerchantID, "available"); e != nil {
				return e
			}
		}
	} else if r.MerchantAmount > 0 {
		if e := add(r.MerchantID, "reserved"); e != nil {
			return e
		}
		if state == "succeeded" {
			if e := add(0, "clearing"); e != nil {
				return e
			}
		} else {
			if e := add(r.MerchantID, "available"); e != nil {
				return e
			}
		}
	}
	if r.FeeAmount > 0 {
		if e := add(0, "fee_reserved"); e != nil {
			return e
		}
		if state == "succeeded" {
			if e := add(0, "clearing"); e != nil {
				return e
			}
		} else {
			if e := add(0, "fees"); e != nil {
				return e
			}
		}
	}
	if c != nil {
		if state != "succeeded" || c.Amount > c.FeeContribution {
			ids = append(ids, c.EscrowAccountID)
		}
		if state != "succeeded" {
			ids = append(ids, c.PromoterAccountID)
		}
		if c.FeeContribution > 0 || state == "succeeded" && c.Amount > c.FeeContribution {
			ids = append(ids, c.FeesAccountID)
		}
	}
	return lockLedgerAccountSet(ctx, tx, ids)
}

func referralClawbackJournal(ctx context.Context, tx pgx.Tx, c referralClawbackRecord, suffix string, from, to, amount int64) (int64, error) {
	if amount == 0 {
		return 0, nil
	}
	key := fmt.Sprintf("referral-clawback:%d:%s", c.RefundID, suffix)
	if err := postByAccountIDs(ctx, tx, key, "original referral reward "+suffix, from, to, amount); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRow(ctx, "SELECT id FROM journals WHERE event_key=$1", key).Scan(&id)
	return id, err
}
func (s *Store) reserveReferralClawback(ctx context.Context, tx pgx.Tx, c *referralClawbackRecord, r Refund) error {
	if c == nil {
		return nil
	}
	if c.daily != nil {
		return s.reserveReferralDailyRefund(ctx, tx, c.daily, r)
	}
	var err error
	if c.ReserveJournalID, err = referralClawbackJournal(ctx, tx, *c, "reserve", c.PromoterAccountID, c.EscrowAccountID, c.Amount); err != nil {
		return err
	}
	if c.ContributionJournalID, err = referralClawbackJournal(ctx, tx, *c, "fee-contribution", c.EscrowAccountID, c.FeesAccountID, c.FeeContribution); err != nil {
		return err
	}
	if c.RequestHash, err = s.referralClawbackProof(*c, r); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO referral_refund_clawbacks(refund_id,reward_id,order_id,merchant_id,promoter_id,amount,original_fee,prior_fee_used,refund_fee,fee_contribution,escrow_account_id,promoter_account_id,fees_account_id,fee_reserved_account_id,reserve_journal_id,contribution_journal_id,reward_hash,request_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,NULLIF($16::bigint,0),$17,$18)`, c.RefundID, c.RewardID, c.OrderID, c.MerchantID, c.PromoterID, c.Amount, c.OriginalFee, c.PriorFeeUsed, c.RefundFee, c.FeeContribution, c.EscrowAccountID, c.PromoterAccountID, c.FeesAccountID, c.FeeReservedAccountID, c.ReserveJournalID, c.ContributionJournalID, c.RewardHash, c.RequestHash)
	if err != nil {
		return err
	}
	return insertReferralClawbackAudit(ctx, tx, *c, c.RequestHash)
}
func insertReferralClawbackAudit(ctx context.Context, tx pgx.Tx, c referralClawbackRecord, proof []byte) error {
	_, err := tx.Exec(ctx, `INSERT INTO referral_clawback_audits(refund_id,state,event_key,actor_role,actor_name,proof_hash) VALUES($1,$2,$3,'system','refund-engine',$4) ON CONFLICT(refund_id,state) DO NOTHING`, c.RefundID, c.State, fmt.Sprintf("referral-clawback:%d:%s", c.RefundID, c.State), proof)
	return err
}
func (s *Store) resolveReferralClawback(ctx context.Context, tx pgx.Tx, c *referralClawbackRecord, state string) error {
	if c == nil {
		return nil
	}
	if c.daily != nil {
		return s.resolveReferralDailyRefund(ctx, tx, c.daily, state)
	}
	var err error
	switch state {
	case "failed":
		if c.RestoreContributionJournalID, err = referralClawbackJournal(ctx, tx, *c, "restore-contribution", c.FeesAccountID, c.EscrowAccountID, c.FeeContribution); err != nil {
			return err
		}
		if c.RestoreRewardJournalID, err = referralClawbackJournal(ctx, tx, *c, "restore-reward", c.EscrowAccountID, c.PromoterAccountID, c.Amount); err != nil {
			return err
		}
	case "succeeded":
		if c.ReleaseRemainderJournalID, err = referralClawbackJournal(ctx, tx, *c, "release-remainder", c.EscrowAccountID, c.FeesAccountID, c.Amount-c.FeeContribution); err != nil {
			return err
		}
	case "unknown":
	default:
		return ErrInvalid
	}
	c.State = state
	if c.ResultHash, err = s.referralClawbackResultProof(*c); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE referral_refund_clawbacks SET state=$2,restore_contribution_journal_id=NULLIF($3::bigint,0),restore_reward_journal_id=NULLIF($4::bigint,0),release_remainder_journal_id=NULLIF($5::bigint,0),result_hash=$6,resolved_at=CASE WHEN $2 IN ('succeeded','failed') THEN transaction_timestamp() END WHERE refund_id=$1`, c.RefundID, c.State, c.RestoreContributionJournalID, c.RestoreRewardJournalID, c.ReleaseRemainderJournalID, c.ResultHash)
	if err != nil {
		return err
	}
	return insertReferralClawbackAudit(ctx, tx, *c, c.ResultHash)
}
