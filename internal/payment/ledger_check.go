package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

// LedgerCheck reports structural invariants from one consistent, read-only
// snapshot. It does not repair balances or claim reconciliation with a bank.
type LedgerCheck struct {
	InvalidSettlementBatches      int64 `json:"invalid_settlement_batches"`
	InvalidScanRecoverySources    int64 `json:"invalid_scan_recovery_sources"`
	UnknownScanCancellations      int64 `json:"unknown_scan_cancellations"`
	InvalidChannelStops           int64 `json:"invalid_channel_stops"`
	InvalidAuthorizationReleases  int64 `json:"invalid_authorization_releases"`
	InvalidOrderIncomeFundings    int64 `json:"invalid_order_income_fundings"`
	InvalidRefundFundings         int64 `json:"invalid_refund_fundings"`
	InvalidJournals               int64 `json:"invalid_journals"`
	InvalidAccounts               int64 `json:"invalid_accounts"`
	UnknownRefunds                int64 `json:"unknown_refunds"`
	UnknownTransfers              int64 `json:"unknown_transfers"`
	UnsettledAuthorizations       int64 `json:"unsettled_authorizations"`
	PendingBankReversals          int64 `json:"pending_bank_reversals"`
	InvalidSettlementFundings     int64 `json:"invalid_settlement_fundings"`
	InvalidChannelSharingFundings int64 `json:"invalid_channel_sharing_fundings"`
	InvalidChannelSharingReturns  int64 `json:"invalid_channel_sharing_returns"`
	InvalidChannelSharingFinishes int64 `json:"invalid_channel_sharing_finishes"`
	InvalidReferralTerms          int64 `json:"invalid_referral_terms"`
	InvalidReferralRewards        int64 `json:"invalid_referral_rewards"`
	InvalidReferralClawbacks      int64 `json:"invalid_referral_clawbacks"`
	InvalidProviderRefundResults  int64 `json:"invalid_provider_refund_results"`
	InvalidSettlementPlans        int64 `json:"invalid_settlement_plans"`
}

