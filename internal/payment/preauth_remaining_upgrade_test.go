package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestPreauthRemaining75UpgradeKeepsActualLegacyAmountAndUAWithoutInventingSources(t *testing.T) {
	s, facts := actualHistoricalStore155(t, "preauth74")
	ctx := context.Background()
	var o Order
	facts.decode(t, "order", &o)
	var e error
	const original = `SELECT jsonb_build_array(order_id,channel_id,request_no,auth_no,amount,state,sent,created_at,updated_at)::text FROM payment_authorization_releases WHERE order_id=$1`
	var before, after string
	if e = s.pool.QueryRow(ctx, original, o.ID).Scan(&before); e != nil {
		t.Fatal(e)
	}
	sql, e := os.ReadFile("../../db/migrations/000075_preauth_remaining_release.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, string(sql)); e != nil {
		t.Fatal(e)
	}
	if e = s.pool.QueryRow(ctx, original, o.ID).Scan(&after); e != nil || before != after {
		t.Fatal("migration rewrote actual original74 release", before, after, e)
	}
	r, e := s.AuthorizationRelease(ctx, 70, o.TradeNo)
	if e != nil || r.SourceRevision != 0 || r.OriginalAmount != 0 || r.Amount != 125 || r.RequestNo != fmt.Sprintf("UA%d", o.ID) {
		t.Fatal(r, e)
	}
	raw, _ := json.Marshal(r)
	var view map[string]any
	json.Unmarshal(raw, &view)
	if _, ok := view["source_revision"]; ok {
		t.Fatal("legacy response shape changed", view)
	}
	var children int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM authorization_remaining_sources)+(SELECT count(*) FROM authorization_remaining_claims)`).Scan(&children); e != nil || children != 0 {
		t.Fatal("invented historical remaining authority", children, e)
	}
	if e = s.MarkAuthorizationReleaseSent(ctx, o.TradeNo); e != nil {
		t.Fatal("legacy first-send behavior lost", e)
	}
	if e = s.ResolveAuthorizationRelease(ctx, o.TradeNo, "succeeded"); e != nil {
		t.Fatal(e)
	}
	// Historical74->75 assertions are complete. Install76 only for the latest
	// whole-system checker; no stop, notice or historical source is backfilled.
	later, e := os.ReadFile("../../db/migrations/000076_channel_auto_stop.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, string(later)); e != nil {
		t.Fatal(e)
	}
	check, e := s.CheckLedger(ctx)
	if e != nil || check != (LedgerCheck{}) {
		t.Fatal(check, e)
	}
}
