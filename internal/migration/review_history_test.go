package migration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestImportReplayUsesSingleConnection(t *testing.T) {
	p := migrationPool(t)
	master := bytes.Repeat([]byte{3}, 32)
	if _, e := Apply(context.Background(), p, master, []byte(snapshot)); e != nil {
		t.Fatal(e)
	}
	config := p.Config()
	config.MaxConns = 1
	single, e := pgxpool.NewWithConfig(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	defer single.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	report, e := Apply(ctx, single, master, []byte(snapshot))
	if e != nil || !report.AlreadyImported {
		t.Fatalf("valid replay required a second pool connection: report=%+v err=%v", report, e)
	}
}

func TestHistoryEmptyPageVerifiesIndependentManifest(t *testing.T) {
	p := migrationPool(t)
	ctx := context.Background()
	master := bytes.Repeat([]byte{3}, 32)
	if _, e := Apply(ctx, p, master, []byte(snapshot)); e != nil {
		t.Fatal(e)
	}
	if rows, e := ListRefundHistory(ctx, p, master, 9002, 0, 20); e != nil || len(rows) != 0 {
		t.Fatal("valid import prevented an owner-scoped empty history read", rows, e)
	}
	if _, e := p.Exec(ctx, `ALTER TABLE legacy_refund_history DISABLE TRIGGER USER; DELETE FROM legacy_refund_history; ALTER TABLE legacy_refund_history ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if rows, e := ListRefundHistory(ctx, p, master, 9001, 0, 20); e == nil {
		t.Fatalf("missing last owner child appeared to be authentic empty history: %+v", rows)
	}
	if _, e := ListRefundHistory(ctx, p, master, 9002, 0, 20); e == nil {
		t.Fatal("damaged global import manifest did not block an unrelated empty owner page")
	}
}

func TestImportManifestCompletionChecksAreBoundedPerExport(t *testing.T) {
	p := migrationPool(t)
	ctx := context.Background()
	// Instrument the actual deferred completion function, without replacing its
	// checks, to measure repeated full-export scans deterministically.
	if _, e := p.Exec(ctx, `CREATE TABLE review_manifest_checks(id bigserial)`); e != nil {
		t.Fatal(e)
	}
	var definition string
	if e := p.QueryRow(ctx, `SELECT pg_get_functiondef('legacy_refund_manifest_complete()'::regprocedure)`).Scan(&definition); e != nil {
		t.Fatal(e)
	}
	definition = strings.Replace(definition, "BEGIN", "BEGIN\n INSERT INTO review_manifest_checks DEFAULT VALUES;", 1)
	if _, e := p.Exec(ctx, definition); e != nil {
		t.Fatal(e)
	}
	var plan Plan
	if e := json.Unmarshal([]byte(snapshot), &plan); e != nil {
		t.Fatal(e)
	}
	for i := 1; i < 16; i++ {
		r := plan.Refunds[0]
		r.RefundNo, r.Status = fmt.Sprintf("failed-%d", i), "failed"
		plan.Refunds = append(plan.Refunds, r)
	}
	raw, e := json.Marshal(plan)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Apply(ctx, p, bytes.Repeat([]byte{3}, 32), raw); e != nil {
		t.Fatal(e)
	}
	var checks int
	if e = p.QueryRow(ctx, `SELECT count(*) FROM review_manifest_checks`).Scan(&checks); e != nil {
		t.Fatal(e)
	}
	if checks < 1 || checks > 2 {
		t.Fatalf("16-child export repeated its completion scan %d times; parent and manifest need at most two checks", checks)
	}
	t.Logf("16-child export completed with %d full-export checks", checks)
}

func TestHistoryCannotAppendAfterParentOrManifestBirth(t *testing.T) {
	for _, birth := range []string{"committed_parent", "same_tx_parent", "same_tx_manifest"} {
		t.Run(birth, func(t *testing.T) {
			p := migrationPool(t)
			ctx := context.Background()
			if _, e := Apply(ctx, p, bytes.Repeat([]byte{3}, 32), []byte(snapshot)); e != nil {
				t.Fatal(e)
			}
			tx, e := p.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			export := "sanitized-fixture-1"
			if birth != "committed_parent" {
				export = "new-export"
				statement := `INSERT INTO legacy_import_runs(export_id,source,sha256,report) VALUES($1,'epay_pro',repeat('0',64),'{}')`
				if birth == "same_tx_manifest" {
					statement = `INSERT INTO legacy_refund_manifests(export_id,source,source_hash,refund_count,unknown_count,proof_hash) VALUES($1,'epay_pro',repeat('0',64),1,0,decode(repeat('00',32),'hex'))`
				}
				if _, e = tx.Exec(ctx, statement, export); e != nil {
					t.Fatal(e)
				}
			}
			_, e = tx.Exec(ctx, `INSERT INTO legacy_refund_history(export_id,merchant_id,order_id,trade_no,refund_no,amount,legacy_status,source_hash,proof_hash) SELECT $1,merchant_id,id,trade_no,'late-refund',1,'failed',repeat('0',64),decode(repeat('00',32),'hex') FROM orders WHERE trade_no='legacy-t1'`, export)
			if e == nil {
				t.Fatal("archive accepted a child after immutable export completion records were born")
			}
		})
	}
}

func TestHistoryCannotCommitChildAcrossConcurrentExportSeal(t *testing.T) {
	p := migrationPool(t)
	ctx := context.Background()
	if _, e := Apply(ctx, p, bytes.Repeat([]byte{3}, 32), []byte(snapshot)); e != nil {
		t.Fatal(e)
	}
	late, e := p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer late.Rollback(ctx)
	if _, e = late.Exec(ctx, `INSERT INTO legacy_refund_history(export_id,merchant_id,order_id,trade_no,refund_no,amount,legacy_status,source_hash,proof_hash) SELECT 'concurrent-export',merchant_id,id,trade_no,'concurrent-refund',1,'failed',repeat('0',64),decode(repeat('00',32),'hex') FROM orders WHERE trade_no='legacy-t1'`); e != nil {
		t.Fatal(e)
	}
	seal, e := p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer seal.Rollback(ctx)
	// This transaction cannot see the other transaction's uncommitted child.
	if _, e = seal.Exec(ctx, `INSERT INTO legacy_refund_manifests(export_id,source,source_hash,refund_count,unknown_count,proof_hash) VALUES('concurrent-export','epay_pro',repeat('0',64),0,0,decode(repeat('00',32),'hex')); INSERT INTO legacy_import_runs(export_id,source,sha256,report) VALUES('concurrent-export','epay_pro',repeat('0',64),'{}')`); e != nil {
		t.Fatal(e)
	}
	if e = seal.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = late.Commit(ctx); e == nil {
		t.Fatal("child committed into an export sealed concurrently by another transaction")
	}
}

func TestHistoryParentInsideReleasedSavepointCommits(t *testing.T) {
	p := migrationPool(t)
	ctx := context.Background()
	if _, e := Apply(ctx, p, bytes.Repeat([]byte{3}, 32), []byte(snapshot)); e != nil {
		t.Fatal(e)
	}
	tx, e := p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `INSERT INTO legacy_refund_history(export_id,merchant_id,order_id,trade_no,refund_no,amount,legacy_status,source_hash,proof_hash) SELECT 'savepoint-export',merchant_id,id,trade_no,'savepoint-refund',1,'failed',repeat('0',64),decode(repeat('00',32),'hex') FROM orders WHERE trade_no='legacy-t1'; INSERT INTO legacy_refund_manifests(export_id,source,source_hash,refund_count,unknown_count,proof_hash) VALUES('savepoint-export','epay_pro',repeat('0',64),1,0,decode(repeat('00',32),'hex')); SAVEPOINT parent_birth; INSERT INTO legacy_import_runs(export_id,source,sha256,report) VALUES('savepoint-export','epay_pro',repeat('0',64),'{}'); RELEASE SAVEPOINT parent_birth`); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatalf("valid same-transaction history failed after parent savepoint release: %v", e)
	}
}

func TestHistoryRejectsForgedBirthTransaction(t *testing.T) {
	p := migrationPool(t)
	ctx := context.Background()
	// Reusing a prior import's transaction identity must fail at parent birth,
	// before deferred manifest checks; otherwise a concurrent sealer could name
	// another writer's xid and let that writer append to the completed export.
	tx, e := p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var wrongXID string
	if e = p.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&wrongXID); e != nil {
		t.Fatal(e)
	}
	_, e = tx.Exec(ctx, `INSERT INTO legacy_import_runs(export_id,source,sha256,report,refund_history_birth_xid) VALUES('forged-birth-export','epay_pro',repeat('0',64),'{}',$1::xid8)`, wrongXID)
	if e == nil {
		t.Fatal("parent accepted a birth xid belonging to another transaction")
	}
}

func TestHistoryCompletionRetainsSourceAndOrderGuards(t *testing.T) {
	for _, damage := range []string{"source_hash", "order_identity"} {
		t.Run(damage, func(t *testing.T) {
			p := migrationPool(t)
			ctx := context.Background()
			if _, e := Apply(ctx, p, bytes.Repeat([]byte{3}, 32), []byte(snapshot)); e != nil {
				t.Fatal(e)
			}
			tx, e := p.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			trade, digest := "legacy-t1", strings.Repeat("0", 64)
			if damage == "source_hash" {
				digest = strings.Repeat("1", 64)
			} else {
				trade = "wrong-order-identity"
			}
			if _, e = tx.Exec(ctx, `INSERT INTO legacy_refund_history(export_id,merchant_id,order_id,trade_no,refund_no,amount,legacy_status,source_hash,proof_hash) SELECT 'invalid-source-export',merchant_id,id,$1,'invalid-source-refund',1,'failed',$2,decode(repeat('00',32),'hex') FROM orders WHERE trade_no='legacy-t1'`, trade, digest); e != nil {
				t.Fatal(e)
			}
			if _, e = tx.Exec(ctx, `INSERT INTO legacy_refund_manifests(export_id,source,source_hash,refund_count,unknown_count,proof_hash) VALUES('invalid-source-export','epay_pro',repeat('0',64),1,0,decode(repeat('00',32),'hex')); INSERT INTO legacy_import_runs(export_id,source,sha256,report) VALUES('invalid-source-export','epay_pro',repeat('0',64),'{}')`); e != nil {
				t.Fatal(e)
			}
			if e = tx.Commit(ctx); e == nil {
				t.Fatal("manifest counts allowed history with a mismatched original source/order")
			}
		})
	}
}
