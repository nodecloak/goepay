package payment

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSettlementThreshold74UpgradePreservesActual73NormalRequestProofAndDailyIntent(t *testing.T) {
	s, facts := actualHistorical73Store(t)
	ctx := context.Background()
	var e error
	var before, after []byte
	if e = s.pool.QueryRow(ctx, "SELECT proof_hash FROM settlement_plan_authorizations WHERE id=$1", facts.AuthorizationID).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(before, facts.ProofHash) {
		t.Fatal("restored original73 authority changed")
	}
	tables := []string{"settlement_plan_versions", "settlement_plan_authorizations", "settlement_plan_runs", "settlement_plan_items", "settlement_plan_budget_uses", "settlement_scheduled_intents", "ledger_requests", "settlement_request_terms", "settlement_fundings", "accounts", "journals", "ledger_entries", "backoffice_audit"}
	hash := func(table string) string {
		t.Helper()
		var out string
		if e = s.pool.QueryRow(ctx, "SELECT md5(COALESCE(jsonb_agg(to_jsonb(t)-ARRAY['threshold_source_version','threshold_source_hash','threshold_source_sequence'] ORDER BY (to_jsonb(t)-ARRAY['threshold_source_version','threshold_source_hash','threshold_source_sequence'])::text),'[]'::jsonb)::text) FROM "+table+" t").Scan(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	prior := map[string]string{}
	for _, table := range tables {
		prior[table] = hash(table)
		if prior[table] != facts.TableHashes[table] {
			t.Fatal("restored original73 row hash changed", table)
		}
	}
	raw, e := os.ReadFile("../../db/migrations/000074_settlement_threshold.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	for _, table := range tables {
		if hash(table) != prior[table] {
			t.Fatal("additive migration rewrote original73 rows", table)
		}
	}
	if e = s.pool.QueryRow(ctx, "SELECT proof_hash FROM settlement_plan_authorizations WHERE id=$1", facts.AuthorizationID).Scan(&after); e != nil || !bytes.Equal(before, after) {
		t.Fatal("original authority proof changed", e)
	}
	var version, sequence, children int
	var source []byte
	if e = s.pool.QueryRow(ctx, "SELECT threshold_source_version,threshold_source_sequence,threshold_source_hash,(SELECT count(*) FROM settlement_threshold_selections) FROM settlement_plan_runs WHERE id=$1", facts.RunID).Scan(&version, &sequence, &source, &children); e != nil || version != 0 || sequence != 0 || source != nil || children != 0 {
		t.Fatal("historical normal run received invented threshold proof", version, sequence, children, e)
	}
	// Current writers run only after the complete upgrade. Migration74 above
	// was checked independently against every original financial row.
	paths, err := filepath.Glob("../../db/migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		version, err := strconv.Atoi(filepath.Base(path)[:6])
		if err != nil {
			t.Fatal(err)
		}
		if version <= 74 {
			continue
		}
		later, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.pool.Exec(ctx, string(later)); err != nil {
			t.Fatal(filepath.Base(path), err)
		}
	}
	if e = s.GenerateSettlementPlanRun(ctx, facts.RunID); e != nil {
		t.Fatal("original normal replay no longer valid", e)
	}
	v, e := s.SettlementPlanRun(ctx, facts.RunID, settlementPlanAdmin)
	if e != nil || v.CreatedCount != 1 || len(v.Items) != 1 || v.Items[0].RequestID != facts.RequestID {
		t.Fatal(v, e)
	}
	// Historical daily authority belongs to its original date, independent of
	// the date on which this backup is tested. Replay cannot consume it again.
	if e = s.GenerateSettlementPlanRun(ctx, facts.RunID); e != nil {
		t.Fatal(e)
	}
	if hash("settlement_scheduled_intents") != facts.TableHashes["settlement_scheduled_intents"] {
		t.Fatal("original daily intent changed during replay")
	}
	_, e = s.pool.Exec(ctx, "INSERT INTO settlement_scheduled_intents(merchant_id,business_date,item_id,request_id) SELECT merchant_id,business_date,item_id,request_id FROM settlement_scheduled_intents WHERE request_id=$1", facts.RequestID)
	var duplicate *pgconn.PgError
	if !errors.As(e, &duplicate) || duplicate.Code != "23505" {
		t.Fatal("original daily slot is no longer unique", e)
	}
	if report, e := s.CheckLedger(ctx); e != nil || report != (LedgerCheck{}) {
		t.Fatal(report, e)
	}
}
