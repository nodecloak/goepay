package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func (s *Store) appendSettlementBatchEvent(ctx context.Context, tx pgx.Tx, h settlementBatchHeader, operation string, actor TransferBatchActor) error {
	e := settlementBatchEvent{BatchID: h.View.ID, Operation: operation, Actor: actor}
	err := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('settlement_batch_events','id')),clock_timestamp(),pg_current_xact_id()::text`).Scan(&e.ID, &e.At, &e.Transaction)
	if err != nil {
		return err
	}
	e.At = e.At.UTC()
	e.Proof, err = s.settlementBatchEventProof(h, e)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO settlement_batch_events(id,batch_id,operation,actor_id,actor_name,actor_class,created_at,transaction_id,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8::xid8,$9)`, e.ID, e.BatchID, e.Operation, actor.ID, actor.Name, actor.Class, e.At, e.Transaction, e.Proof); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'settlement-batches',$3,$4,$5::jsonb)`, actor.ID, actor.Name, e.BatchID, operation, settlementBatchAudit(e))
	return err
}
