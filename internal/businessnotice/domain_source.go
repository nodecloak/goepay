package businessnotice

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantnotice"
)

func readDomainSource(ctx context.Context, tx pgx.Tx, id int64) (auditSource, error) {
	var a auditSource
	err := tx.QueryRow(ctx, `SELECT id,resource_id,actor_id,actor_role,actor_name,operation,changes,created_at,business_notice_scene,business_notice_transaction::text FROM backoffice_audit WHERE id=$1 AND resource='domains' AND actor_role='merchant' AND operation IN('create','update') AND business_notice_scene='domain' AND business_notice_transaction IS NOT NULL`, id).Scan(&a.ID, &a.ResourceID, &a.MerchantID, &a.Role, &a.Name, &a.Operation, &a.Changes, &a.At, &a.Scene, &a.Transaction)
	a.At = a.At.UTC()
	return a, err
}
func (s *Store) CaptureDomain(ctx context.Context, tx pgx.Tx, id int64) error {
	if !s.available() || tx == nil || id <= 0 {
		return ErrInvalid
	}
	a, err := readDomainSource(ctx, tx, id)
	if err != nil {
		return err
	}
	var current string
	var valid bool
	err = tx.QueryRow(ctx, `SELECT pg_current_xact_id()::text,EXISTS(SELECT 1 FROM backoffice_records r JOIN merchants m ON m.id=r.owner_id WHERE r.id=$1 AND r.resource='domains' AND r.owner_id=$2 AND r.body=$3::jsonb AND r.body->'approved'='false'::jsonb AND r.body->'enabled'='true'::jsonb AND m.enabled)`, a.ResourceID, a.MerchantID, a.Changes).Scan(&current, &valid)
	if err != nil {
		return err
	}
	if !valid || current != a.Transaction {
		return ErrConflict
	}
	var fields struct {
		Domain string `json:"domain"`
	}
	if json.Unmarshal(a.Changes, &fields) != nil || fields.Domain == "" {
		return ErrInvalid
	}
	return s.appendEvent(ctx, tx, eventRecord{MerchantID: a.MerchantID, AuditID: a.ID, Scene: "domain", Transaction: a.Transaction}, eventData{Audit: &a, Input: merchantnotice.Input{Params: map[string]string{"uid": fmt.Sprint(a.MerchantID), "domain": fields.Domain}}})
}
