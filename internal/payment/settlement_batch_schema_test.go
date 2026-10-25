package payment

import (
	"context"
	"testing"
)

func TestSettlementBatches84CheckerSupportsActualOldSchemaButDetectsMissingNewTables(t *testing.T) {
	ctx := context.Background()
	old := membership72HistoricalStore(t, 82)
	if report, err := old.CheckLedger(ctx); err != nil || report.InvalidSettlementBatches != 0 {
		t.Fatal("actual pre83 empty schema no longer readable", report, err)
	}
	for _, which := range []string{"partial", "all-with-original-audit", "all-with-migration-marker"} {
		t.Run(which, func(t *testing.T) {
			s, items := settlementBatchFixture84(t)
			if which == "all-with-original-audit" {
				if _, err := s.CreateSettlementBatch(ctx, SettlementBatchInput{ExternalNo: "schema-check84", RequestIDs: []int64{items[0].ID}}, batchAdmin84); err != nil {
					t.Fatal(err)
				}
			}
			if which == "all-with-migration-marker" {
				if _, err := s.pool.Exec(ctx, `CREATE TABLE schema_migrations(name text PRIMARY KEY,sha256 text NOT NULL);INSERT INTO schema_migrations(name,sha256) VALUES('000083_settlement_batches.up.sql',repeat('a',64))`); err != nil {
					t.Fatal(err)
				}
			}
			if which == "partial" {
				if _, err := s.pool.Exec(ctx, `DROP TABLE settlement_batch_events`); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := s.pool.Exec(ctx, `DROP TABLE settlement_batch_events,settlement_batch_items,settlement_batches`); err != nil {
					t.Fatal(err)
				}
			}
			report, err := s.CheckLedger(ctx)
			if err != nil || report.InvalidSettlementBatches == 0 {
				t.Fatal("new source schema disappearance accepted", report, err)
			}
		})
	}
}
