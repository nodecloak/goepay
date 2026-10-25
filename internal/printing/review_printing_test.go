package printing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	provider "goepay/internal/adapters/payment"
	"goepay/internal/payment"
)

// These ordered cases reproduce both possible serializations of the automatic
// preparation/manual request race. Both paths take the merchant row lock, so a
// missing logical exclusion remains reproducible without scheduler timing.
func TestReviewPrintingManualBeforeDelayedAutomaticDoesNotPrintTwice(t *testing.T) {
	s, pay := printFixture(t)
	configurePrinting(t, s, 2)
	ctx := context.Background()
	o, _, _, _ := printPaidOrder(t, pay, "review-manual-before-auto")
	manual, err := s.CreateManual(ctx, 901, o.TradeNo, ManualInput{ExternalNo: "review-original-manual"}, printOwner)
	if err != nil {
		t.Fatal(err)
	}
	automatic, err := s.PrepareAutomatic(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	connector := &countedPrintConnector{}
	if _, err = s.Dispatch(ctx, manual.ID, connector); err != nil {
		t.Fatal(err)
	}
	if automatic != nil {
		if _, err = s.Dispatch(ctx, automatic.ID, connector); err != nil {
			t.Fatal(err)
		}
	}
	var intents int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM printing_intents WHERE order_id=$1", o.ID).Scan(&intents); err != nil {
		t.Fatal(err)
	}
	if automatic != nil || intents != 1 || connector.calls.Load() != 1 {
		t.Fatalf("delayed automatic preparation duplicated an explicitly requested original print: automatic=%+v intents=%d sends=%d", automatic, intents, connector.calls.Load())
	}
}

func TestReviewPrintingQueuedAutomaticBlocksUnconfirmedManualDuplicate(t *testing.T) {
	s, pay := printFixture(t)
	configurePrinting(t, s, 2)
	ctx := context.Background()
	o, _, _, _ := printPaidOrder(t, pay, "review-auto-before-manual")
	automatic, err := s.PrepareAutomatic(ctx, o.ID)
	if err != nil || automatic == nil {
		t.Fatal(automatic, err)
	}
	manual, manualErr := s.CreateManual(ctx, 901, o.TradeNo, ManualInput{ExternalNo: "review-unconfirmed-extra"}, printOwner)
	connector := &countedPrintConnector{}
	if _, err = s.Dispatch(ctx, automatic.ID, connector); err != nil {
		t.Fatal(err)
	}
	if manualErr == nil {
		if _, err = s.Dispatch(ctx, manual.ID, connector); err != nil {
			t.Fatal(err)
		}
	}
	var intents int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM printing_intents WHERE order_id=$1", o.ID).Scan(&intents); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(manualErr, ErrState) || intents != 1 || connector.calls.Load() != 1 {
		t.Fatalf("queued automatic print accepted an unconfirmed duplicate: manual=%+v err=%v intents=%d sends=%d", manual, manualErr, intents, connector.calls.Load())
	}
}

func reviewPrintingPendingPayment(t *testing.T, pay *payment.Store, no string) (payment.Order, payment.ProviderFlow, http.Header, []byte) {
	t.Helper()
	ctx := context.Background()
	o, err := pay.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 901, OutTradeNo: no, Name: "local independently verified item", Amount: 123, Type: "wxpay"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = pay.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); err != nil {
		t.Fatal(err)
	}
	body := []byte(o.TradeNo + ":123")
	mac := hmac.New(sha256.New, []byte("local-only-payment-signer"))
	mac.Write(body)
	return o, payment.ProviderFlow{Store: pay, Providers: map[int64]provider.Provider{7: printPaymentVerifier{}}}, http.Header{"Local-Signature": []string{hex.EncodeToString(mac.Sum(nil))}}, body
}

