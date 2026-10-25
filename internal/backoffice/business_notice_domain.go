package backoffice

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

func (s *Service) auditDomainSubmission(ctx context.Context, tx pgx.Tx, p Principal, id int64, operation string, fields map[string]any) (bool, error) {
	if p.Role != "merchant" || p.SubjectID <= 0 || fields["approved"] != false || fields["enabled"] != true {
		return false, nil
	}
	if s.store == nil || !s.store.AdminBusinessSourceReady(ctx, tx) {
		return false, nil
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return false, err
	}
	var auditID int64
	err = tx.QueryRow(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes,business_notice_scene,business_notice_transaction) VALUES('merchant',$1,$2,'domains',$3,$4,$5,'domain',pg_current_xact_id()) RETURNING id`, p.SubjectID, p.Username, id, operation, raw).Scan(&auditID)
	if err != nil {
		return false, err
	}
	if s.store != nil {
		s.store.RecordAdminBusinessDomain(ctx, tx, auditID)
	}
	return true, nil
}
