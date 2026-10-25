package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strings"
	"unicode/utf8"
)

type SettlementBatchList struct {
	Items        []SettlementBatch `json:"items"`
	Total        int64             `json:"total"`
	Limit        int               `json:"limit"`
	NextBeforeID int64             `json:"next_before_id,string"`
}
type SettlementBatchQuery struct {
	ExternalNo, Batch, Status string
	BeforeID                  int64
	Limit                     int
}

func (s *Store) ListSettlementBatches(ctx context.Context, externalNo string, beforeID int64, limit int) (SettlementBatchList, error) {
	return s.ListSettlementBatchesFiltered(ctx, SettlementBatchQuery{ExternalNo: externalNo, BeforeID: beforeID, Limit: limit})
}
func (s *Store) ListSettlementBatchesFiltered(ctx context.Context, q SettlementBatchQuery) (SettlementBatchList, error) {
	externalNo, beforeID, limit := q.ExternalNo, q.BeforeID, q.Limit
	out := SettlementBatchList{Items: []SettlementBatch{}, Limit: limit}
	if s == nil || s.pool == nil || ctx == nil || beforeID < 0 || limit < 1 || limit > 100 || len(externalNo) > 128 || !utf8.ValidString(externalNo) || strings.TrimSpace(externalNo) != externalNo {
		return out, ErrInvalid
	}
	for _, r := range externalNo {
		if r < 32 || r == 127 {
			return out, ErrInvalid
		}
	}
	if q.Batch != "" {
		if len(q.Batch) < 20 || len(q.Batch) > 27 {
			return out, ErrInvalid
		}
		for _, r := range q.Batch {
			if r < '0' || r > '9' {
				return out, ErrInvalid
			}
		}
	}
	if q.Status != "" && q.Status != "active" && q.Status != "archived" && q.Status != "completed" {
		return out, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	const where = ` FROM settlement_batches b WHERE ($1::text='' OR b.external_no=$1) AND ($2::text='' OR b.batch=$2) AND ($3::text='' OR $3=(SELECT CASE e.operation WHEN 'create' THEN 'active' WHEN 'archive' THEN 'archived' WHEN 'complete' THEN 'completed' END FROM settlement_batch_events e WHERE e.batch_id=b.id ORDER BY e.id DESC LIMIT 1))`
	if err = tx.QueryRow(ctx, `SELECT count(*)`+where, externalNo, q.Batch, q.Status).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT b.id`+where+` AND ($4::bigint=0 OR b.id<$4) ORDER BY b.id DESC LIMIT $5`, externalNo, q.Batch, q.Status, beforeID, limit+1)
	if err != nil {
		return out, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return out, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(ids) > limit {
		ids = ids[:limit]
		out.NextBeforeID = ids[len(ids)-1]
	}
	for _, id := range ids {
		batch, e := s.readSettlementBatch(ctx, tx, id)
		if e != nil {
			return out, e
		}
		batch.Items = []SettlementBatchMember{}
		out.Items = append(out.Items, batch)
	}
	return out, tx.Commit(ctx)
}

func (s *Store) checkSettlementBatches(ctx context.Context, tx pgx.Tx, report *LedgerCheck) error {
	var tables int
	var migrationTable bool
	if err := tx.QueryRow(ctx, `SELECT num_nonnulls(to_regclass('settlement_batches'),to_regclass('settlement_batch_items'),to_regclass('settlement_batch_events')),to_regclass('schema_migrations') IS NOT NULL`).Scan(&tables, &migrationTable); err != nil {
		return err
	}
	if tables != 3 {
		var auditAnchors int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource IN('settlement-batches','settlement-batch-exports')`).Scan(&auditAnchors); err != nil {
			return err
		}
		var migrated bool
		if migrationTable {
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000083_settlement_batches.up.sql')`).Scan(&migrated); err != nil {
				return err
			}
		}
		// Actual pre83 histories contain no batch tables, migration or audit
		// anchors. A partial schema or any durable new anchor is corruption.
		if tables != 0 || migrated || auditAnchors > 0 {
			report.InvalidSettlementBatches++
		}
		return nil
	}
	for cursor := int64(0); ; {
		ids, err := sharingEvidenceIDsPage(ctx, tx, `SELECT id FROM settlement_batches WHERE id>$1 ORDER BY id LIMIT 100`, cursor)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err = s.readSettlementBatch(ctx, tx, id); err != nil {
				if invalidSharingEvidence(err) {
					report.InvalidSettlementBatches++
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
	var orphan int64
	if err := tx.QueryRow(ctx, `SELECT
	 (SELECT count(*) FROM backoffice_audit a WHERE a.resource IN('settlement-batches','settlement-batch-exports') AND NOT EXISTS(SELECT 1 FROM settlement_batches b WHERE b.id=a.resource_id))+
	 (SELECT count(*) FROM settlement_batch_items i WHERE NOT EXISTS(SELECT 1 FROM settlement_batches b WHERE b.id=i.batch_id))+
	 (SELECT count(*) FROM settlement_batch_events e WHERE NOT EXISTS(SELECT 1 FROM settlement_batches b WHERE b.id=e.batch_id))`).Scan(&orphan); err != nil {
		return err
	}
	report.InvalidSettlementBatches += orphan
	return nil
}
