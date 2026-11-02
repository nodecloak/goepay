package migration

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The native business roots and all their foreign-key descendants are checked.
// This also fences new domain tables added after the wizard was introduced.
func wizardTargetTables234(ctx context.Context, tx pgx.Tx) ([]string, error) {
	rows, e := tx.Query(ctx, `WITH RECURSIVE roots(oid) AS (
 SELECT c.oid FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relkind='r' AND c.relname=ANY($1::text[])
 UNION SELECT fk.conrelid FROM pg_constraint fk JOIN roots r ON fk.confrelid=r.oid
 WHERE fk.contype='f' AND r.oid<>to_regclass(current_schema()||'.backoffice_records')
 ) SELECT DISTINCT c.relname FROM roots r JOIN pg_class c ON c.oid=r.oid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() ORDER BY c.relname`, []string{"merchants", "orders", "accounts", "journals", "ledger_entries", "jobs", "refunds", "transfers", "provider_attempts", "provider_transfer_attempts", "settlement_batches", "inbox", "outbox", "legacy_import_runs"})
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			return nil, e
		}
		names = append(names, name)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return names, nil
}

func wizardTargetReady234(ctx context.Context, tx pgx.Tx, lock bool) (bool, error) {
	if lock {
		// SHARE permits the row-share locks used by authenticated readers,
		// but fences orphan merchant accounts/sessions being inserted. All
		// target locks are NOWAIT: another writer causes a safe refusal,
		// rather than a lock-order cycle with ordinary console operations.
		if _, e := tx.Exec(ctx, "LOCK TABLE console_users,console_sessions IN SHARE MODE NOWAIT"); e != nil {
			return false, wizardTargetLockError234(e)
		}
	}
	names, e := wizardTargetTables234(ctx, tx)
	if e != nil {
		return false, e
	}
	if len(names) < 10 {
		return false, ErrWizardUnavailable234
	}
	if lock {
		quoted := make([]string, len(names))
		for i, n := range names {
			quoted[i] = pgx.Identifier{n}.Sanitize()
		}
		if _, e = tx.Exec(ctx, "LOCK TABLE "+strings.Join(quoted, ",")+" IN ACCESS EXCLUSIVE MODE NOWAIT"); e != nil {
			return false, wizardTargetLockError234(e)
		}
	}
	var merchantIdentity bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM console_users WHERE role='merchant') OR EXISTS(SELECT 1 FROM console_sessions WHERE role='merchant')`).Scan(&merchantIdentity); e != nil {
		return false, e
	}
	if merchantIdentity {
		return false, nil
	}
	for _, name := range names {
		query := "SELECT EXISTS(SELECT 1 FROM " + pgx.Identifier{name}.Sanitize() + " LIMIT 1)"
		if name == "accounts" {
			query = "SELECT EXISTS(SELECT 1 FROM accounts WHERE owner_id<>0 OR balance<>0)"
		}
		if name == "backoffice_records" {
			// Global configuration (including the seeded default group) is not
			// merchant business history. Do not traverse its generic FK bridge
			// into unrelated platform configuration tables either.
			query = `SELECT EXISTS(SELECT 1 FROM backoffice_records WHERE owner_id IS NOT NULL OR resource NOT IN ('groups','announcements','settings','channels','blacklist','collection-styles','profitsharing-rules','pay-types','pay-rolls','weixin-apps','onboarding-channels','cron-tasks','settlement-settings','plugin-configs','template-configs','addon-configs','invitation-codes'))`
		}
		var present bool
		if e = tx.QueryRow(ctx, query).Scan(&present); e != nil {
			return false, e
		}
		if present {
			return false, nil
		}
	}
	var legacy bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_records WHERE resource=ANY($1::text[]))`, []string{"settlements", "settles", "settle-pay", "transfers", "orders", "merchants"}).Scan(&legacy)
	return !legacy, e
}

func wizardTargetLockError234(e error) error {
	var pg *pgconn.PgError
	if errors.As(e, &pg) && pg.Code == "55P03" {
		return ErrWizardBusy234
	}
	return e
}

// Read views must remain available while the importer holds business tables.
// This is a non-blocking observation, not an authorization to perform writes.
func wizardReadTarget234(ctx context.Context, tx pgx.Tx) (ready, busy bool, err error) {
	var available bool
	if err = tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(74973232)").Scan(&available); err != nil {
		return false, false, err
	}
	if !available {
		return false, true, nil
	}
	ready, err = wizardTargetReady234(ctx, tx, false)
	return ready, false, err
}
