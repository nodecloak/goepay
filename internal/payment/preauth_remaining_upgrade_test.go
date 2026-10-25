package payment

import (
	"context"
	"encoding/json"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"os"
	"testing"
)

func TestPreauthRemaining75UpgradeKeepsActualLegacyAmountAndUAWithoutInventingSources(t *testing.T) {
	s := membership72HistoricalStore(t, 74)
	ctx := context.Background()
	o := providerTestOrder(t, s)
	if _, _, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "preauth"); e != nil {
		t.Fatal(e)
	}
	if e := s.RecordPreauthorization(ctx, 7, adapter.AuthorizationEvent{TradeNo: o.TradeNo, RequestNo: o.TradeNo, AuthNo: "actual-legacy-auth75", BuyerID: "2088000000000075", Amount: o.Amount, State: "SUCCESS"}); e != nil {
		t.Fatal(e)
	}
	// Use the historical74 shape and its original guards before applying75.
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `INSERT INTO payment_authorization_releases(order_id,channel_id,request_no,auth_no,amount)VALUES($1,7,$2,'actual-legacy-auth75',$3)`, o.ID, fmt.Sprintf("UA%d", o.ID), o.Amount); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE payment_authorizations SET release_state='checking' WHERE order_id=$1`, o.ID); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
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
