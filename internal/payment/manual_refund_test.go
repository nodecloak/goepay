package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"sync"
	"testing"
)

var manualRefundActor = IncomeHoldActor{ID: 1, Name: "admin", Class: "super"}

func TestManualRefundUXInvalidReservationDoesNotRequireDatabase(t *testing.T) {
	s := &Store{}
	for _, input := range []struct {
		no     string
		amount int64
	}{{"", 10}, {"invalid", 0}, {"invalid", -1}} {
		if _, err := s.ReserveRefund(context.Background(), 1, "order", input.no, input.amount); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
}

func TestManualRefundUXUsesOriginalFundingAndNeverSendsProvider(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 991, 1000, "platform")
	o := incomePaidOrder(t, s, 991, "manual-refund")
	r, e := s.ManualRefund(ctx, 991, o.TradeNo, "manual-test-1", 30, manualRefundActor, "买家已收到线下退款", true)
	if e != nil || r.Status != "succeeded" {
		t.Fatal(r, e)
	}
	replay, e := s.ManualRefund(ctx, 991, o.TradeNo, "manual-test-1", 30, manualRefundActor, "买家已收到线下退款", true)
	if e != nil || replay.ID != r.ID {
		t.Fatal(replay, e)
	}
	if _, e = s.ManualRefund(ctx, 991, o.TradeNo, "manual-test-1", 31, manualRefundActor, "买家已收到线下退款", true); !errors.Is(e, ErrConflict) {
		t.Fatal("changed amount accepted", e)
	}
	b, e := s.Balance(ctx, 991)
	if e != nil || b.Available != 60 || b.Reserved != 0 {
		t.Fatal(b, e)
	}
	var providers, refunds, audits int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_operations WHERE order_id=$1),(SELECT count(*) FROM refunds WHERE order_id=$1),(SELECT count(*) FROM backoffice_audit WHERE resource='orders' AND resource_id=$1 AND operation='manual-refund')`, o.ID).Scan(&providers, &refunds, &audits); e != nil || providers != 0 || refunds != 1 || audits != 1 {
		t.Fatal(providers, refunds, audits, e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.ManualRefund(ctx, 991, o.TradeNo, "manual-test-final", 70, manualRefundActor, "线下剩余退款", true)
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	b, e = s.Balance(ctx, 991)
	if e != nil || b.Available != 0 || b.Reserved != 0 {
		t.Fatal(b, e)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidJournals != 0 || report.InvalidAccounts != 0 || report.InvalidProviderRefundResults != 0 {
		t.Fatal(report, e)
	}
}

func TestManualRefundUXRejectsUnacknowledgedWrongActorAndRollsBackAuditFailure(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 992, 1000, "platform")
	o := incomePaidOrder(t, s, 992, "manual-refund-rollback")
	for _, input := range []struct {
		actor  IncomeHoldActor
		reason string
		ack    bool
	}{{manualRefundActor, "线下退款", false}, {IncomeHoldActor{ID: 1, Name: "merchant", Class: "merchant"}, "线下退款", true}, {manualRefundActor, "", true}} {
		if _, e := s.ManualRefund(ctx, 992, o.TradeNo, "bad", 10, input.actor, input.reason, input.ack); e == nil {
			t.Fatal("unsafe manual refund accepted")
		}
	}
	_, e := s.pool.Exec(ctx, `CREATE FUNCTION reject_manual_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation='manual-refund' THEN RAISE EXCEPTION 'audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_manual_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_manual_audit()`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ManualRefund(ctx, 992, o.TradeNo, "audit-failure", 10, manualRefundActor, "线下退款", true); e == nil {
		t.Fatal("audit failure accepted")
	}
	b, _ := s.Balance(ctx, 992)
	var count int
	s.pool.QueryRow(ctx, "SELECT count(*) FROM refunds WHERE order_id=$1", o.ID).Scan(&count)
	if b.Available != 90 || b.Reserved != 0 || count != 0 {
		t.Fatal("partially committed refund", b, count)
	}
}

func TestManualRefundUXCannotAdoptProviderUnknownOrBypassFundsGuard(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 993, 1000, "platform")
	o := incomePaidOrder(t, s, 993, "manual-refund-unknown")
	r, e := s.ReserveRefund(ctx, 993, o.OutTradeNo, "api-unknown", 20)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "unknown"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ManualRefund(ctx, 993, o.TradeNo, "api-unknown", 20, manualRefundActor, "线下退款", true); !errors.Is(e, ErrConflict) {
		t.Fatal("adopted unknown original API refund", e)
	}
	if _, e = s.ManualRefund(ctx, 993, o.TradeNo, "over-limit", 90, manualRefundActor, "线下退款", true); e == nil {
		t.Fatal("ignored unknown reservation")
	}
	guarded := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { return ErrState })
	if _, e = s.ManualRefund(guarded, 993, o.TradeNo, "no-grant", 10, manualRefundActor, "线下退款", true); !errors.Is(e, ErrState) {
		t.Fatal("funds guard bypassed", e)
	}
}