func (s *Store) CheckLedger(ctx context.Context) (LedgerCheck, error) {
	var report LedgerCheck
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return report, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `SELECT
	 (SELECT count(*) FROM (SELECT j.id FROM journals j LEFT JOIN ledger_entries e ON e.journal_id=j.id GROUP BY j.id HAVING count(e.id)<2 OR COALESCE(sum(e.amount),0)<>0) invalid),
	 (SELECT count(*) FROM accounts a WHERE a.balance<>(SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.account_id=a.id)),
	 (SELECT count(*) FROM refunds WHERE status='unknown'),
	 (SELECT count(*) FROM transfers WHERE status='unknown'),
	 (SELECT count(*) FROM payment_authorizations WHERE capture_state<>'succeeded' AND release_state<>'succeeded'),
	 (SELECT count(*) FROM bank_transfer_lifecycle WHERE state='reversal_pending'),
	 (SELECT count(*) FROM settlement_fundings f WHERE NOT EXISTS(SELECT 1 FROM settlement_funding_invariants v WHERE v.request_id=f.request_id AND v.valid IS TRUE))+(SELECT count(*) FROM accounts a WHERE a.kind='settlement_reserve' AND NOT EXISTS(SELECT 1 FROM settlement_fundings f WHERE f.account_id=a.id)),
	 (SELECT count(*) FROM channel_sharing_fundings f WHERE NOT EXISTS(SELECT 1 FROM channel_sharing_funding_invariants v WHERE v.order_id=f.order_id AND v.valid IS TRUE))+(SELECT count(*) FROM accounts a WHERE a.kind='channel_sharing_reserve' AND NOT EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.account_id=a.id))+(SELECT count(*) FROM provider_attempt_sharing d JOIN orders o ON o.id=d.order_id WHERE d.declared AND o.status='paid' AND NOT EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=o.id))`).Scan(&report.InvalidJournals, &report.InvalidAccounts, &report.UnknownRefunds, &report.UnknownTransfers, &report.UnsettledAuthorizations, &report.PendingBankReversals, &report.InvalidSettlementFundings, &report.InvalidChannelSharingFundings)
	if err != nil {
		return report, err
	}
	err = tx.QueryRow(ctx, `SELECT count(*) FROM channel_sharing_returns r WHERE NOT EXISTS(SELECT 1 FROM channel_sharing_return_invariants v WHERE v.return_id=r.id AND v.valid IS TRUE)`).Scan(&report.InvalidChannelSharingReturns)
	if err != nil {
		return report, err
	}
	err = tx.QueryRow(ctx, `SELECT count(*) FROM channel_sharing_finishes p WHERE NOT EXISTS(SELECT 1 FROM channel_sharing_finish_invariants v WHERE v.finish_id=p.id AND v.valid IS TRUE)`).Scan(&report.InvalidChannelSharingFinishes)
	if err != nil {
		return report, err
	}
	for cursor := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, tx, `SELECT p.id FROM channel_sharing_finishes p WHERE p.id>$1 AND EXISTS(SELECT 1 FROM channel_sharing_finish_invariants v WHERE v.finish_id=p.id AND v.valid IS TRUE) ORDER BY p.id LIMIT 100`, cursor)
		if e != nil {
			return report, e
		}
		for _, id := range ids {
			if _, e = s.scanSharingFinish(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingFinishColumns+" FROM channel_sharing_finishes p JOIN orders o ON o.id=p.order_id WHERE p.id=$1", id)); e != nil {
				if invalidSharingEvidence(e) {
					report.InvalidChannelSharingFinishes++
				} else {
					return report, e
				}
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	for cursor := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, tx, `SELECT f.order_id FROM channel_sharing_fundings f WHERE f.order_id>$1 AND EXISTS(SELECT 1 FROM channel_sharing_funding_invariants v WHERE v.order_id=f.order_id AND v.valid IS TRUE) ORDER BY f.order_id LIMIT 100`, cursor)
		if e != nil {
			return report, e
		}
		for _, id := range ids {
			if e = s.verifySharingFundingEvidence(ctx, tx, id); e != nil {
				if invalidSharingEvidence(e) {
					report.InvalidChannelSharingFundings++
				} else {
					return report, e
				}
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	for cursor := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, tx, `SELECT r.id FROM channel_sharing_returns r WHERE r.id>$1 AND EXISTS(SELECT 1 FROM channel_sharing_return_invariants v WHERE v.return_id=r.id AND v.valid IS TRUE) ORDER BY r.id LIMIT 100`, cursor)
		if e != nil {
			return report, e
		}
		for _, id := range ids {
			if _, e = s.scanSharingReturn(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingReturnColumns+" FROM channel_sharing_returns WHERE id=$1", id)); e != nil {
				if invalidSharingEvidence(e) {
					report.InvalidChannelSharingReturns++
				} else {
					return report, e
				}
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	if err = s.checkReferralLedger(ctx, tx, &report); err != nil {
		return report, err
	}
	if err = s.checkProviderRefundResults(ctx, tx, &report); err != nil {
		return report, err
	}
	if err = s.checkSettlementPlans(ctx, tx, &report); err != nil {
		return report, err
	}
	if err = s.checkIncomeRefundFundings(ctx, tx, &report); err != nil {
		return report, err
	}
	if err = s.checkMembershipReferralLedger(ctx, tx, &report); err != nil {
		return report, err
	}
	if err = s.checkAuthorizationReleases(ctx, tx, &report); err != nil {
		return report, err
	}
	if err = s.checkChannelStops(ctx, tx, &report); err != nil {
		return report, err
	}
	if err = s.checkScanRecoveries(ctx, tx, &report); err != nil {
		return report, err
	}
	if err = s.checkSettlementBatches(ctx, tx, &report); err != nil {
		return report, err
	}
	return report, tx.Commit(ctx)
}

func invalidSharingEvidence(e error) bool {
	return errors.Is(e, ErrConflict) || errors.Is(e, ErrState) || errors.Is(e, ErrNotFound) || errors.Is(e, adapter.ErrAuthentication)
}
func (s *Store) verifySharingFundingEvidence(ctx context.Context, tx pgx.Tx, orderID int64) error {
	d, e := scanSharingPayment(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingPaymentColumns+" FROM provider_attempt_sharing WHERE order_id=$1", orderID))
	if e != nil {
		return e
	}
	if e = s.verifySharingPayment(d); e != nil {
		return e
	}
	var exists bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM channel_sharing_orders WHERE order_id=$1)", orderID).Scan(&exists); e != nil {
		return e
	}
	if exists {
		if _, e = s.scanSharingOrder(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingOrderColumns+" FROM channel_sharing_orders WHERE order_id=$1", orderID)); e != nil {
			return e
		}
	}
	return s.verifyOrderSharingFinishEvidence(ctx, tx, orderID)
}
