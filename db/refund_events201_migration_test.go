package db

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/payment"
	"goepay/internal/refundevent201"
)

const migrationRefundMerchant201 int64 = 20101
const migrationRefundOrder201 int64 = 20101
const migrationRefundAvailable201 int64 = 201001
const migrationRefundReserved201 int64 = 201002

func migrationRefundPost201(t *testing.T, tx pgx.Tx, journal int64, key string, from, to, amount int64) {
	t.Helper()
	ctx := t.Context()
	if _, err := tx.Exec(ctx, `INSERT INTO journals(id,event_key,description)VALUES($1,$2,'legacy refund upgrade fixture201')`, journal, key); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_entries(id,journal_id,account_id,amount)VALUES($1::bigint,$2::bigint,$3::bigint,-($5::bigint)),($1::bigint+1::bigint,$2::bigint,$4::bigint,$5::bigint)`, journal*10, journal, from, to, amount); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE accounts SET balance=balance-($2::bigint) WHERE id=$1::bigint`, from, amount); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE accounts SET balance=balance+($2::bigint) WHERE id=$1::bigint`, to, amount); err != nil {
		t.Fatal(err)
	}
}

// These explicit legacy rows are born before result/funding proof versions are
// introduced. They exercise the real old schema and balanced ledger, not a
// current writer pretending to author a historical MAC.
func migrationRefundLegacyBirth201(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT setval('operation_audit_id_seq',201000,false);
INSERT INTO merchants(id,name,api_key)VALUES(20101,'Legacy refund201',decode(repeat('ab',48),'hex'));
INSERT INTO accounts(id,owner_id,kind,balance)VALUES(201001,20101,'available',0),(201002,20101,'reserved',0);
INSERT INTO orders(id,trade_no,merchant_id,out_trade_no,name,type,amount,status,paid_at)VALUES(20101,'1000000000000020101',20101,'legacy-refund-order201','legacy refund source201','alipay',100,'paid',clock_timestamp());
INSERT INTO refunds(id,merchant_id,order_id,refund_no,amount)VALUES(20101,20101,20101,'legacy-succeeded201',10),(20102,20101,20101,'legacy-unknown201',20),(20103,20101,20101,'opaque-result-bytes201',30)`); err != nil {
		t.Fatal(err)
	}
	var clearing int64
	if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE owner_id=0 AND kind='clearing' AND scope_id=0`).Scan(&clearing); err != nil {
		t.Fatal(err)
	}
	migrationRefundPost201(t, tx, 201010, "legacy-refund-paid201", clearing, migrationRefundAvailable201, 100)
	for i, amount := range []int64{10, 20, 30} {
		id := int64(20101 + i)
		migrationRefundPost201(t, tx, int64(201011+i), fmt.Sprintf("refund:%d:reserve", id), migrationRefundAvailable201, migrationRefundReserved201, amount)
	}
	migrationRefundPost201(t, tx, 201014, "refund:20101:resolve", migrationRefundReserved201, clearing, 10)
	if _, err = tx.Exec(ctx, `UPDATE refunds SET status='succeeded'WHERE id=20101;UPDATE refunds SET status='unknown'WHERE id=20102`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal("legacy fixture ledger must satisfy original database guards", err)
	}
}

func migrationRefundOpaqueResult201(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var clearing int64
	if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE owner_id=0 AND kind='clearing' AND scope_id=0`).Scan(&clearing); err != nil {
		t.Fatal(err)
	}
	migrationRefundPost201(t, tx, 201015, "refund:20103:resolve", migrationRefundReserved201, clearing, 30)
	if _, err = tx.Exec(ctx, `UPDATE refunds SET status='succeeded'WHERE id=20103;
INSERT INTO provider_refund_result_evidence(refund_id,order_id,amount,refund_no,prior_refund_state,source,state,proof_hash)VALUES(20103,20101,30,'opaque-result-bytes201','pending','internal','succeeded',decode(repeat('a7',32),'hex'))`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal("opaque proof preservation fixture must satisfy relational result/ledger invariants", err)
	}
}

type migrationRefundTable201 struct {
	name, columns string
	hash          [32]byte
}

