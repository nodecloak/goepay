package payment

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func (s *Store) checkMembershipReferralLedger(ctx context.Context, tx pgx.Tx, report *LedgerCheck) error {
	var invalidTerms, invalidRewards, invalidAccounts, invalidJournals int64
	e := tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM membership_referral_sources s LEFT JOIN membership_purchases p ON p.id=s.purchase_id WHERE p.id IS NULL OR p.source_version<>1)
 +(SELECT count(*) FROM membership_referral_legacy_purchases l LEFT JOIN membership_purchases p ON p.id=l.purchase_id WHERE p.id IS NULL OR p.source_version<>0 OR l.row_snapshot IS DISTINCT FROM membership_referral_legacy_snapshot(p) OR l.row_hash IS DISTINCT FROM sha256(convert_to(membership_referral_legacy_snapshot(p)::text,'UTF8')))
 +(SELECT count(*) FROM membership_referral_policy_heads h LEFT JOIN membership_referral_policies p ON p.id=h.policy_id WHERE p.id IS NULL OR (p.scope_kind,p.scope_id) IS DISTINCT FROM (h.scope_kind,h.scope_id) OR EXISTS(SELECT 1 FROM membership_referral_policies n WHERE (n.scope_kind,n.scope_id)=(h.scope_kind,h.scope_id) AND n.revision>p.revision))
 +(SELECT count(*) FROM (SELECT scope_kind,scope_id FROM membership_referral_policies GROUP BY scope_kind,scope_id HAVING min(revision)<>1 OR count(*)<>max(revision)) broken),
 (SELECT count(*) FROM membership_referral_rewards r LEFT JOIN membership_referral_sources s ON s.purchase_id=r.purchase_id WHERE s.purchase_id IS NULL),
 (SELECT count(*) FROM accounts a WHERE a.kind='membership_income' AND NOT EXISTS(SELECT 1 FROM membership_referral_sources s JOIN membership_purchases p ON p.id=s.purchase_id AND p.source_version=1 WHERE s.income_account_id=a.id AND (a.owner_id,a.kind,a.scope_id)=(0::bigint,'membership_income',p.id))),
 (SELECT count(*) FROM journals j WHERE j.event_key LIKE 'membership-referral:%:award' AND NOT EXISTS(SELECT 1 FROM membership_referral_rewards r WHERE r.journal_id=j.id AND j.event_key='membership-referral:'||r.purchase_id::text||':award'))
 +(SELECT count(*) FROM journals j WHERE j.event_key ~ '^membership:[0-9]+$' AND NOT EXISTS(SELECT 1 FROM membership_referral_sources s WHERE s.purchase_journal_id=j.id AND j.event_key='membership:'||s.purchase_id::text) AND NOT EXISTS(SELECT 1 FROM membership_referral_legacy_purchases l JOIN membership_purchases p ON p.id=l.purchase_id AND p.source_version=0 WHERE j.event_key='membership:'||p.id::text AND l.row_snapshot=membership_referral_legacy_snapshot(p)))`).Scan(&invalidTerms, &invalidRewards, &invalidAccounts, &invalidJournals)
	if e != nil {
		return e
	}
	report.InvalidReferralTerms += invalidTerms
	report.InvalidReferralRewards += invalidRewards
	report.InvalidAccounts += invalidAccounts
	report.InvalidJournals += invalidJournals
	for cursor := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, tx, "SELECT id FROM membership_purchases WHERE id>$1 ORDER BY id LIMIT 100", cursor)
		if e != nil {
			return e
		}
		for _, id := range ids {
			p, e := scanMembership(tx.QueryRow(ctx, "SELECT "+membershipColumns+" FROM membership_purchases WHERE id=$1", id))
			if e != nil {
				return e
			}
			if e = s.verifyMembershipPurchase(ctx, tx, p); e != nil {
				if !invalidSharingEvidence(e) {
					return e
				}
				report.InvalidReferralTerms++
				var reward bool
				if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM membership_referral_rewards WHERE purchase_id=$1)", id).Scan(&reward); e != nil {
					return e
				}
				if reward {
					report.InvalidReferralRewards++
				}
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	for cursor := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, tx, "SELECT id FROM membership_referral_policies WHERE id>$1 ORDER BY id LIMIT 100", cursor)
		if e != nil {
			return e
		}
		for _, id := range ids {
			if _, e = s.membershipReferralPolicyByID(ctx, tx, id); e != nil {
				if !invalidSharingEvidence(e) {
					return e
				}
				report.InvalidReferralTerms++
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	// Inspect every audit subject, including facts whose ordinary parent scan
	// could not run because an attacker removed the source row.
	for cursor := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, tx, "SELECT id FROM membership_referral_events WHERE id>$1 ORDER BY id LIMIT 100", cursor)
		if e != nil {
			return e
		}
		for _, id := range ids {
			var purchase int64
			if e = tx.QueryRow(ctx, "SELECT purchase_id FROM membership_referral_events WHERE id=$1", id).Scan(&purchase); e != nil {
				return e
			}
			p, e := scanMembership(tx.QueryRow(ctx, "SELECT "+membershipColumns+" FROM membership_purchases WHERE id=$1", purchase))
			if e == nil {
				_, e = s.readMembershipReferralSource(ctx, tx, p)
			}
			if e != nil {
				if !invalidSharingEvidence(e) {
					return e
				}
				report.InvalidReferralTerms++
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	return nil
}
