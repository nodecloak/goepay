package payment

import (
	"context"
	"testing"
)

func TestCollectionGeneratorLedgerMissingSchemaCannotHideIndependentAnchors(t *testing.T) {
	for _, anchor := range []string{"legacy_empty", "audit", "record", "migration"} {
		t.Run(anchor, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			if _, err := s.pool.Exec(ctx, `DROP TABLE collection_code_generations`); err != nil {
				t.Fatal(err)
			}
			switch anchor {
			case "audit":
				if _, err := s.pool.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation) VALUES('merchant',101,'synthetic','collection-generator',11901,'generate')`); err != nil {
					t.Fatal(err)
				}
			case "record":
				if _, err := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('collection-codes','{"generated":true}')`); err != nil {
					t.Fatal(err)
				}
			case "migration":
				if _, err := s.pool.Exec(ctx, `CREATE TABLE schema_migrations(name text PRIMARY KEY,sha256 text);INSERT INTO schema_migrations(name,sha256) VALUES('000086_collection_code_generations.up.sql','synthetic-test-only')`); err != nil {
					t.Fatal(err)
				}
			}
			report, err := s.CheckLedger(ctx)
			want := int64(1)
			if anchor == "legacy_empty" {
				want = 0
			}
			if err != nil || report.InvalidCollectionGenerators != want {
				t.Fatalf("missing table %s hidden or crashes checker: got %d want %d err %v", anchor, report.InvalidCollectionGenerators, want, err)
			}
		})
	}
}
