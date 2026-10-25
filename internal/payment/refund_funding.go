package payment

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

type refundFundingResult struct {
	RefundID                                      int64
	State                                         string
	HeldJournal, AvailableJournal, ConsumeJournal int64
	Request, Proof                                []byte
	At                                            time.Time
}
type refundFundingRecord struct {
	RefundID, OrderID, MerchantID, HoldID, Merchant, Held, Available, Fee, AccountID, HeldJournal, AvailableJournal int64
	HoldHash, Proof                                                                                                 []byte
	At                                                                                                              time.Time
	Result                                                                                                          refundFundingResult
}

func (s *Store) refundFundingProof(f refundFundingRecord, r Refund, o Order) ([]byte, error) {
	return s.referralDigest("refund-original-funding", []any{f.RefundID, f.OrderID, f.MerchantID, f.HoldID, f.Merchant, f.Held, f.Available, f.Fee, f.AccountID, f.HeldJournal, f.AvailableJournal, f.HoldHash, f.At.UTC().Format(time.RFC3339Nano), r.RefundNo, r.Amount, o.Amount, o.MerchantAmount, o.PlatformFee, o.RefundFeePayer})
}
func (s *Store) refundFundingResultProof(z refundFundingResult) ([]byte, error) {
	return s.referralDigest("refund-original-funding-result", []any{z.RefundID, z.State, z.HeldJournal, z.AvailableJournal, z.ConsumeJournal, z.Request, z.At.UTC().Format(time.RFC3339Nano)})
}
func (s *Store) scanRefundFunding(ctx context.Context, q referralReader, r Refund) (refundFundingRecord, error) {
	var f refundFundingRecord
	e := q.QueryRow(ctx, `SELECT refund_id,order_id,merchant_id,COALESCE(hold_id,0),merchant_amount,hold_amount,available_amount,fee_amount,reserve_account_id,COALESCE(hold_reserve_journal_id,0),COALESCE(available_reserve_journal_id,0),hold_funding_hash,request_hash,created_at FROM refund_fundings WHERE refund_id=$1`, r.ID).Scan(&f.RefundID, &f.OrderID, &f.MerchantID, &f.HoldID, &f.Merchant, &f.Held, &f.Available, &f.Fee, &f.AccountID, &f.HeldJournal, &f.AvailableJournal, &f.HoldHash, &f.Proof, &f.At)
	if errors.Is(e, pgx.ErrNoRows) {
		var version int
		if e = q.QueryRow(ctx, "SELECT funding_evidence_version FROM refunds WHERE id=$1", r.ID).Scan(&version); e != nil {
			return f, e
		}
		if version != 1 {
			return f, ErrConflict
		}
		return f, ErrNotFound
	}
	if e != nil {
		return f, e
	}
	o, e := scanOrder(q.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", r.OrderID))
	if e != nil {
		return f, e
	}
	proof, e := s.refundFundingProof(f, r, o)
	if e != nil || !hmac.Equal(proof, f.Proof) || f.OrderID != r.OrderID || f.MerchantID != r.MerchantID || f.Merchant != r.MerchantAmount || f.Fee != r.FeeAmount || f.Held < 0 || f.Available < 0 || f.Held > f.Merchant || f.Available != f.Merchant-f.Held {
		return f, ErrConflict
	}
	if f.HoldID > 0 {
		h, e := scanIncomeHold(q.QueryRow(ctx, "SELECT "+incomeHoldColumns+" FROM order_income_holds h JOIN orders o ON o.id=h.order_id WHERE h.id=$1", f.HoldID))
		if e != nil {
			return f, e
		}
		hf, e := s.scanIncomeFunding(ctx, q, h)
		if e != nil || h.OrderID != r.OrderID || !hmac.Equal(hf.Proof, f.HoldHash) {
			return f, ErrConflict
		}
	}
	var valid bool
	if e = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM refund_funding_invariants WHERE refund_id=$1 AND valid IS TRUE)", r.ID).Scan(&valid); e != nil {
		return f, e
	}
	if !valid {
		return f, ErrConflict
	}
	z := refundFundingResult{}
	e = q.QueryRow(ctx, `SELECT refund_id,state,COALESCE(held_return_journal_id,0),COALESCE(available_return_journal_id,0),COALESCE(consume_journal_id,0),request_hash,result_hash,created_at FROM refund_funding_resolutions WHERE refund_id=$1`, r.ID).Scan(&z.RefundID, &z.State, &z.HeldJournal, &z.AvailableJournal, &z.ConsumeJournal, &z.Request, &z.Proof, &z.At)
	if errors.Is(e, pgx.ErrNoRows) {
		if r.Status == "succeeded" || r.Status == "failed" {
			return f, ErrConflict
		}
		return f, nil
	}
	if e != nil {
		return f, e
	}
	proof, e = s.refundFundingResultProof(z)
	if e != nil || z.State != r.Status || !hmac.Equal(z.Request, f.Proof) || !hmac.Equal(proof, z.Proof) {
		return f, ErrConflict
	}
	f.Result = z
	return f, nil
}
func (s *Store) verifyRefundFundingEvidence(ctx context.Context, q referralReader, r Refund) error {
	_, e := s.scanRefundFunding(ctx, q, r)
	if errors.Is(e, ErrNotFound) {
		return nil
	}
	return e
}
func (s *Store) verifyOrderRefundFundingHistory(ctx context.Context, q referralReader, oid int64) error {
	for cursor := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, q, "SELECT id FROM refunds WHERE order_id=$1 AND id>$2 ORDER BY id LIMIT 100", oid, cursor)
		if e != nil {
			return e
		}
		for _, id := range ids {
			r, e := scanRefund(q.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1", id))
			if e != nil {
				return e
			}
			if e = s.verifyProviderRefundResult(ctx, q, r); e != nil {
				return e
			}
			if e = s.verifyRefundFundingEvidence(ctx, q, r); e != nil {
				return e
			}
			cursor = id
		}
		if len(ids) < 100 {
			return nil
		}
	}
}
func (s *Store) prepareRefundFunding(ctx context.Context, tx pgx.Tx, o Order, r Refund, p RefundFundingSnapshot) (refundFundingRecord, error) {
	f := refundFundingRecord{RefundID: r.ID, OrderID: o.ID, MerchantID: o.MerchantID, HoldID: p.HoldID, Merchant: r.MerchantAmount, Held: p.HeldAmount, Available: p.AvailableAmount, Fee: r.FeeAmount, HoldHash: p.SourceHash}
	if f.Merchant != p.MerchantAmount || f.Fee != p.FeeAmount {
		return f, ErrConflict
	}
	if e := lockMerchantForeignKeySet(ctx, tx, []int64{o.MerchantID}); e != nil {
		return f, e
	}
	if f.HoldID > 0 {
		if e := tx.QueryRow(ctx, "INSERT INTO accounts(owner_id,kind,scope_id) VALUES($1,'order_income_refund_reserve',$2) RETURNING id", o.MerchantID, r.ID).Scan(&f.AccountID); e != nil {
			return f, e
		}
	} else {
		if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='reserved' AND scope_id=0", o.MerchantID).Scan(&f.AccountID); e != nil {
			return f, e
		}
	}
	return f, nil
}
func (s *Store) reserveRefundFunding(ctx context.Context, tx pgx.Tx, o Order, r Refund, f *refundFundingRecord) error {
	var av, held int64
	if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", r.MerchantID).Scan(&av); e != nil {
		return e
	}
	if f.HoldID > 0 {
		if e := tx.QueryRow(ctx, "SELECT account_id FROM order_income_hold_fundings WHERE hold_id=$1", f.HoldID).Scan(&held); e != nil {
			return e
		}
	}
	var e error
	f.HeldJournal, e = incomeJournal(ctx, tx, fmt.Sprintf("refund-hold:%d:reserve", r.ID), held, f.AccountID, f.Held)
	if e != nil {
		return e
	}
	f.AvailableJournal, e = incomeJournal(ctx, tx, fmt.Sprintf("refund:%d:reserve", r.ID), av, f.AccountID, f.Available)
	if e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&f.At); e != nil {
		return e
	}
	f.Proof, e = s.refundFundingProof(*f, r, o)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO refund_fundings(refund_id,order_id,merchant_id,hold_id,merchant_amount,hold_amount,available_amount,fee_amount,reserve_account_id,hold_reserve_journal_id,available_reserve_journal_id,hold_funding_hash,request_hash,created_at) VALUES($1,$2,$3,NULLIF($4::bigint,0),$5,$6,$7,$8,$9,NULLIF($10::bigint,0),NULLIF($11::bigint,0),$12,$13,$14)`, f.RefundID, f.OrderID, f.MerchantID, f.HoldID, f.Merchant, f.Held, f.Available, f.Fee, f.AccountID, f.HeldJournal, f.AvailableJournal, f.HoldHash, f.Proof, f.At); e != nil {
		return e
	}
	return incomeAudit(ctx, tx, "refund-fundings", r.ID, "reserve", nil, map[string]string{"hold_id": fmt.Sprint(f.HoldID), "held": fmt.Sprint(f.Held), "available": fmt.Sprint(f.Available), "fee": fmt.Sprint(f.Fee)})
}
func (s *Store) resolveRefundFunding(ctx context.Context, tx pgx.Tx, r Refund, f refundFundingRecord, state string) error {
	if state == "unknown" {
		return nil
	}
	z := refundFundingResult{RefundID: r.ID, State: state, Request: f.Proof}
	var e error
	var target int64
	if state == "succeeded" {
		if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=0 AND kind='clearing' AND scope_id=0").Scan(&target); e != nil {
			return e
		}
		z.ConsumeJournal, e = incomeJournal(ctx, tx, fmt.Sprintf("refund:%d:resolve", r.ID), f.AccountID, target, f.Merchant)
	} else {
		if f.Held > 0 {
			if e = tx.QueryRow(ctx, "SELECT account_id FROM order_income_hold_fundings WHERE hold_id=$1", f.HoldID).Scan(&target); e != nil {
				return e
			}
			z.HeldJournal, e = incomeJournal(ctx, tx, fmt.Sprintf("refund-hold:%d:resolve", r.ID), f.AccountID, target, f.Held)
			if e != nil {
				return e
			}
		}
		if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", r.MerchantID).Scan(&target); e != nil {
			return e
		}
		z.AvailableJournal, e = incomeJournal(ctx, tx, fmt.Sprintf("refund:%d:resolve", r.ID), f.AccountID, target, f.Available)
	}
	if e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&z.At); e != nil {
		return e
	}
	z.Proof, e = s.refundFundingResultProof(z)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO refund_funding_resolutions(refund_id,state,held_return_journal_id,available_return_journal_id,consume_journal_id,request_hash,result_hash,created_at) VALUES($1,$2,NULLIF($3::bigint,0),NULLIF($4::bigint,0),NULLIF($5::bigint,0),$6,$7,$8)`, r.ID, state, z.HeldJournal, z.AvailableJournal, z.ConsumeJournal, z.Request, z.Proof, z.At); e != nil {
		return e
	}
	return incomeAudit(ctx, tx, "refund-fundings", r.ID, state, nil, map[string]string{"state": state, "hold_id": fmt.Sprint(f.HoldID)})
}
