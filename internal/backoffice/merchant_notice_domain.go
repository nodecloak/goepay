package backoffice

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// Domain edits and decisions are distinct. Repeating the same explicit
// decision for the same owner/hostname retains normal audit without creating
// another notification source. The caller holds the domain record lock.
func (s *Service) auditDomainReview(ctx context.Context, tx pgx.Tx, p Principal, id int64, fields map[string]any) (bool, error) {
	if p.Role != "admin" || p.SubjectID <= 0 || p.AdminClass != "" && p.AdminClass != "operator" && p.AdminClass != "super" {
		return false, ErrForbidden
	}
	decision := map[string]any{"merchant_id": fields["merchant_id"], "domain": fields["domain"], "approved": fields["approved"]}
	canonical, err := json.Marshal(decision)
	if err != nil {
		return false, err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT jsonb_build_object('merchant_id',changes->'merchant_id','domain',changes->'domain','approved',changes->'approved')=$2::jsonb FROM backoffice_audit WHERE resource='domains' AND resource_id=$1 AND notice_source_transaction IS NOT NULL ORDER BY id DESC LIMIT 1),false)`, id, canonical).Scan(&same)
	if err != nil || same {
		return false, err
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return false, err
	}
	var auditID int64
	err = tx.QueryRow(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes,notice_source_transaction) VALUES($1,$2,$3,'domains',$4,'update',$5,pg_current_xact_id()) RETURNING id`, p.Role, p.SubjectID, p.Username, id, raw).Scan(&auditID)
	if err != nil {
		return false, err
	}
	if s.store != nil {
		s.store.RecordMerchantNoticeDomainAudit(ctx, tx, auditID)
	}
	return true, nil
}
