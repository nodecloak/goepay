package server

import "testing"

func TestKYC233LedgerReportingTypeAndFilter(t *testing.T) {
	f := channel168Local(t)
	var kind string
	query := `WITH j(event_key,description) AS (VALUES ('certification233:0123456789abcdef0123456789abcdef','实名认证')),a(kind) AS (VALUES('available')),e(amount) AS (VALUES(-25::bigint)) SELECT ` + ledgerTypeExpression + ` FROM j,a,e`
	if err := f.p.QueryRow(t.Context(), query).Scan(&kind); err != nil || kind != "certification" {
		t.Fatalf("certification fee lost its original ledger type: %q %v", kind, err)
	}
	predicate, ok := ledgerTypeCodes["certification"]
	if !ok {
		t.Fatal("certification filter missing")
	}
	var count int
	query = `WITH j(event_key,description) AS (VALUES ('certification233:0123456789abcdef0123456789abcdef','实名认证'),('admin-adjust:unrelated','实名认证')),a(kind) AS (VALUES('available')),e(amount) AS (VALUES(-25::bigint)) SELECT count(*) FROM j,a,e WHERE ` + predicate
	if err := f.p.QueryRow(t.Context(), query).Scan(&count); err != nil || count != 1 {
		t.Fatal("localized description must not grant certification classification", count, err)
	}
}
