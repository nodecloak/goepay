package payment

import (
	"context"
	"errors"
	"testing"

	provider "goepay/internal/adapters/payment"
)

// These tests observe existing refund entry points and the real durable queue.
// They deliberately do not require a new 201 table or an unimplemented symbol.
func refundEventJobs201(t *testing.T, s *Store) int {
	t.Helper()
	var count int
	if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE kind='merchant.refund-event'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestRefundEvent201TrustedPlatformSuccessCapturesExactlyOnce(t *testing.T) {
	for _, entry := range []string{"manual", "platform", "merchant"} {
		t.Run(entry, func(t *testing.T) {
			s := testStore(t)
			feeMerchant(t, s, 20101, 1000, "platform")
			o := incomePaidOrder(t, s, 20101, "platform-event-"+entry)
			var r Refund
			var err error
			if entry == "manual" {
				r, err = s.ManualRefund(t.Context(), 20101, o.TradeNo, "线下/退款201", 30, manualRefundActor, "已完成线下退款", true)
			} else {
				if entry == "merchant" {
					r, err = s.ReserveMerchantRefund(t.Context(), 20101, o.OutTradeNo, "平台/退款201", 30)
				} else {
					r, err = s.ReserveRefund(t.Context(), 20101, o.OutTradeNo, "平台/退款201", 30)
				}
				if err == nil {
					err = s.ResolveRefund(t.Context(), r.ID, "succeeded")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := refundEventJobs201(t, s); got != 1 {
				t.Fatalf("trusted %s refund succeeded without exactly one durable platform refund event: got %d, want 1", entry, got)
			}
			if entry == "manual" {
				_, err = s.ManualRefund(t.Context(), 20101, o.TradeNo, r.RefundNo, 30, manualRefundActor, "已完成线下退款", true)
			} else {
				err = s.ResolveRefund(t.Context(), r.ID, "succeeded")
			}
			if err != nil || refundEventJobs201(t, s) != 1 {
				t.Fatal("replayed trusted result created a second refund event", err)
			}
		})
	}
}

func TestRefundEvent201APIOriginCannotBecomePlatformOnReservationReplay(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 20102, 0, "platform")
	o := incomePaidOrder(t, s, 20102, "api-origin201")
	r, err := s.ReserveAPIRefund(t.Context(), 20102, o.OutTradeNo, "external-api201", 30)
	if err != nil {
		t.Fatal(err)
	}
	// Re-entering through the platform must preserve the creation-time origin.
	if _, err = s.ReserveRefund(t.Context(), 20102, o.OutTradeNo, r.RefundNo, 30); err != nil && !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err = s.ResolveRefund(t.Context(), r.ID, "succeeded"); err != nil {
		t.Fatal(err)
	}
	if got := refundEventJobs201(t, s); got != 0 {
		t.Fatalf("external API refund generated a platform event after replay: %d", got)
	}
}

func TestRefundEvent201PlatformOriginCannotBecomeAPIOnReservationReplay(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 20103, 0, "platform")
	o := incomePaidOrder(t, s, 20103, "platform-origin201")
	r, err := s.ReserveMerchantRefund(t.Context(), 20103, o.OutTradeNo, "console201", 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReserveAPIRefund(t.Context(), 20103, o.OutTradeNo, r.RefundNo, 30); err != nil && !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err = s.ResolveRefund(t.Context(), r.ID, "succeeded"); err != nil {
		t.Fatal(err)
	}
	if got := refundEventJobs201(t, s); got != 1 {
		t.Fatalf("platform origin was lost on external API replay: got %d events, want 1", got)
	}
}

func TestRefundEvent201OnlyTrustedSuccessCapturesAfterUnknownRecovery(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 20104, 0, "platform")
	o := incomePaidOrder(t, s, 20104, "recover201")
	r, err := s.ReserveRefund(t.Context(), 20104, o.OutTradeNo, "unknown201", 20)
	if err != nil {
		t.Fatal(err)
	}
	if got := refundEventJobs201(t, s); got != 0 {
		t.Fatal("reservation emitted a refund event", got)
	}
	if err = s.ResolveRefund(t.Context(), r.ID, "unknown"); err != nil {
		t.Fatal(err)
	}
	if got := refundEventJobs201(t, s); got != 0 {
		t.Fatal("unknown result emitted a successful refund event", got)
	}
	if err = s.ResolveRefund(t.Context(), r.ID, "succeeded"); err != nil {
		t.Fatal(err)
	}
	if got := refundEventJobs201(t, s); got != 1 {
		t.Fatalf("recovered trusted success did not capture exactly one refund event: %d", got)
	}
	failed, err := s.ReserveRefund(t.Context(), 20104, o.OutTradeNo, "failed201", 20)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ResolveRefund(t.Context(), failed.ID, "failed"); err != nil || refundEventJobs201(t, s) != 1 {
		t.Fatal("failed refund emitted an event", err)
	}
}

func TestRefundEvent201ProviderQueryRecoveryCapturesPlatformButNeverAPIRefund(t *testing.T) {
	for _, origin := range []string{"platform", "merchant", "api"} {
		t.Run(origin, func(t *testing.T) {
			s, flow, upstream, o := setupOperations(t)
			if _, err := flow.Query(t.Context(), 1, o.TradeNo); err != nil {
				t.Fatal(err)
			}
			upstream.unknown = true
			var err error
			switch origin {
			case "platform":
				_, err = flow.Refund(t.Context(), 1, o.TradeNo, "provider201", 30)
			case "merchant":
				_, err = flow.RefundFromMerchant(t.Context(), 1, o.TradeNo, "provider201", 30)
			case "api":
				_, err = flow.RefundFromAPI(t.Context(), 1, o.TradeNo, "provider201", 30)
			}
			var unknown *provider.UnknownOutcome
			if !errors.As(err, &unknown) || upstream.refunds.Load() != 1 || refundEventJobs201(t, s) != 0 {
				t.Fatal("fixture did not retain a single genuinely unknown provider result", err, upstream.refunds.Load())
			}
			r, err := flow.QueryRefund(t.Context(), 1, "provider201")
			if err != nil || r.Status != "succeeded" {
				t.Fatal("provider query did not recover trusted refund success", r, err)
			}
			want := 1
			if origin == "api" {
				want = 0
			}
			if got := refundEventJobs201(t, s); got != want {
				t.Fatalf("trusted provider recovery lost immutable %s origin: got %d event jobs, want %d", origin, got, want)
			}
			if _, err = flow.QueryRefund(t.Context(), 1, "provider201"); err != nil || refundEventJobs201(t, s) != want || upstream.refunds.Load() != 1 {
				t.Fatal("query replay duplicated event or resent original refund", err)
			}
		})
	}
}

type immediateRefundProvider201 struct{ *operationsProvider }

func (p immediateRefundProvider201) Refund(_ context.Context, r provider.RefundRequest) (provider.RefundResult, error) {
	p.refunds.Add(1)
	return provider.RefundResult{RefundNo: r.RefundNo, Amount: r.Amount, State: "SUCCESS"}, nil
}

func TestRefundEvent201ImmediateProviderSuccessIsPlatformEventDespiteUpstreamAPIUse(t *testing.T) {
	s, flow, upstream, o := setupOperations(t)
	flow.Providers[7] = immediateRefundProvider201{upstream}
	if _, err := flow.Query(t.Context(), 1, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	r, err := flow.Refund(t.Context(), 1, o.TradeNo, "immediate-upstream201", 30)
	if err != nil || r.Status != "succeeded" || upstream.refunds.Load() != 1 {
		t.Fatal("fixture did not complete the platform refund through upstream API", r, err)
	}
	if got := refundEventJobs201(t, s); got != 1 {
		t.Fatalf("using an upstream refund API suppressed the platform event: got %d, want 1", got)
	}
}

func TestRefundEvent201ManualAuditFailureRollsBackCapturedEvent(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 20105, 0, "platform")
	o := incomePaidOrder(t, s, 20105, "rollback201")
	if _, err := s.pool.Exec(t.Context(), `CREATE FUNCTION reject_manual_event201() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation='manual-refund' THEN RAISE EXCEPTION 'synthetic audit failure201'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_manual_event201 BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_manual_event201()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ManualRefund(t.Context(), 20105, o.TradeNo, "rollback201", 30, manualRefundActor, "线下退款", true); err == nil {
		t.Fatal("manual audit failure committed")
	}
	var refunds int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM refunds WHERE order_id=$1`, o.ID).Scan(&refunds); err != nil || refunds != 0 || refundEventJobs201(t, s) != 0 {
		t.Fatal("rolled back refund left a publishable event", refunds, err)
	}
}
