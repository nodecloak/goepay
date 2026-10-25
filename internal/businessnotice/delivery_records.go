package businessnotice

import (
	"context"
	"crypto/hmac"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Store) readClaim(ctx context.Context, tx pgx.Tx, e eventRecord) (claimRecord, string, error) {
	var c claimRecord
	err := tx.QueryRow(ctx, `SELECT event_id,job_id,merchant_id,transport,blocked,recipient_hash,scope_hash,lease_hash,created_at,transaction_id::text,proof_hash FROM admin_business_notice_claims WHERE event_id=$1`, e.ID).Scan(&c.EventID, &c.JobID, &c.MerchantID, &c.Transport, &c.Blocked, &c.RecipientHash, &c.ScopeHash, &c.LeaseHash, &c.At, &c.Transaction, &c.Proof)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, "", ErrNotFound
	}
	if err != nil {
		return c, "", err
	}
	c.EventProof = e.Proof
	c.At = c.At.UTC()
	kind, _ := ModeFor(e.Scene, e.Mode)
	if c.JobID != e.JobID || c.MerchantID != e.MerchantID || c.Transport != kind || len(c.Proof) != 32 || !hmac.Equal(c.Proof, s.claimProof(c)) {
		return c, "", ErrConflict
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='admin-business-notice-deliveries' AND resource_id=$1 AND operation='attempt' AND (actor_role,actor_id,actor_name)=('system',0,'business-notice') AND changes=$2::jsonb`, e.ID, claimAudit(c)).Scan(&valid); err != nil {
		return c, "", err
	}
	if !valid {
		return c, "", ErrConflict
	}
	var r resultRecord
	var at *time.Time
	var count int
	// ACK and audit absence share one MVCC snapshot, including concurrent commits.
	err = tx.QueryRow(ctx, `SELECT COALESCE(r.event_id,0),r.claim_hash,COALESCE(r.state,''),r.created_at,COALESCE(r.transaction_id::text,''),r.proof_hash,(SELECT count(*) FROM backoffice_audit WHERE resource='admin-business-notice-deliveries' AND resource_id=c.event_id AND operation='result') FROM admin_business_notice_claims c LEFT JOIN admin_business_notice_results r ON r.event_id=c.event_id WHERE c.event_id=$1`, e.ID).Scan(&r.EventID, &r.ClaimHash, &r.State, &at, &r.Transaction, &r.Proof, &count)
	if err != nil {
		return c, "", err
	}
	if r.EventID == 0 {
		if count != 0 {
			return c, "", ErrConflict
		}
		return c, "unknown", nil
	}
	if at == nil || count != 1 {
		return c, "", ErrConflict
	}
	r.At = at.UTC()
	if !hmac.Equal(r.ClaimHash, c.Proof) || len(r.Proof) != 32 || !hmac.Equal(r.Proof, s.resultProof(r)) || r.State != "accepted" && r.State != "unknown" && r.State != "not_sent" || c.Blocked && r.State != "not_sent" {
		return c, "", ErrConflict
	}
	if err = tx.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='admin-business-notice-deliveries' AND resource_id=$1 AND operation='result' AND (actor_role,actor_id,actor_name)=('system',0,'business-notice') AND changes=$2::jsonb`, e.ID, resultAudit(r)).Scan(&valid); err != nil {
		return c, "", err
	}
	if !valid {
		return c, "", ErrConflict
	}
	return c, r.State, nil
}
func (s *Store) recordResult(ctx context.Context, c claimRecord, state string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id int64
	if err = tx.QueryRow(ctx, `SELECT event_id FROM admin_business_notice_claims WHERE event_id=$1 FOR UPDATE`, c.EventID).Scan(&id); err != nil {
		return err
	}
	event, _, err := s.readEvent(ctx, tx, c.EventID)
	if err != nil {
		return err
	}
	stored, _, err := s.readClaim(ctx, tx, event)
	if err != nil || !hmac.Equal(stored.Proof, c.Proof) {
		return ErrConflict
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admin_business_notice_results WHERE event_id=$1)`, id).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return tx.Commit(ctx)
	}
	r := resultRecord{EventID: id, ClaimHash: c.Proof, State: state}
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp(),pg_current_xact_id()::text`).Scan(&r.At, &r.Transaction); err != nil {
		return err
	}
	r.At = r.At.UTC()
	r.Proof = s.resultProof(r)
	if _, err = tx.Exec(ctx, `INSERT INTO admin_business_notice_results(event_id,claim_hash,state,created_at,transaction_id,proof_hash) VALUES($1,$2,$3,$4,$5::xid8,$6)`, r.EventID, r.ClaimHash, r.State, r.At, r.Transaction, r.Proof); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('system',0,'business-notice','admin-business-notice-deliveries',$1,'result',$2::jsonb)`, id, resultAudit(r)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
