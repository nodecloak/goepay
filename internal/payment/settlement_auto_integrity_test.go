package payment

import (
	"context"
	"errors"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

func TestSettlementAuto73ClaimAuditFaultRollsBackApprovalIntentAndBudget(t *testing.T) {
	s, g, provider, _, _, id := autoReady73(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION reject_auto73_claim() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='claim' AND NEW.actor_role='system' THEN RAISE EXCEPTION 'local automatic audit fault';END IF;RETURN NEW;END $$;CREATE TRIGGER reject_auto73 BEFORE INSERT ON settlement_payout_events FOR EACH ROW EXECUTE FUNCTION reject_auto73_claim()`); e != nil {
		t.Fatal(e)
	}
	if _, e := g.ExecuteSettlementAuto(ctx, id, nil); e == nil || provider.transfers.Load() != 0 {
		t.Fatal("failed automatic audit reached provider", e)
	}
	var status string
	var claims, payouts, transfers int
	if e := s.pool.QueryRow(ctx, `SELECT l.status,(SELECT count(*) FROM settlement_auto_claims),(SELECT count(*) FROM settlement_payouts),(SELECT count(*) FROM transfers) FROM ledger_requests l WHERE id=$1`, id).Scan(&status, &claims, &payouts, &transfers); e != nil || status != "pending" || claims != 0 || payouts != 0 || transfers != 0 {
		t.Fatal("automatic audit failure partly committed", status, claims, payouts, transfers, e)
	}
	b, e := s.Balance(ctx, 7395)
	if e != nil || b.Available != 1000 || b.Reserved != 1000 {
		t.Fatal(b, e)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidSettlementPlans != 0 || report.InvalidSettlementFundings != 0 || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
		t.Fatal(report, e)
	}
}

func TestSettlementAuto73MissingClaimCannotBecomeManualOrReachQuery(t *testing.T) {
	for _, eraseMarker := range []bool{false, true} {
		t.Run(map[bool]string{false: "parent-marker", true: "independent-system-audit"}[eraseMarker], func(t *testing.T) {
			s, g, provider, _, _, id := autoReady73(t)
			ctx := context.Background()
			out, e := g.ExecuteSettlementAuto(ctx, id, nil)
			var unknown *adapter.UnknownOutcome
			if !errors.As(e, &unknown) || out.State != "unknown" || provider.transfers.Load() != 1 {
				t.Fatal(out.State, e)
			}
			tx, e := s.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			if _, e = tx.Exec(ctx, "SET LOCAL session_replication_role=replica"); e != nil {
				t.Fatal(e)
			}
			if _, e = tx.Exec(ctx, "DELETE FROM settlement_auto_claims WHERE request_id=$1", id); e != nil {
				t.Fatal(e)
			}
			if eraseMarker {
				if _, e = tx.Exec(ctx, "UPDATE settlement_payouts SET auto_authorization_id=NULL WHERE request_id=$1", id); e != nil {
					t.Fatal(e)
				}
			}
			if e = tx.Commit(ctx); e != nil {
				t.Fatal(e)
			}
			if _, e = g.ExecuteSettlementAuto(ctx, id, nil); !errors.Is(e, ErrConflict) {
				t.Fatal("missing automatic child rearmed", e)
			}
			if _, e = g.QuerySettlementPayout(ctx, id, settlementAdmin); !errors.Is(e, ErrConflict) || provider.query.Load() != 0 || provider.transfers.Load() != 1 {
				t.Fatal("missing automatic child reached provider", e)
			}
			report, e := s.CheckLedger(ctx)
			if e != nil || report.InvalidSettlementPlans == 0 {
				t.Fatal("missing automatic source silently became historical manual payout", report, e)
			}
		})
	}
}
