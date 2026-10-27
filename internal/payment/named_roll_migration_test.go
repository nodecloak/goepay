package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestOriginalNamedRollMigrationPreservesEveryHistoricalRow(t *testing.T) {
	s, facts := actualHistorical73Store(t)
	ctx := context.Background()
	paths, e := filepath.Glob("../../db/migrations/*.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	apply := func(path string) {
		t.Helper()
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.pool.Exec(ctx, string(raw)); e != nil {
			t.Fatal(filepath.Base(path), e)
		}
	}
	for _, path := range paths {
		version, e := strconv.Atoi(filepath.Base(path)[:6])
		if e != nil {
			t.Fatal(e)
		}
		if version > 73 && version < 94 {
			apply(path)
		}
	}
	rows, e := s.pool.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY tablename")
	if e != nil {
		t.Fatal(e)
	}
	var tables []string
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			t.Fatal(e)
		}
		tables = append(tables, name)
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	rows.Close()
	hash := func(table string) string {
		t.Helper()
		var value string
		if e := s.pool.QueryRow(ctx, "SELECT md5(COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text) FROM "+pgx.Identifier{table}.Sanitize()+" t").Scan(&value); e != nil {
			t.Fatal(table, e)
		}
		return value
	}
	before := map[string]string{}
	for _, table := range tables {
		before[table] = hash(table)
	}
	apply("../../db/migrations/000094_named_pay_roll_groups.up.sql")
	for _, table := range tables {
		if got := hash(table); got != before[table] {
			t.Fatal("named-roll index migration changed original row", table)
		}
	}
	if _, e := s.readSettlementPlanAuthorization(ctx, s.pool, facts.AuthorizationID); e != nil {
		t.Fatal("old authenticated authorization no longer readable", e)
	}
	if _, e := s.SettlementPlanRun(ctx, facts.RunID, settlementPlanAdmin); e != nil {
		t.Fatal("old authenticated run no longer readable", e)
	}
	t.Logf("Preserved all columns and rows in %d tables while changing only the routing uniqueness index", len(tables))
}