func TestReviewPrintingDedicatedInsertFaultCannotRollbackVerifiedPayment(t *testing.T) {
	for _, fault := range []string{"payment_print_sources", "printing_prepare_job"} {
		t.Run(fault, func(t *testing.T) {
			s, pay := printFixture(t)
			configurePrinting(t, s, 2)
			ctx := context.Background()
			o, flow, headers, body := reviewPrintingPendingPayment(t, pay, "review-print-insert-fault")
			trigger := `CREATE TRIGGER review_print_insert_fail BEFORE INSERT ON payment_print_sources FOR EACH ROW EXECUTE FUNCTION review_print_insert_fail()`
			if fault == "printing_prepare_job" {
				trigger = `CREATE TRIGGER review_print_insert_fail BEFORE INSERT ON jobs FOR EACH ROW WHEN (NEW.kind='receipt-print.prepare') EXECUTE FUNCTION review_print_insert_fail()`
			}
			if _, err := s.pool.Exec(ctx, `CREATE FUNCTION review_print_insert_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'local optional printing insert failure'; END $$;`+trigger); err != nil {
				t.Fatal(err)
			}
			notifyErr := flow.HandleNotification(ctx, 7, o.TradeNo, headers, body)
			order, readErr := pay.OrderByTradeNo(ctx, o.TradeNo)
			balance, balanceErr := pay.Balance(ctx, 901)
			var sources, printJobs, receipts, paidOutbox int
			err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM payment_print_sources),(SELECT count(*) FROM jobs WHERE kind LIKE 'receipt-print.%'),(SELECT count(*) FROM verified_payment_receipts WHERE order_id=$1),(SELECT count(*) FROM outbox WHERE event_key=$2 AND kind='payment.paid')`, o.ID, "payment:"+o.TradeNo).Scan(&sources, &printJobs, &receipts, &paidOutbox)
			if notifyErr != nil || readErr != nil || balanceErr != nil || err != nil || order.Status != "paid" || balance.Available != 123 || receipts != 1 || paidOutbox != 1 || sources != 0 || printJobs != 0 {
				t.Fatalf("optional print-only insert fault rolled back verified payment or left partial printing state: notify=%v read=%v balanceErr=%v query=%v status=%s available=%d receipts=%d paidOutbox=%d sources=%d printJobs=%d", notifyErr, readErr, balanceErr, err, order.Status, balance.Available, receipts, paidOutbox, sources, printJobs)
			}
			if _, err = s.Receipt(ctx, 901, o.TradeNo, printOwner); !errors.Is(err, ErrState) {
				t.Fatal("missing print fact was reconstructed from mutable paid projections", err)
			}
			if _, err = s.CreateManual(ctx, 901, o.TradeNo, ManualInput{ExternalNo: "review-no-optional-fact"}, printOwner); !errors.Is(err, ErrState) {
				t.Fatal("missing print fact allowed manual printing", err)
			}
			table := "payment_print_sources"
			if fault == "printing_prepare_job" {
				table = "jobs"
			}
			if _, err = s.pool.Exec(ctx, "DROP TRIGGER review_print_insert_fail ON "+table); err != nil {
				t.Fatal(err)
			}
			// A genuinely reverified callback may establish a historical source,
			// but must never invent the lost original automatic-print snapshot.
			if err = flow.HandleNotification(ctx, 7, o.TradeNo, headers, body); err != nil {
				t.Fatal("verified paid replay failed after optional fault cleared", err)
			}
			if automatic, err := s.PrepareAutomatic(ctx, o.ID); err != nil || automatic != nil {
				t.Fatal("print recovery retroactively created an automatic intent", automatic, err)
			}
			balance, err = pay.Balance(ctx, 901)
			if err != nil || balance.Available != 123 {
				t.Fatal("paid replay duplicated original payment credit", balance, err)
			}
		})
	}
}

func TestReviewPrintingUnsignedPaidProjectionsCannotInventTrustedReceipt(t *testing.T) {
	for _, forgedSource := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing_source", true: "zero_hmac_source"}[forgedSource], func(t *testing.T) {
			s, pay := printFixture(t)
			configurePrinting(t, s, 2)
			ctx := context.Background()
			o, err := pay.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 901, OutTradeNo: "review-unsigned-print-projections", Name: "local item", Amount: 123, Type: "wxpay"})
			if err != nil {
				t.Fatal(err)
			}
			// The legacy internal helper can credit an order without asserting a
			// provider receipt. A cached verified_receipts row is not a new call's
			// cryptographic provider verification and must not create print proof.
			if err = pay.MarkPaid(ctx, o.TradeNo, "local-internal-unverified-print-event"); err != nil {
				t.Fatal(err)
			}
			if _, err = s.pool.Exec(ctx, "INSERT INTO verified_payment_receipts(order_id,channel_id,provider_trade_no) VALUES($1,7,'LOCAL_UNSIGNED_PRINT_CACHE')", o.ID); err != nil {
				t.Fatal(err)
			}
			want := ErrState
			if forgedSource {
				fact := payment.PrintingPaidSource{OrderID: o.ID, MerchantID: 901, TradeNo: o.TradeNo, Name: o.Name, Amount: 123, Type: "wxpay", ChannelID: 7, ProviderTradeNo: "LOCAL_UNSIGNED_PRINT_CACHE", MerchantAmount: 123, Origin: "api", PlatformConfigID: 1, MerchantConfigID: 1, PlatformEnabled: true, Mode: 2, SnapshotState: "captured", Fresh: true}
				raw, e := json.Marshal(fact)
				if e != nil {
					t.Fatal(e)
				}
				if _, err = s.pool.Exec(ctx, "INSERT INTO payment_print_sources(order_id,merchant_id,body,proof_hash) VALUES($1,901,$2,$3)", o.ID, raw, make([]byte, 32)); err != nil {
					t.Fatal(err)
				}
				want = ErrConflict
			}
			if _, err = s.Receipt(ctx, 901, o.TradeNo, printOwner); !errors.Is(err, want) {
				t.Fatal("unsigned paid projections were treated as trusted receipt evidence", err)
			}
			if _, err = s.CreateManual(ctx, 901, o.TradeNo, ManualInput{ExternalNo: "review-unsigned-print"}, printOwner); !errors.Is(err, want) {
				t.Fatal("unsigned paid projections allowed manual printing", err)
			}
			if _, err = s.PrepareAutomatic(ctx, o.ID); !errors.Is(err, ErrConflict) {
				t.Fatal("unsigned paid projections allowed automatic printing", err)
			}
			if _, err = s.Receipt(ctx, 902, o.TradeNo, Actor{Role: "merchant", ID: 902, Name: "other-owner"}); !errors.Is(err, ErrNotFound) {
				t.Fatal("receipt read crossed original merchant ownership", err)
			}
			var intents int
			if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM printing_intents").Scan(&intents); err != nil || intents != 0 {
				t.Fatal("unsigned source left a print intent", intents, err)
			}
		})
	}
}
