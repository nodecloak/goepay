package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ArchiveSettlementBatch(ctx context.Context, id int64, actor TransferBatchActor) (SettlementBatch, error) {
	return s.settlementBatchAction(ctx, id, actor, "archive")
}
func (s *Store) CompleteSettlementBatch(ctx context.Context, id int64, actor TransferBatchActor) (SettlementBatch, error) {
	return s.settlementBatchAction(ctx, id, actor, "complete")
}
func (s *Store) settlementBatchAction(ctx context.Context, id int64, actor TransferBatchActor, operation string) (SettlementBatch, error) {
	var empty SettlementBatch
	if s == nil || s.pool == nil || ctx == nil || id <= 0 || actor.Role != "admin" || !actor.valid(0) || operation != "archive" && operation != "complete" {
		return empty, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	// Grouping changes no original payment or request state.
	if err = tx.QueryRow(ctx, `SELECT id FROM settlement_batches WHERE id=$1 FOR UPDATE`, id).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, ErrNotFound
		}
		return empty, err
	}
	if operation == "complete" {
		// Existing bank and wallet lifecycle mutations use this same order.
		for _, sql := range []string{
			`SELECT id FROM merchants WHERE id IN(SELECT merchant_id FROM settlement_batch_items WHERE batch_id=$1) ORDER BY id FOR NO KEY UPDATE`,
			`SELECT id FROM ledger_requests WHERE id IN(SELECT request_id FROM settlement_batch_items WHERE batch_id=$1) ORDER BY merchant_id,id FOR UPDATE`,
			`SELECT id FROM transfers WHERE id IN(SELECT transfer_id FROM settlement_payouts WHERE request_id IN(SELECT request_id FROM settlement_batch_items WHERE batch_id=$1)) ORDER BY id FOR UPDATE`,
		} {
			rows, e := tx.Query(ctx, sql, id)
			if e != nil {
				return empty, e
			}
			for rows.Next() {
				var locked int64
				if e = rows.Scan(&locked); e != nil {
					rows.Close()
					return empty, e
				}
			}
			rows.Close()
			if e = rows.Err(); e != nil {
				return empty, e
			}
		}
	}
	current, err := s.readSettlementBatch(ctx, tx, id)
	if err != nil {
		return empty, err
	}
	var already bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settlement_batch_events WHERE batch_id=$1 AND operation=$2)`, id, operation).Scan(&already); err != nil {
		return empty, err
	}
	if already {
		return current, tx.Commit(ctx)
	}
	if operation == "complete" && (current.Status == "archived" || !current.AllPaid) {
		return empty, ErrState
	}
	h, err := s.readSettlementBatchHeader(ctx, tx, id)
	if err != nil {
		return empty, err
	}
	if err = s.appendSettlementBatchEvent(ctx, tx, h, operation, actor); err != nil {
		return empty, err
	}
	current.Status = map[string]string{"archive": "archived", "complete": "completed"}[operation]
	return current, tx.Commit(ctx)
}
