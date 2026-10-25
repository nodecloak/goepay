package payment

import (
	"context"
	"fmt"
)

// CancelUnsentTransferBatch releases only rows still provably unsent under the
// same merchant/transfer/item lock order used by execute. It never queries or
// invents a provider failure, including for bank-card transfers.
func (g ProviderFlow) CancelUnsentTransferBatch(ctx context.Context, mid, id int64, actor TransferBatchActor) (TransferBatch, error) {
	if g.Store == nil || !actor.valid(mid) {
		return TransferBatch{}, ErrInvalid
	}
	batch, e := g.Store.TransferBatch(ctx, mid, id)
	if e != nil {
		return batch, e
	}
	tx, e := g.Store.pool.Begin(ctx)
	if e != nil {
		return batch, e
	}
	defer tx.Rollback(ctx)
	var merchant int64
	if e = tx.QueryRow(ctx, "SELECT id FROM merchants WHERE id=$1 FOR NO KEY UPDATE", mid).Scan(&merchant); e != nil {
		return batch, e
	}
	authorized := false
	for _, item := range batch.Items {
		t, e := g.Store.scanTransfer(tx.QueryRow(ctx, "SELECT "+transferColumns+transferFrom+" WHERE t.id=$1 AND t.merchant_id=$2 FOR UPDATE OF t", item.Transfer.ID, mid))
		if e != nil {
			return batch, e
		}
		var state string
		if e = tx.QueryRow(ctx, "SELECT state FROM transfer_batch_items WHERE batch_id=$1 AND transfer_id=$2 FOR UPDATE", id, t.ID).Scan(&state); e != nil {
			return batch, e
		}
		if state != "unsent" {
			continue
		}
		if t.LedgerStatus != "pending" || t.AttemptState != "processing" || t.ProviderTransferNo != "" {
			return batch, ErrState
		}
		if !authorized {
			if e = requireFundsAction(ctx, tx, "batch-cancel", fmt.Sprint(id)); e != nil {
				return batch, e
			}
			authorized = true
		}
		if t.Type == "bank" {
			var settled, returned bool
			var bankState string
			if e = tx.QueryRow(ctx, "SELECT state,settled,return_observed FROM bank_transfer_lifecycle WHERE transfer_id=$1 FOR UPDATE", t.ID).Scan(&bankState, &settled, &returned); e != nil {
				return batch, e
			}
			if settled || returned || bankState != "processing" {
				return batch, ErrState
			}
		}
		if _, e = tx.Exec(ctx, "UPDATE transfer_batch_items SET state='cancelled',cancelled_at=clock_timestamp() WHERE transfer_id=$1 AND state='unsent'", t.ID); e != nil {
			return batch, e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO transfer_unsent_cancellations(transfer_id,cost_amount) VALUES($1,$2)", t.ID, t.CostAmount); e != nil {
			return batch, e
		}
		if e = batchEventTx(ctx, tx, id, t.ID, "cancel", "cancelled", actor); e != nil {
			return batch, e
		}
		if e = g.Store.resolveTransferTx(ctx, tx, t.ID, "failed"); e != nil {
			return batch, e
		}
		if _, e = tx.Exec(ctx, "UPDATE provider_transfer_attempts SET state='failed',updated_at=statement_timestamp() WHERE transfer_id=$1", t.ID); e != nil {
			return batch, e
		}
		if t.Type == "bank" {
			if _, e = tx.Exec(ctx, "UPDATE bank_transfer_lifecycle SET state='cancelled',review_reason='platform_proven_unsent',updated_at=statement_timestamp() WHERE transfer_id=$1", t.ID); e != nil {
				return batch, e
			}
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return batch, e
	}
	return g.Store.TransferBatch(ctx, mid, id)
}
