package payment

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func invalidReferralEvidence(err error) bool {
	return invalidSharingEvidence(err) || errors.Is(err, ErrInvalid)
}

// Structural SQL projections cannot authenticate keyed application evidence.
// Check each parent, then page through structurally valid records for HMAC
// verification; never let a missing joined projection disappear from a count.
func (s *Store) checkReferralLedger(ctx context.Context, tx pgx.Tx, report *LedgerCheck) error {
	err := tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM referral_rewards r WHERE NOT EXISTS(SELECT 1 FROM referral_reward_invariants v WHERE v.reward_id=r.id AND v.valid IS TRUE))
 +(SELECT count(*) FROM referral_order_terms t JOIN orders o ON o.id=t.order_id WHERE t.enabled AND o.status='paid' AND NOT EXISTS(SELECT 1 FROM referral_rewards r WHERE r.order_id=t.order_id)
 AND NOT referral_daily_liability_valid(t.order_id)),
 (SELECT count(*) FROM referral_refund_clawbacks c WHERE NOT EXISTS(SELECT 1 FROM referral_clawback_invariants v WHERE v.refund_id=c.refund_id AND v.valid IS TRUE))
 +(SELECT count(*) FROM accounts a WHERE a.kind='referral_clawback_reserve' AND NOT EXISTS(SELECT 1 FROM referral_refund_clawbacks c WHERE c.escrow_account_id=a.id))
 +(SELECT count(*) FROM refunds f JOIN referral_rewards r ON r.order_id=f.order_id WHERE NOT EXISTS(SELECT 1 FROM referral_refund_clawbacks c WHERE c.refund_id=f.id)
 AND NOT EXISTS(SELECT 1 FROM referral_refund_clawbacks c WHERE c.reward_id=r.id AND c.state='succeeded' AND c.refund_id<f.id)
 AND NOT EXISTS(SELECT 1 FROM referral_daily_refund_reservations d WHERE d.refund_id=f.id AND d.state='failed' AND referral_daily_refund_valid(d.refund_id)))`).Scan(&report.InvalidReferralRewards, &report.InvalidReferralClawbacks)
	if err != nil {
		return err
	}
	for cursor := int64(0); ; {
		ids, err := sharingEvidenceIDsPage(ctx, tx, "SELECT order_id FROM (SELECT order_id FROM referral_order_terms UNION SELECT id FROM orders WHERE referral_source_version<>0 OR referral_source_hash IS NOT NULL) sources WHERE order_id>$1 ORDER BY order_id LIMIT 100", cursor)
		if err != nil {
			return err
		}
		for _, id := range ids {
			o, err := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", id))
			if err == nil {
				var terms referralOrderTerms
				terms, err = s.readReferralTerms(ctx, tx, o)
				if err == nil && terms.daily != nil {
					if e := s.checkReferralDailySource(ctx, tx, o, terms); e != nil {
						if invalidReferralEvidence(e) {
							report.InvalidReferralRewards++
						} else {
							return e
						}
					}
				}
			}
			if err != nil {
				if invalidReferralEvidence(err) {
					report.InvalidReferralTerms++
				} else {
					return err
				}
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	for cursor := int64(0); ; {
		ids, err := sharingEvidenceIDsPage(ctx, tx, `SELECT r.order_id FROM referral_rewards r WHERE r.order_id>$1 AND EXISTS(SELECT 1 FROM referral_reward_invariants v WHERE v.reward_id=r.id AND v.valid IS TRUE) ORDER BY r.order_id LIMIT 100`, cursor)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err = s.readReferralRewardTx(ctx, tx, id); err != nil {
				if invalidReferralEvidence(err) {
					report.InvalidReferralRewards++
				} else {
					return err
				}
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	for cursor := int64(0); ; {
		ids, err := sharingEvidenceIDsPage(ctx, tx, `SELECT c.refund_id FROM referral_refund_clawbacks c WHERE c.refund_id>$1 AND EXISTS(SELECT 1 FROM referral_clawback_invariants v WHERE v.refund_id=c.refund_id AND v.valid IS TRUE) ORDER BY c.refund_id LIMIT 100`, cursor)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err = s.scanReferralClawback(ctx, tx, tx.QueryRow(ctx, "SELECT "+referralClawbackColumns+" FROM referral_refund_clawbacks WHERE refund_id=$1", id)); err != nil {
				if invalidReferralEvidence(err) {
					report.InvalidReferralClawbacks++
				} else {
					return err
				}
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	if err = s.checkReferralProfitOrphans(ctx, tx, report); err != nil {
		return err
	}
	return s.checkReferralDailyOrphans(ctx, tx, report)
}
