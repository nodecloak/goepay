package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
)

func seedMigrationAdmin234(ctx context.Context, pool *pgxpool.Pool, password string) (bool, error) {
	refuse := errors.New("migration administrator initialization requires a fresh installation and a strong GOEPAY_SEED_PASSWORD")
	if pool == nil || len(password) < 12 || len(password) > 1024 {
		return false, refuse
	}
	tx, e := pool.Begin(ctx)
	if e != nil {
		return false, refuse
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(74973235)"); e != nil {
		return false, refuse
	}
	if _, e = tx.Exec(ctx, "LOCK TABLE console_users IN SHARE ROW EXCLUSIVE MODE"); e != nil {
		return false, refuse
	}
	var admins int
	var existing bool
	if e = tx.QueryRow(ctx, `SELECT count(*),COALESCE(bool_or(username='admin' AND admin_class='super' AND enabled),false) FROM console_users WHERE role='admin'`).Scan(&admins, &existing); e != nil {
		return false, refuse
	}
	if admins > 0 {
		if existing {
			return false, nil
		}
		return false, refuse
	}
	// Installation may race a public enrollment or an accidental business
	// writer. Refuse a busy target instead of creating its first administrator
	// against an uncommitted view of apparently empty financial tables.
	if _, e = tx.Exec(ctx, "LOCK TABLE merchants,orders,journals,jobs,accounts IN SHARE MODE NOWAIT"); e != nil {
		return false, refuse
	}
	var occupied bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchants) OR EXISTS(SELECT 1 FROM orders) OR EXISTS(SELECT 1 FROM journals) OR EXISTS(SELECT 1 FROM jobs) OR EXISTS(SELECT 1 FROM accounts WHERE owner_id<>0 OR balance<>0) OR EXISTS(SELECT 1 FROM console_users WHERE role='merchant')`).Scan(&occupied); e != nil || occupied {
		return false, refuse
	}
	hash, e := identity.HashPassword(password)
	if e != nil {
		return false, refuse
	}
	if _, e = tx.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('admin','admin',1,'Administrator',$1,'super')`, hash); e != nil {
		return false, refuse
	}
	if e = tx.Commit(ctx); e != nil {
		return false, refuse
	}
	return true, nil
}
