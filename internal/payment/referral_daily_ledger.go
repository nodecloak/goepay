package payment

import (
	"context"
	"crypto/hmac"
	"github.com/jackc/pgx/v5"
)

// Every daily source is checked from the original terms page, including
// unfunded intents. A missing activation or marker cannot vanish through a join.
func (s *Store) checkReferralDailySource(ctx context.Context, tx pgx.Tx, o Order, t referralOrderTerms) error {
	if o.Status == "paid" {
		_, _, _, e := s.readReferralDailyState(ctx, tx, o, t)
		return e
	}
	var valid bool
	e := tx.QueryRow(ctx, `SELECT a.balance=0 AND NOT EXISTS(SELECT 1 FROM referral_daily_liabilities l WHERE l.order_id=$1) AND NOT EXISTS(SELECT 1 FROM referral_rewards r WHERE r.order_id=$1) FROM accounts a WHERE a.id=$2`, o.ID, t.daily.PendingAccountID).Scan(&valid)
	if e != nil {
		return e
	}
	if !valid {
		return ErrConflict
	}
	return nil
}

func (s *Store) checkReferralDailyEvents(ctx context.Context, tx pgx.Tx, report *LedgerCheck) error {
	for cursor := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, tx, `SELECT id FROM referral_daily_events WHERE id>$1 ORDER BY id LIMIT 100`, cursor)
		if e != nil {
			return e
		}
		for _, id := range ids {
			var oid, ref int64
			var op, actor string
			var subject, expected []byte
			e = tx.QueryRow(ctx, `SELECT e.order_id,e.operation,e.reference_id,e.actor_name,e.subject_hash,
 CASE e.operation
 WHEN 'birth' THEN (SELECT d.source_hash FROM referral_daily_terms d WHERE d.order_id=e.order_id AND e.reference_id=0)
 WHEN 'funded' THEN (SELECT l.source_hash FROM referral_daily_liabilities l WHERE l.order_id=e.order_id AND e.reference_id=0)
 WHEN 'awarded' THEN (SELECT a.source_hash FROM referral_daily_awards a WHERE a.order_id=e.order_id AND a.reward_id=e.reference_id)
 WHEN 'refund-pending' THEN (SELECT c.request_hash FROM referral_daily_refund_reservations c WHERE c.order_id=e.order_id AND c.refund_id=e.reference_id)
 WHEN 'refund-unknown' THEN (SELECT CASE WHEN c.state='unknown' THEN c.result_hash ELSE c.previous_result_hash END FROM referral_daily_refund_reservations c WHERE c.order_id=e.order_id AND c.refund_id=e.reference_id)
 WHEN 'refund-failed' THEN (SELECT c.result_hash FROM referral_daily_refund_reservations c WHERE c.order_id=e.order_id AND c.refund_id=e.reference_id AND c.state='failed')
 WHEN 'refund-succeeded' THEN (SELECT c.result_hash FROM referral_daily_refund_reservations c WHERE c.order_id=e.order_id AND c.refund_id=e.reference_id AND c.state='succeeded') END
 FROM referral_daily_events e WHERE e.id=$1`, id).Scan(&oid, &op, &ref, &actor, &subject, &expected)
			if e != nil {
				return e
			}
			wantActor := "refund-engine"
			if op == "birth" || op == "funded" {
				wantActor = "payment-engine"
			} else if op == "awarded" {
				wantActor = "daily-referral-engine"
			}
			if len(expected) != 32 || !hmac.Equal(subject, expected) || actor != wantActor {
				report.InvalidReferralRewards++
			} else if e = s.verifyDailyEvent(ctx, tx, oid, op, ref, wantActor, expected); e != nil {
				if invalidReferralEvidence(e) {
					report.InvalidReferralRewards++
				} else {
					return e
				}
			}
			cursor = id
		}
		if len(ids) < 100 {
			return nil
		}
	}
}
func (s *Store) checkReferralDailyOrphans(ctx context.Context, tx pgx.Tx, report *LedgerCheck) error {
	var invalidTerms, invalidRewards, invalidRefunds int64
	e := tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM referral_daily_terms d WHERE NOT EXISTS(SELECT 1 FROM referral_order_terms t WHERE t.order_id=d.order_id)),
 (SELECT count(*) FROM accounts a WHERE a.kind='referral_daily_pending' AND NOT EXISTS(SELECT 1 FROM referral_daily_terms d WHERE d.pending_account_id=a.id))
 +(SELECT count(*) FROM referral_daily_liabilities l WHERE NOT referral_daily_liability_valid(l.order_id))
 +(SELECT count(*) FROM referral_daily_awards a WHERE NOT referral_daily_award_valid(a.order_id)),
 (SELECT count(*) FROM accounts a WHERE a.kind='referral_daily_refund_reserve' AND NOT EXISTS(SELECT 1 FROM referral_daily_refund_reservations c WHERE c.escrow_account_id=a.id))
 +(SELECT count(*) FROM referral_daily_refund_reservations c WHERE NOT referral_daily_refund_valid(c.refund_id))`).Scan(&invalidTerms, &invalidRewards, &invalidRefunds)
	if e != nil {
		return e
	}
	report.InvalidReferralTerms += invalidTerms
	report.InvalidReferralRewards += invalidRewards
	report.InvalidReferralClawbacks += invalidRefunds
	return s.checkReferralDailyEvents(ctx, tx, report)
}
