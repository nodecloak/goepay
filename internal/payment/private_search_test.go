package payment

import (
	"context"
	"encoding/json"
	"errors"
	"goepay/internal/privatesearch"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPrivateSearchBackfillVerifiesActualSourceAndNeverChangesHistory(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	const mid int64 = 15801
	if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "Local Search", APIKey: "local-only158"}); e != nil {
		t.Fatal(e)
	}
	for i, name := range []string{"Original 张三", "Replacement 李四"} {
		if _, e := s.SetSettlementRecipient(ctx, mid, SettlementRecipientInput{Type: "alipay", Account: []string{"First.Private158@example.invalid", "Second.Private158@example.invalid"}[i], Name: name}, nil, SettlementRecipientActor{Role: "merchant", ID: mid, Name: "local"}); e != nil {
			t.Fatal(e)
		}
	}
	fingerprint := func() string {
		var hash string
		if e := s.pool.QueryRow(ctx, `SELECT md5(jsonb_build_array((SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM settlement_recipient_configs t),(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM backoffice_audit t))::text)`).Scan(&hash); e != nil {
			t.Fatal(e)
		}
		return hash
	}
	before := fingerprint()
	if _, e := s.pool.Exec(ctx, "DELETE FROM private_search_projections"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.PrivateSearchIDs(ctx, s.pool, "settlement_config", mid, []privatesearch.Field{privatesearch.Name}, "张三"); !errors.Is(e, ErrState) {
		t.Fatal("partial projection silently broadened", e)
	}
	if e := s.RebuildPrivateSearch(ctx); e != nil {
		t.Fatal(e)
	}
	if e := s.RebuildPrivateSearch(ctx); e != nil {
		t.Fatal("rebuild not idempotent", e)
	}
	for _, q := range []string{"FIRST.PRIVATE158", "张三"} {
		ids, e := s.PrivateSearchIDs(ctx, s.pool, "settlement_config", mid, []privatesearch.Field{privatesearch.Account, privatesearch.Name}, q)
		if e != nil || len(ids) != 1 {
			t.Fatal("original encrypted source not searchable", q, ids, e)
		}
	}
	if ids, e := s.PrivateSearchIDs(ctx, s.pool, "settlement_config", mid+1, []privatesearch.Field{privatesearch.Name}, "张三"); e != nil || len(ids) != 0 {
		t.Fatal("tenant broadened", ids, e)
	}
	rows, e := s.pool.Query(ctx, "SELECT to_jsonb(p) FROM private_search_projections p")
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			t.Fatal(e)
		}
		for _, private := range []string{"Private158", "张三", "李四", "example.invalid"} {
			if strings.Contains(string(raw), private) {
				t.Fatal("plaintext index", private)
			}
		}
	}
	rows.Close()
	if fingerprint() != before {
		t.Fatal("index rebuild rewrote original recipient or audit")
	}
	if report, e := s.CheckLedger(ctx); e != nil || report != (LedgerCheck{}) {
		t.Fatal(report, e)
	}
}

