package db

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"goepay/internal/payment"
)

func TestRisk177Migration102PreservesEveryOldColumnAndOriginalFeeProof(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema := fmt.Sprintf("risk177_upgrade_%d", time.Now().UnixNano())
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	p, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if _, e = p.Exec(ctx, `CREATE TABLE schema_migrations(name text PRIMARY KEY,sha256 text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); e != nil {
		t.Fatal(e)
	}
	entries, e := files.ReadDir("migrations")
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if entry.Name() >= "000102_" {
			continue
		}
		raw, e := files.ReadFile("migrations/" + entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		if _, e = p.Exec(ctx, string(raw)); e != nil {
			t.Fatal(entry.Name(), e)
		}
		if _, e = p.Exec(ctx, `INSERT INTO schema_migrations(name,sha256) VALUES($1,$2)`, entry.Name(), fmt.Sprintf("%x", sha256.Sum256(raw))); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = p.Exec(ctx, `INSERT INTO merchants(id,name,api_key,signature_algorithm,keytype,enabled,config_revision,profile) SELECT i,'old168-'||i,decode(repeat('ab',48),'hex'),'MD5',CASE WHEN i%2=0 THEN 0 ELSE NULL END,i%3<>0,13+i,jsonb_build_object('oldproof',i) FROM generate_series(1,9) i;
 INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount,platform_fee) VALUES('168-old-trade',1,'168-old-out','old original','alipay',123,123,0);
 INSERT INTO provider_route_assignments(merchant_id,out_trade_no,channel_id,method,pay_type,amount,request_hash) VALUES(1,'168-old-out',7,'native','alipay',123,decode(repeat('ab',32),'hex')),(1,'168-old-pending-without-order',8,'native','alipay',123,decode(repeat('cd',32),'hex'));
 INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',1,'old-source','channels',7,'oldproof','{"evidence":"preserve"}')`); e != nil {
		t.Fatal(e)
	}
	// Keep actual old source and proof bytes; current quote admission must
	// never be relaxed to author pre104 historical facts for an upgrade test.
	store, e := payment.NewWithKey(p, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	loadActualPre102Fixture196(t, p)
	in := payment.CreateOrderInput{MerchantID: 11, OutTradeNo: "pre102-original-priced", Name: "real original96 proof", Type: "wxpay", Amount: 123}
	born, e := store.Order(ctx, 11, in.OutTradeNo)
	if e != nil {
		t.Fatal(e)
	}
	oldColumns := map[string][]string{}
	snapshot := func() map[string]string {
		t.Helper()
		rows, e := p.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY tablename")
		if e != nil {
			t.Fatal(e)
		}
		names := []string{}
		for rows.Next() {
			var name string
			if e = rows.Scan(&name); e != nil {
				t.Fatal(e)
			}
			names = append(names, name)
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		out := map[string]string{}
		for _, name := range names {
			columns, known := oldColumns[name]
			if !known {
				cr, e := p.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 ORDER BY ordinal_position`, name)
				if e != nil {
					t.Fatal(e)
				}
				for cr.Next() {
					var col string
					if e = cr.Scan(&col); e != nil {
						t.Fatal(e)
					}
					columns = append(columns, pgx.Identifier{col}.Sanitize())
				}
				cr.Close()
				if e = cr.Err(); e != nil {
					t.Fatal(e)
				}
				oldColumns[name] = columns
			}
			var digest string
			where := ""
			if name == "schema_migrations" {
				where = " WHERE name<'000102_'"
			}
			if e = p.QueryRow(ctx, `SELECT md5(COALESCE(string_agg(row::text,E'\n' ORDER BY row::text),'')) FROM (SELECT to_jsonb(r) AS row FROM (SELECT `+strings.Join(columns, ",")+` FROM `+pgx.Identifier{name}.Sanitize()+where+`) r) x`).Scan(&digest); e != nil {
				t.Fatal(e)
			}
			out[name] = digest
		}
		return out
	}
	before := snapshot()
	if e = Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	after := snapshot()
	for name, digest := range before {
		if after[name] != digest {
			t.Fatal("upgrade rewrote old row/column/proof", name)
		}
	}
	replay, e := store.CreateOrder(ctx, in)
	if e != nil || replay.ID != born.ID || replay.MerchantAmount != born.MerchantAmount {
		t.Fatal("pre102 genuine96 quote/terms lost old MAC compatibility", e)
	}
	var registered int
	if e = p.QueryRow(ctx, `SELECT count(*) FROM schema_migrations WHERE name IN ('000095_merchant_signature_policy.up.sql','000096_runtime_channel_fees.up.sql','000097_global_channel_last_used.up.sql','000099_runtime_channel_limits.up.sql')`).Scan(&registered); e != nil || registered != 4 {
		t.Fatal("original migration registration chain not preserved", registered, e)
	}
	for _, table := range []string{"core_payment_risk_policy", "core_payment_risk_groups", "core_risk_origins", "core_risk_successes", "core_risk_actions", "core_risk_check_runs", "core_risk_notices", "core_risk_notice_claims", "core_risk_notice_results"} {
		if _, exists := before[table]; exists {
			t.Fatal("test was not pre102")
		}
		var count int
		if e = p.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); e != nil || count != 0 {
			t.Fatal("upgrade fabricated risk history", table, count, e)
		}
	}
	var marked int
	if e = p.QueryRow(ctx, "SELECT count(*) FROM orders WHERE runtime_fee_quote_required").Scan(&marked); e != nil || marked != 1 {
		t.Fatal("upgrade fabricated historical fee birth markers", marked, e)
	}
	var riskMarkers int
	if e = p.QueryRow(ctx, "SELECT count(*) FROM provider_attempts WHERE core_risk_required").Scan(&riskMarkers); e != nil || riskMarkers != 0 {
		t.Fatal("upgrade fabricated risk birth", riskMarkers, e)
	}
	if e = Migrate(ctx, p); e != nil {
		t.Fatal("repeat actual Migrate", e)
	}
	repeated := snapshot()
	for name, digest := range before {
		if repeated[name] != digest {
			t.Fatal("repeat changed pre102 bytes", name)
		}
	}
	if e = VerifySchema(ctx, p); e != nil {
		t.Fatal("actual registry/checksum readiness", e)
	}

}
