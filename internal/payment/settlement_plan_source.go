package payment

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Existing reservations remain reviewable after authority expiry or revocation,
// but their original signed source must remain authentic. Neither a manual
// approval nor an existing payout can give a damaged source a new seal.
func (s *Store) verifyScheduledRequestSource(ctx context.Context, q fundsReader, id int64) error {
	var itemID *int64
	var kind string
	var merchant int64
	var systemEvent bool
	err := q.QueryRow(ctx, `SELECT r.settlement_plan_item_id,r.kind,r.merchant_id,EXISTS(SELECT 1 FROM ledger_request_events e WHERE e.request_id=r.id AND e.actor='system:settlement-scheduler' AND e.status='pending') FROM ledger_requests r WHERE r.id=$1`, id).Scan(&itemID, &kind, &merchant, &systemEvent)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if itemID == nil && !systemEvent {
		return nil
	}
	if itemID == nil || *itemID <= 0 || kind != "settlements" || !systemEvent {
		return ErrConflict
	}
	item, err := s.readSettlementPlanItem(ctx, q, *itemID)
	if err != nil {
		if invalidReferralEvidence(err) {
			return ErrConflict
		}
		return err
	}
	if item.View.RequestID != id || item.View.MerchantID != merchant || item.View.State != "created" {
		return ErrConflict
	}
	return nil
}
