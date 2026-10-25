package migration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/payment"
	"os"
	"strings"
	"testing"
)

func migrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	root, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	b := make([]byte, 8)
	rand.Read(b)
	schema := "import_" + hex.EncodeToString(b)
	if _, e = root.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	config, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	p, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Close(); root.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); root.Close() })
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	return p
}
func TestImportOpeningBalancesHistoryAndIdempotency(t *testing.T) {
	p := migrationPool(t)
	ctx := context.Background()
	master := bytes.Repeat([]byte{3}, 32)
	r, e := Apply(ctx, p, master, []byte(snapshot))
	if e != nil || !r.Applied {
		t.Fatalf("import %v %v", r, e)
	}
	store, e := payment.NewWithKey(p, master)
	if e != nil {
		t.Fatal(e)
	}
	balance, e := store.Balance(ctx, 9001)
	if e != nil || balance.Available != 1234 || balance.Reserved != 50 {
		t.Fatalf("opening wrong %v %v", balance, e)
	}
	merchant, e := store.Merchant(ctx, 9001)
	if e != nil || merchant.Enabled || len(merchant.APIKey) < 24 {
		t.Fatal("unsafe imported credentials")
	}
	o, e := store.OrderByTradeNo(ctx, "legacy-t1")
	if e != nil || o.Status != "paid" {
		t.Fatal("history missing")
	}
	refunds, e := ListRefundHistory(ctx, p, master, 9001, 0, 20)
	if e != nil || len(refunds) != 1 || refunds[0].Status != "unknown" || refunds[0].RefundNo != "legacy-r1" || refunds[0].Amount != 50 || refunds[0].TradeNo != "legacy-t1" {
		t.Fatal("original unknown obligation history missing", refunds, e)
	}
	var activeRefunds int
	if e = p.QueryRow(ctx, "SELECT count(*) FROM refunds").Scan(&activeRefunds); e != nil || activeRefunds != 0 {
		t.Fatal("unverified historical refund became executable", activeRefunds, e)
	}
	var jobs, journals int
	p.QueryRow(ctx, "SELECT count(*) FROM jobs").Scan(&jobs)
	p.QueryRow(ctx, "SELECT count(*) FROM journals").Scan(&journals)
	if jobs != 0 || journals != 2 {
		t.Fatal("historical orders generated external jobs or were credited", jobs, journals)
	}
	check, e := CheckRefundHistory(ctx, p, master)
	if e != nil || check.InvalidLegacyRefundHistory != 0 || check.UnknownLegacyRefunds != 1 {
		t.Fatal("unknown opening obligation disappeared from reconciliation", check, e)
	}
	r, e = Apply(ctx, p, master, []byte(snapshot))
	if e != nil || !r.AlreadyImported {
		t.Fatal("idempotency failed", e)
	}
	altered := strings.Replace(snapshot, "Imported demo", "Changed demo", 1)
	if _, e := Apply(ctx, p, master, []byte(altered)); e == nil {
		t.Fatal("same export ID changed snapshot accepted")
	}
}
func TestImportRollsBackOnLateFailure(t *testing.T) {
	p := migrationPool(t)
	ctx := context.Background()
	_, e := p.Exec(ctx, `CREATE FUNCTION fail_import_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected import failure'; END $$;CREATE TRIGGER fail_import_test BEFORE INSERT ON legacy_refund_history FOR EACH ROW EXECUTE FUNCTION fail_import_test()`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := Apply(ctx, p, bytes.Repeat([]byte{3}, 32), []byte(snapshot)); e == nil {
		t.Fatal("injected import failure ignored")
	}
	var merchants, journals, runs, history int
	p.QueryRow(ctx, "SELECT count(*) FROM merchants").Scan(&merchants)
	p.QueryRow(ctx, "SELECT count(*) FROM journals").Scan(&journals)
	p.QueryRow(ctx, "SELECT count(*) FROM legacy_import_runs").Scan(&runs)
	p.QueryRow(ctx, "SELECT count(*) FROM legacy_refund_history").Scan(&history)
	if merchants != 0 || journals != 0 || runs != 0 || history != 0 {
		t.Fatal("partial import committed")
	}
}

func TestImportedRefundHistoryOwnerPagingImmutableAndDamagedRestore(t *testing.T) {
	p := migrationPool(t)
	ctx := context.Background()
	master := bytes.Repeat([]byte{3}, 32)
	if _, e := Apply(ctx, p, master, []byte(snapshot)); e != nil {
		t.Fatal(e)
	}
	history, e := ListRefundHistory(ctx, p, master, 9001, 0, 1)
	if e != nil || len(history) != 1 || history[0].VerifiedPaymentSource {
		t.Fatal(history, e)
	}
	h := history[0]
	if next, e := ListRefundHistory(ctx, p, master, 9001, h.ID, 1); e != nil || len(next) != 0 {
		t.Fatal("paging repeated history", next, e)
	}
	if other, e := ListRefundHistory(ctx, p, master, 9002, 0, 100); e != nil || len(other) != 0 {
		t.Fatal("cross merchant history leak", other, e)
	}
	if _, e = ListRefundHistory(ctx, p, bytes.Repeat([]byte{4}, 32), 9001, 0, 100); e == nil {
		t.Fatal("incorrect source key accepted")
	}
	if _, e = p.Exec(ctx, `UPDATE legacy_refund_history SET amount=amount+1 WHERE id=$1`, h.ID); e == nil {
		t.Fatal("immutable history changed")
	}
	if _, e = p.Exec(ctx, `ALTER TABLE legacy_refund_history DISABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `UPDATE legacy_refund_history SET proof_hash=decode(repeat('00',32),'hex') WHERE id=$1`, h.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `ALTER TABLE legacy_refund_history ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = ListRefundHistory(ctx, p, master, 9001, 0, 100); e == nil {
		t.Fatal("damaged history was presented as authentic export history")
	}
	check, e := CheckRefundHistory(ctx, p, master)
	if e != nil || check.InvalidLegacyRefundHistory != 1 || check.UnknownLegacyRefunds != 1 {
		t.Fatal("checker lost corrupt or unknown historical obligation", check, e)
	}
}

func TestImportedRefundManifestDetectsMissingLastHistoryAndIdempotentReplay(t *testing.T) {
	p := migrationPool(t)
	ctx := context.Background()
	master := bytes.Repeat([]byte{3}, 32)
	if _, e := Apply(ctx, p, master, []byte(snapshot)); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Exec(ctx, `ALTER TABLE legacy_refund_history DISABLE TRIGGER USER;DELETE FROM legacy_refund_history;ALTER TABLE legacy_refund_history ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	check, e := CheckRefundHistory(ctx, p, master)
	if e != nil || check.InvalidLegacyRefundHistory == 0 {
		t.Fatal("missing final child disappeared from checker", check, e)
	}
	if _, e = Apply(ctx, p, master, []byte(snapshot)); e == nil {
		t.Fatal("idempotent replay falsely confirmed an incomplete import")
	}
	if _, e = p.Exec(ctx, `INSERT INTO legacy_import_runs(export_id,source,sha256,report,refund_history_version)VALUES('new-forged-legacy','epay_pro',repeat('0',64),'{}',1)`); e == nil {
		t.Fatal("new import bypassed manifest birth guard")
	}
	if _, e = p.Exec(ctx, `ALTER TABLE legacy_import_runs DISABLE TRIGGER ALL;DELETE FROM legacy_import_runs;ALTER TABLE legacy_import_runs ENABLE TRIGGER ALL`); e != nil {
		t.Fatal(e)
	}
	check, e = CheckRefundHistory(ctx, p, master)
	if e != nil || check.InvalidLegacyRefundHistory == 0 {
		t.Fatal("orphan zero-child manifest disappeared from parent-only scan", check, e)
	}
}