func TestPrivateSearchIndexFailureRollsBackRecipientAndDetachedProofFailsClosed(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	const mid int64 = 15802
	if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "Local Search Rollback", APIKey: "local-only158"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION reject_search158() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'synthetic index failure158';END$$;CREATE TRIGGER reject_search158 BEFORE INSERT ON private_search_projections FOR EACH ROW EXECUTE FUNCTION reject_search158()`); e != nil {
		t.Fatal(e)
	}
	in := SettlementRecipientInput{Type: "alipay", Account: "proof158@example.invalid", Name: "Local Proof"}
	actor := SettlementRecipientActor{Role: "merchant", ID: mid, Name: "local"}
	if _, e := s.SetSettlementRecipient(ctx, mid, in, nil, actor); e == nil {
		t.Fatal("index failure left original config committed")
	}
	var count int
	if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM settlement_recipient_configs WHERE merchant_id=$1)+(SELECT count(*) FROM settlement_recipient_current WHERE merchant_id=$1)+(SELECT count(*) FROM backoffice_audit WHERE resource='settlement-recipient' AND resource_id=$1)", mid).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
	if _, e := s.pool.Exec(ctx, "DROP TRIGGER reject_search158 ON private_search_projections"); e != nil {
		t.Fatal(e)
	}
	c, e := s.SetSettlementRecipient(ctx, mid, in, nil, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE private_search_projections SET proof_hash=decode(repeat('00',32),'hex') WHERE source_kind='settlement_config' AND source_id=$1", c.ConfigID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PrivateSearchIDs(ctx, s.pool, "settlement_config", mid, []privatesearch.Field{privatesearch.Account}, "proof158"); !errors.Is(e, ErrConflict) {
		t.Fatal("detached projection proof trusted", e)
	}
	if e = s.RebuildPrivateSearch(ctx); !errors.Is(e, ErrConflict) {
		t.Fatal("startup silently trusted or resealed corrupted projection", e)
	}
}

func TestPrivateSearchFullSubstringFiltersOpaqueCandidateFalsePositives(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	const mid int64 = 15803
	if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "Local Search Candidate", APIKey: "local-only158"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SetSettlementRecipient(ctx, mid, SettlementRecipientInput{Type: "alipay", Account: "ababa", Name: "本地候选"}, nil, SettlementRecipientActor{Role: "merchant", ID: mid, Name: "local"}); e != nil {
		t.Fatal(e)
	}
	ids, e := s.PrivateSearchIDs(ctx, s.pool, "settlement_config", mid, []privatesearch.Field{privatesearch.Account}, "ababab")
	if e != nil || len(ids) != 0 {
		t.Fatal("opaque token candidate became false result", ids, e)
	}
	raw, _ := json.Marshal(ids)
	if string(raw) != "[]" {
		t.Fatal("empty result must remain canonical", string(raw))
	}
}

func TestPrivateSearchMigrationBackfillsActualHistoricalSourcesWithoutChangingAnyOriginalRow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema int
		kind   string
		field  privatesearch.Field
		query  string
	}{{"settlement_schema73", 73, "settlement_config", privatesearch.Account, "local-recipient@example.test"}, {"stop75", 75, "transfer_intent", privatesearch.Account, "local-recipient@example.test"}} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := actualHistoricalStore155(t, tc.name)
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
				if version > tc.schema && version < 92 {
					apply(path)
				}
			}
			hash := func(table string) string {
				var out string
				if e := s.pool.QueryRow(ctx, "SELECT md5(COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text) FROM "+table+" t").Scan(&out); e != nil {
					t.Fatal(table, e)
				}
				return out
			}
			rows, e := s.pool.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY tablename")
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
			before := map[string]string{}
			for _, name := range names {
				before[name] = hash(name)
			}
			apply("../../db/migrations/000092_private_search_projection.up.sql")
			if _, e = s.PrivateSearchIDs(ctx, s.pool, tc.kind, 0, []privatesearch.Field{tc.field}, tc.query); !errors.Is(e, ErrState) {
				t.Fatal("missing historical projection silently accepted", e)
			}
			if e = s.RebuildPrivateSearch(ctx); e != nil {
				t.Fatal("actual historical source index failed", e)
			}
			ids, e := s.PrivateSearchIDs(ctx, s.pool, tc.kind, 0, []privatesearch.Field{tc.field}, tc.query)
			if e != nil || len(ids) != 1 {
				t.Fatal("actual original historical search", ids, e)
			}
			for _, name := range names {
				if hash(name) != before[name] {
					t.Fatal("index migration/backfill rewrote historical table", name)
				}
			}
			report, e := s.CheckLedger(ctx)
			if e != nil || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
				t.Fatal(report, e)
			}
			t.Logf("Preserved all original rows in %d pre92 tables while indexing authentic encrypted history", len(names))
		})
	}
}
