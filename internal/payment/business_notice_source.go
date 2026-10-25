package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
	"goepay/internal/businessnotice"
)

// Optional messaging never rolls back the original domain submission.
func (s *Store) RecordAdminBusinessDomain(ctx context.Context, tx pgx.Tx, auditID int64) {
	if s.business == nil || tx == nil {
		return
	}
	child, err := tx.Begin(ctx)
	if err != nil {
		return
	}
	defer child.Rollback(ctx)
	if err = s.business.CaptureDomain(ctx, child, auditID); err != nil {
		return
	}
	_ = child.Commit(ctx)
}
func (s *Store) AdminBusinessNotices() *businessnotice.Store { return s.business }

// Real older migration fixtures remain readable without manufacturing source
// metadata. Startup migrations provide all three tables in the actual runtime.
func (s *Store) AdminBusinessSourceReady(ctx context.Context, tx pgx.Tx) bool {
	if s == nil || s.business == nil || tx == nil {
		return false
	}
	var ready bool
	err := tx.QueryRow(ctx, `SELECT to_regclass('admin_business_notice_events') IS NOT NULL`).Scan(&ready)
	return err == nil && ready
}
func (s *Store) RecordAdminBusinessRegistration(ctx context.Context, tx pgx.Tx, id int64) {
	if s.business == nil || tx == nil || id <= 0 {
		return
	}
	child, err := tx.Begin(ctx)
	if err != nil {
		return
	}
	defer child.Rollback(ctx)
	if err = s.business.CaptureRegistration(ctx, child, id); err != nil {
		return
	}
	_ = child.Commit(ctx)
}
func (s *Store) recordAdminBusinessRequest(ctx context.Context, tx pgx.Tx, id int64) {
	if s.business == nil || tx == nil || id <= 0 {
		return
	}
	child, err := tx.Begin(ctx)
	if err != nil {
		return
	}
	defer child.Rollback(ctx)
	if err = s.business.CaptureRequest(ctx, child, id); err != nil {
		return
	}
	_ = child.Commit(ctx)
}