func migrationRefundTables201(t *testing.T, p *pgxpool.Pool, includeRegistry bool) []migrationRefundTable201 {
	t.Helper()
	rows, err := p.Query(t.Context(), `SELECT c.relname,string_agg(quote_ident(a.attname),',' ORDER BY a.attnum) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped WHERE n.nspname=current_schema() AND c.relkind='r'AND($1 OR c.relname<>'schema_migrations')GROUP BY c.relname ORDER BY c.relname`, includeRegistry)
	if err != nil {
		t.Fatal(err)
	}
	var tables []migrationRefundTable201
	for rows.Next() {
		var table migrationRefundTable201
		if err = rows.Scan(&table.name, &table.columns); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	for i := range tables {
		tables[i].hash = migrationRefundHash201(t, p, tables[i])
	}
	return tables
}
func migrationRefundHash201(t *testing.T, p *pgxpool.Pool, table migrationRefundTable201) [32]byte {
	t.Helper()
	var raw string
	query := `SELECT coalesce(jsonb_agg(to_jsonb(x)ORDER BY to_jsonb(x)::text),'[]'::jsonb)::text FROM(SELECT ` + table.columns + ` FROM ` + pgx.Identifier{table.name}.Sanitize() + `)x`
	if err := p.QueryRow(t.Context(), query).Scan(&raw); err != nil {
		t.Fatal(table.name, err)
	}
	return sha256.Sum256([]byte(raw))
}

func TestRefundEvent201Migration113PreservesLegacyRowsProofBytesAndNeverBackfillsEvents(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("actual PostgreSQL required")
	}
	ctx := t.Context()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("refund201_upgrade_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err = p.Exec(ctx, `CREATE TABLE schema_migrations(name text PRIMARY KEY,sha256 text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	entries, err := files.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "000113_" {
			continue
		}
		raw, err := files.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = p.Exec(ctx, string(raw)); err != nil {
			t.Fatal(entry.Name(), err)
		}
		if _, err = p.Exec(ctx, `INSERT INTO schema_migrations(name,sha256)VALUES($1,$2)`, entry.Name(), fmt.Sprintf("%x", sha256.Sum256(raw))); err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.HasPrefix(entry.Name(), "000056_"):
			migrationRefundLegacyBirth201(t, p)
		case strings.HasPrefix(entry.Name(), "000059_"):
			migrationRefundOpaqueResult201(t, p)
		case strings.HasPrefix(entry.Name(), "000101_"):
			// The frozen oracle also retains a route-assignment row for merchant1.
			// Match the existing196 upgrade fixture's prerequisite merchant rather
			// than rewriting that original assignment or any authenticated bytes.
			if _, err = p.Exec(ctx, `INSERT INTO merchants(id,name,api_key,signature_algorithm,keytype,enabled,config_revision,profile)VALUES(1,'old168-1',decode(repeat('ab',48),'hex'),'MD5',NULL,true,14,'{"oldproof":1}')`); err != nil {
				t.Fatal("frozen196 route-assignment prerequisite merchant", err)
			}
			loadActualPre102Fixture196(t, p)
		}
	}
	store, err := payment.NewWithKey(p, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	legacyBefore, err := store.Refund(ctx, migrationRefundMerchant201, "legacy-succeeded201")
	if err != nil || legacyBefore.Status != "succeeded" {
		t.Fatal("explicit pre-proof legacy refund no longer queryable before113", legacyBefore, err)
	}
	unknownBefore, err := store.Refund(ctx, migrationRefundMerchant201, "legacy-unknown201")
	if err != nil || unknownBefore.Status != "unknown" {
		t.Fatal("legacy unknown refund no longer queryable before113", unknownBefore, err)
	}
	// No available historical oracle contains a refund-result MAC. The a7 bytes
	// above are intentionally opaque/untrusted: preserve them exactly and prove
	// migration never promotes them to an authenticated result. Genuine old
	// order/quote proof acceptance is independently covered by the frozen196 data.
	if _, err = store.Refund(ctx, migrationRefundMerchant201, "opaque-result-bytes201"); !errors.Is(err, payment.ErrConflict) {
		t.Fatal("opaque fixture was incorrectly treated as an authentic historical refund MAC", err)
	}
	orderInput := payment.CreateOrderInput{MerchantID: 11, OutTradeNo: "pre102-original-priced", Name: "real original96 proof", Type: "wxpay", Amount: 123}
	orderBefore, err := store.CreateOrder(ctx, orderInput)
	if err != nil || orderBefore.ID != 2 {
		t.Fatal("genuine historical order/quote MAC oracle not recognized before113", orderBefore, err)
	}
	var valid bool
	if err = p.QueryRow(ctx, `SELECT valid FROM provider_refund_result_invariants WHERE refund_id=20103`).Scan(&valid); err != nil || !valid {
		t.Fatal("result preservation fixture lacks valid relational ledger/audit evidence", valid, err)
	}
	var oldProof string
	if err = p.QueryRow(ctx, `SELECT encode(proof_hash,'hex')FROM provider_refund_result_evidence WHERE refund_id=20103`).Scan(&oldProof); err != nil {
		t.Fatal(err)
	}
	before := migrationRefundTables201(t, p, false)
	var registryBefore string
	if err = p.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(s)ORDER BY name)::text FROM schema_migrations s`).Scan(&registryBefore); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("upgrade112 through113/114", err)
	}
	for _, table := range before {
		if after := migrationRefundHash201(t, p, table); after != table.hash {
			t.Fatal("refund migration altered old columns, financial rows or original proof bytes", table.name)
		}
	}
	var registryAfter string
	if err = p.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(s)ORDER BY name)::text FROM schema_migrations s WHERE name<'000113_'`).Scan(&registryAfter); err != nil || registryAfter != registryBefore {
		t.Fatal("old migration registry rows/checksums changed", err)
	}
	var legacyCount, marked, effects int
	if err = p.QueryRow(ctx, `SELECT count(*),count(*)FILTER(WHERE event_origin201 IS NOT NULL)FROM refunds`).Scan(&legacyCount, &marked); err != nil || legacyCount != 3 || marked != 0 {
		t.Fatal("upgrade invented provenance for old refunds", legacyCount, marked, err)
	}
	if err = p.QueryRow(ctx, `SELECT(SELECT count(*)FROM merchant_refund_origins201)+(SELECT count(*)FROM merchant_refund_events201)+(SELECT count(*)FROM merchant_event_subscription_revisions201)+(SELECT count(*)FROM merchant_event_subscription_heads201)+(SELECT count(*)FROM merchant_refund_event_closed_notices201)+(SELECT count(*)FROM merchant_refund_event_claims201)+(SELECT count(*)FROM merchant_refund_event_results201)+(SELECT count(*)FROM merchant_refund_event_notice_claims201)+(SELECT count(*)FROM merchant_refund_event_notice_results201)+(SELECT count(*)FROM jobs WHERE kind IN('merchant.refund-event','merchant.refund-event-closed-notice'))`).Scan(&effects); err != nil || effects != 0 {
		t.Fatal("upgrade fabricated origins, outbox, delivery history or closure mail", effects, err)
	}
	legacyAfter, err := store.Refund(ctx, migrationRefundMerchant201, "legacy-succeeded201")
	if err != nil || legacyAfter != legacyBefore {
		t.Fatal("legacy succeeded refund changed or became unavailable", legacyAfter, err)
	}
	unknownAfter, err := store.Refund(ctx, migrationRefundMerchant201, "legacy-unknown201")
	if err != nil || unknownAfter != unknownBefore {
		t.Fatal("legacy unknown refund changed or became unavailable", unknownAfter, err)
	}
	if _, err = store.Refund(ctx, migrationRefundMerchant201, "opaque-result-bytes201"); !errors.Is(err, payment.ErrConflict) {
		t.Fatal("upgrade promoted opaque result bytes into a trusted refund", err)
	}
	var afterProof string
	if err = p.QueryRow(ctx, `SELECT encode(proof_hash,'hex')FROM provider_refund_result_evidence WHERE refund_id=20103`).Scan(&afterProof); err != nil || afterProof != oldProof {
		t.Fatal("old result MAC bytes were rewritten", err)
	}
	orderAfter, err := store.CreateOrder(ctx, orderInput)
	if err != nil || orderAfter.ID != orderBefore.ID || orderAfter.MerchantAmount != orderBefore.MerchantAmount {
		t.Fatal("upgrade invalidated genuine frozen order/quote MAC replay", orderAfter, err)
	}
	migrationRefundOriginGuards201(t, p)
	current := migrationRefundTables201(t, p, true)
	if err = Migrate(ctx, p); err != nil {
		t.Fatal("repeat migration", err)
	}
	if repeated := migrationRefundTables201(t, p, true); !reflect.DeepEqual(current, repeated) {
		t.Fatal("repeat migrate changed old/new rows or migration registry")
	}
	if err = VerifySchema(ctx, p); err != nil {
		t.Fatal(err)
	}
	t.Log("All old columns and opaque refund-result bytes preserved; legacy grandfather refund reads and genuine196 order/quote MAC replay retained. No historical refund-result MAC oracle was available, so this test does not claim authentic old refund-result MAC acceptance.")
}

