package payment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"unicode/utf8"
)

// ExportSettlementBatch exposes the original encrypted receiver only to a
// verified super administrator. Exporting never approves or submits a payout.
func (s *Store) ExportSettlementBatch(ctx context.Context, id int64, format string, actor TransferBatchActor) (SettlementBatchExport, error) {
	var empty SettlementBatchExport
	if s == nil || s.pool == nil || ctx == nil || id <= 0 || actor.Role != "admin" || actor.Class != "super" || !actor.valid(0) {
		return empty, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	batch, err := s.readSettlementBatch(ctx, tx, id)
	if err != nil {
		return empty, err
	}
	rows := make([]settlementBatchExportRow, 0, batch.Count)
	for _, member := range batch.Items {
		recipient, _, e := s.originalSettlementRecipient(ctx, tx, member.RequestID)
		if e != nil {
			return empty, e
		}
		rows = append(rows, settlementBatchExportRow{RequestID: member.RequestID, Position: member.Position, Type: recipient.Type, Account: recipient.Account, Name: recipient.Name, AppID: recipient.AppID, NetAmount: member.NetAmount})
	}
	// This is the original non-secret transfer_desc setting. Never load unrelated
	// channel configuration or replace a missing original recipient with it.
	remark := "聚合支付平台自动结算"
	settings, err := tx.Query(ctx, `SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='transfer_desc' ORDER BY id LIMIT 2`)
	if err != nil {
		return empty, err
	}
	count := 0
	for settings.Next() {
		count++
		if err = settings.Scan(&remark); err != nil {
			settings.Close()
			return empty, err
		}
	}
	settings.Close()
	if err = settings.Err(); err != nil {
		return empty, err
	}
	if count > 1 || remark == "" || utf8.RuneCountInString(remark) > 32 {
		return empty, ErrInvalid
	}
	file, err := renderSettlementBatchExport(batch, rows, format, remark)
	if err != nil {
		return empty, err
	}
	if format == "summary" {
		format = "common"
	}
	digest := sha256.Sum256(file.Data)
	changes, err := json.Marshal(struct {
		Format string `json:"format"`
		Count  int    `json:"batch_count"`
		SHA256 string `json:"sha256"`
	}{format, batch.Count, hex.EncodeToString(digest[:])})
	if err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'settlement-batch-exports',$3,'export',$4::jsonb)`, actor.ID, actor.Name, id, changes); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return file, nil
}
