package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Messaging is optional. A transport/profile/storage failure must never undo
// the verified provider receipt and balanced payment journal. This publisher
// runs only on the first paid transition, before the payment transaction ends.
func (s *Store) recordMerchantNoticePaid(ctx context.Context, tx pgx.Tx, o Order, r PaidReceipt) {
	if s.notice == nil {
		return
	}
	child, err := tx.Begin(ctx)
	if err != nil {
		return
	}
	origin, _, err := s.readPrintOrigin(ctx, child, o)
	if err == nil {
		err = s.notice.CapturePaid(ctx, child, o.ID, r.ChannelID, r.ProviderTradeNo, origin == "api" || origin == "collection")
	}
	if err != nil {
		_ = child.Rollback(ctx)
		return
	}
	if err = child.Commit(ctx); err != nil {
		_ = child.Rollback(ctx)
	}
}

// Optional notification failure cannot roll back a verified payout or ledger.
// The publisher rejects replays after the original principal transaction.
func (s *Store) recordMerchantNoticeSettlement(ctx context.Context, tx pgx.Tx, transferID int64) {
	if s.notice == nil {
		return
	}
	child, err := tx.Begin(ctx)
	if err != nil {
		return
	}
	defer child.Rollback(ctx)
	var id int64
	if err = child.QueryRow(ctx, `SELECT request_id FROM settlement_payouts WHERE transfer_id=$1`, transferID).Scan(&id); err != nil {
		return
	}
	in, _, err := s.originalSettlementRecipient(ctx, child, id)
	if err != nil {
		return
	}
	if err = s.notice.CaptureSettlement(ctx, child, transferID, in.Account); err != nil {
		return
	}
	_ = child.Commit(ctx)
}

// This bridge is for the authenticated domain-review transaction only; it
// accepts an original audit ID, never a recipient, message body or decision.
func (s *Store) RecordMerchantNoticeDomainAudit(ctx context.Context, tx pgx.Tx, auditID int64) {
	if s.notice == nil || tx == nil {
		return
	}
	child, err := tx.Begin(ctx)
	if err != nil {
		return
	}
	defer child.Rollback(ctx)
	if err = s.notice.CaptureDomainAudit(ctx, child, auditID); err != nil {
		return
	}
	_ = child.Commit(ctx)
}