func migrationRefundOriginGuards201(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()
	if _, err := p.Exec(ctx, `UPDATE refunds SET event_origin201='platform'WHERE id=20101`); err == nil {
		t.Fatal("historical NULL origin could be promoted")
	}
	missing, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = missing.Exec(ctx, `INSERT INTO refunds(id,merchant_id,order_id,refund_no,amount,event_origin201)VALUES(20104,20101,20101,'missing-origin-source201',1,'platform')`); err != nil {
		missing.Rollback(ctx)
		t.Fatal(err)
	}
	_, err = missing.Exec(ctx, `SET CONSTRAINTS refund_event_origin201_required IMMEDIATE`)
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "P0001" || !strings.Contains(pgerr.Message, "authenticated refund event origin required") {
		missing.Rollback(ctx)
		t.Fatal("new non-NULL origin lacks enforced deferred source requirement", err)
	}
	_ = missing.Rollback(ctx)
	// Only a new post-upgrade source is signed here. No old proof is re-signed.
	mac := hmac.New(sha256.New, make([]byte, 32))
	mac.Write([]byte("goepay/transfer-policy-index/v1"))
	newSources := refundevent201.New(p, mac.Sum(nil))
	validTx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer validTx.Rollback(ctx)
	if _, err = validTx.Exec(ctx, `INSERT INTO refunds(id,merchant_id,order_id,refund_no,amount,event_origin201)VALUES(20104,20101,20101,'new-origin-source201',1,'platform')`); err != nil {
		t.Fatal(err)
	}
	origin := refundevent201.Origin{RefundID: 20104, MerchantID: migrationRefundMerchant201, OrderID: migrationRefundOrder201, Amount: 1, ExternalNo: "new-origin-source201", Entry: "platform"}
	if err = newSources.RecordOrigin(ctx, validTx, origin); err != nil {
		t.Fatal(err)
	}
	if got, proof, err := newSources.ReadOrigin(ctx, validTx, origin); err != nil || got != origin || len(proof) != 32 {
		t.Fatal("new source did not carry its actual bound HMAC", got, err)
	}
	if _, err = validTx.Exec(ctx, `SET CONSTRAINTS refund_event_origin201_required IMMEDIATE`); err != nil {
		t.Fatal("bound HMAC source did not satisfy deferred origin guard", err)
	}
	child, err := validTx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = child.Exec(ctx, `UPDATE merchant_refund_origins201 SET body=body WHERE refund_id=20104`); err == nil {
		child.Rollback(ctx)
		t.Fatal("new origin MAC source became mutable")
	}
	_ = child.Rollback(ctx)
	if _, err = validTx.Exec(ctx, `UPDATE refunds SET event_origin201='api'WHERE id=20104`); err == nil {
		t.Fatal("new origin classification became mutable")
	}
	// Roll back the probe; unrelated funding guards are not bypassed to commit
	// an incomplete refund merely to test the independent origin constraints.
	_ = validTx.Rollback(ctx)
	var proofHex string
	if err = p.QueryRow(ctx, `SELECT encode(proof_hash,'hex')FROM provider_refund_result_evidence WHERE refund_id=20103`).Scan(&proofHex); err != nil || proofHex != hex.EncodeToString([]byte(strings.Repeat("\xa7", 32))) {
		t.Fatal("origin guard probes altered historical result bytes", err)
	}
}
