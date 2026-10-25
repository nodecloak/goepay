package businessnotice

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantnotice"
)

func readRegistrationSource(ctx context.Context, tx pgx.Tx, id int64) (auditSource, error) {
	var a auditSource
	err := tx.QueryRow(ctx, `SELECT a.id,a.resource_id,a.actor_id,a.actor_role,a.actor_name,a.operation,a.changes,a.created_at,a.business_notice_scene,a.business_notice_transaction::text,e.email FROM backoffice_audit a JOIN enrollment_emails e ON e.merchant_id=a.actor_id WHERE a.id=$1 AND a.resource='enrollment' AND a.operation='register' AND a.actor_role='public' AND a.resource_id=a.actor_id AND a.business_notice_scene='regaudit' AND a.business_notice_transaction IS NOT NULL AND a.changes->>'pay'='2'`, id).Scan(&a.ID, &a.ResourceID, &a.MerchantID, &a.Role, &a.Name, &a.Operation, &a.Changes, &a.At, &a.Scene, &a.Transaction, &a.Account)
	a.At = a.At.UTC()
	return a, err
}

// The original audit is minted after the enrollment proof was consumed in the
// same registration transaction. Mutable profile email is never an identity.
func (s *Store) CaptureRegistration(ctx context.Context, tx pgx.Tx, id int64) error {
	if !s.available() || tx == nil || id <= 0 {
		return ErrInvalid
	}
	a, err := readRegistrationSource(ctx, tx, id)
	if err != nil {
		return err
	}
	var current string
	var active bool
	if err = tx.QueryRow(ctx, `SELECT pg_current_xact_id()::text,EXISTS(SELECT 1 FROM merchants m JOIN console_users u ON u.subject_id=m.id AND u.role='merchant' AND u.enabled WHERE m.id=$1 AND m.enabled AND m.pay=2)`, a.MerchantID).Scan(&current, &active); err != nil {
		return err
	}
	if current != a.Transaction || !active {
		return ErrConflict
	}
	var fields struct {
		Pay string `json:"pay"`
	}
	if json.Unmarshal(a.Changes, &fields) != nil || fields.Pay != "2" {
		return ErrInvalid
	}
	return s.appendEvent(ctx, tx, eventRecord{MerchantID: a.MerchantID, AuditID: a.ID, Scene: "regaudit", Transaction: a.Transaction}, eventData{Audit: &a, Input: merchantnotice.Input{Params: map[string]string{"uid": fmt.Sprint(a.MerchantID), "account": a.Account}}})
}
