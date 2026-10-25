package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/db"
	"testing"
)

func TestIncomeFundingOriginalFreezeActorRemainsBoundAfterAdoption(t *testing.T) {
	s := incomeFundingFixture(t, true)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 6305, Name: "original actor", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	o, h := incomeLegacyHold(t, s, 6305, "actor")
	if e := db.Migrate(ctx, s.pool); e != nil {
		t.Fatal(e)
	}
	guarded := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { return nil })
	if _, e := s.AdoptOrderIncomeHold(guarded, 6305, o.TradeNo, h.ID, "adopt-actor", IncomeHoldActor{ID: 1, Name: "new-super", Class: "super"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, "UPDATE order_income_hold_events SET actor_name='forged-original' WHERE hold_id=$1", h.ID); e == nil {
		t.Fatal("normal immutable event accepted mutation")
	}
	// Corrupt only the two historical audit families in this isolated schema;
	// all new funding/HMAC records, ledger guards and original journals remain.
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `ALTER TABLE order_income_hold_events DISABLE TRIGGER order_income_events_immutable;ALTER TABLE backoffice_audit DISABLE TRIGGER backoffice_audit_immutable`); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE order_income_hold_events SET actor_name='forged-original' WHERE hold_id=$1 AND action='freeze'", h.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE backoffice_audit SET actor_name='forged-original' WHERE resource='orders' AND operation='freeze-income' AND resource_id=$1", o.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `ALTER TABLE order_income_hold_events ENABLE TRIGGER order_income_events_immutable;ALTER TABLE backoffice_audit ENABLE TRIGGER backoffice_audit_immutable`); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := s.OrderIncomeHold(ctx, 6305, o.TradeNo); !errors.Is(e, ErrConflict) {
		t.Fatal("old freeze actor was reassigned under a genuine new adoption proof", e)
	}
	// Restoring SQL shape still cannot authorize rewriting the adopter's sealed
	// witness. Only this isolated new immutable trigger is additionally degraded.
	tx, e = s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, "ALTER TABLE order_income_hold_fundings DISABLE TRIGGER income_funding_immutable"); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE order_income_hold_fundings SET freeze_actor_name='forged-original' WHERE hold_id=$1", h.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "ALTER TABLE order_income_hold_fundings ENABLE TRIGGER income_funding_immutable"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	var valid bool
	if e = s.pool.QueryRow(ctx, "SELECT valid FROM order_income_hold_funding_evidence WHERE id=$1", h.ID).Scan(&valid); e != nil || !valid {
		t.Fatal("fixture did not reach keyed witness validation", valid, e)
	}
	if _, e = s.OrderIncomeHold(ctx, 6305, o.TradeNo); !errors.Is(e, ErrConflict) {
		t.Fatal("SQL-valid witness rewrite was resealed", e)
	}
	if report, e := s.CheckLedger(ctx); e != nil || report.InvalidOrderIncomeFundings != 1 {
		t.Fatal(report, e)
	}
}

func TestIncomeFundingScopedAccountRejectsUnrelatedBalancedRoundTrip(t *testing.T) {
	s := incomeFundingFixture(t, false)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 6304, Name: "scope", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	o := incomePaidOrder(t, s, 6304, "roundtrip")
	h, e := s.FreezeOrderIncome(ctx, 6304, o.TradeNo, "hold", incomeTestActor)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	var from, to int64
	if e = tx.QueryRow(ctx, "SELECT f.account_id,a.id FROM order_income_hold_fundings f JOIN accounts a ON(a.owner_id,a.kind,a.scope_id)=(6304,'available',0) WHERE f.hold_id=$1", h.ID).Scan(&from, &to); e != nil {
		t.Fatal(e)
	}
	if e = postByAccountIDs(ctx, tx, "local:unauthorized-income:out", "synthetic unrelated debit", from, to, 1); e != nil {
		t.Fatal(e)
	}
	if e = postByAccountIDs(ctx, tx, "local:unauthorized-income:back", "synthetic unrelated return", to, from, 1); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e == nil {
		t.Fatal("unrelated original-reserve journals accepted merely because ending balance matched")
	}
}
