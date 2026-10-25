package payment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var incomeTestActor = IncomeHoldActor{ID: 1, Name: "operator", Class: "operator"}

func incomePaidOrder(t *testing.T, s *Store, mid int64, no string) Order {
	t.Helper()
	ctx := context.Background()
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: no, Name: "item", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "paid-"+no); e != nil {
		t.Fatal(e)
	}
	return o
}
func TestOrderIncomeHoldUsesNetLessSuccessfulRefundsAndNeverChangesPaid(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 401, 1000, "platform")
	o := incomePaidOrder(t, s, 401, "income-net")
	r, e := s.ReserveRefund(ctx, 401, o.OutTradeNo, "partial", 20)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	h, e := s.FreezeOrderIncome(ctx, 401, o.TradeNo, "freeze-1", incomeTestActor)
	if e != nil || h.Amount != 70 || h.MerchantNet != 90 || h.RefundMerchantAmount != 20 || h.State != "held" {
		t.Fatal(h, e)
	}
	b, _ := s.Balance(ctx, 401)
	current, _ := s.OrderByTradeNo(ctx, o.TradeNo)
	if b.Available != 0 || b.Reserved != 0 || current.Status != "paid" {
		t.Fatal("income hold mixed balances or payment status", b, current.Status)
	}
	heldRefund, e := s.ReserveRefund(ctx, 401, o.OutTradeNo, "held-refund", 10)
	if e != nil {
		t.Fatal("prepared held source did not fund refund", e)
	}
	if _, e = s.UnfreezeOrderIncome(ctx, 401, o.TradeNo, h.ID, incomeTestActor); !errors.Is(e, ErrState) {
		t.Fatal("pending held refund released", e)
	}
	if e = s.ResolveRefund(ctx, heldRefund.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	if replay, e := s.ReserveRefund(ctx, 401, o.OutTradeNo, "partial", 20); e != nil || replay.ID != r.ID {
		t.Fatal("old refund replay blocked by new hold", replay, e)
	}
	h, e = s.UnfreezeOrderIncome(ctx, 401, o.TradeNo, h.ID, incomeTestActor)
	if e != nil || h.State != "released" {
		t.Fatal(h, e)
	}
	b, _ = s.Balance(ctx, 401)
	if b.Available != 70 {
		t.Fatal(b)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
		t.Fatal(report, e)
	}
}
func TestOrderIncomeHoldConcurrentIdempotentCyclesAndOldReleaseCannotUnlockNew(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 402, Name: "income", APIKey: "synthetic-key"}); e != nil {
		t.Fatal(e)
	}
	o := incomePaidOrder(t, s, 402, "cycles")
	var wg sync.WaitGroup
	results := make(chan OrderIncomeHold, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, e := s.FreezeOrderIncome(ctx, 402, o.TradeNo, "original", incomeTestActor)
			if e != nil {
				t.Error(e)
			}
			results <- h
		}()
	}
	wg.Wait()
	close(results)
	var first OrderIncomeHold
	for h := range results {
		if first.ID == 0 {
			first = h
		}
		if h.ID != first.ID {
			t.Fatal("duplicate holds", first, h)
		}
	}
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.UnfreezeOrderIncome(ctx, 402, o.TradeNo, first.ID, incomeTestActor); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	second, e := s.FreezeOrderIncome(ctx, 402, o.TradeNo, "new-cycle", incomeTestActor)
	if e != nil || second.ID == first.ID {
		t.Fatal(second, e)
	}
	if _, e = s.UnfreezeOrderIncome(ctx, 402, o.TradeNo, first.ID, incomeTestActor); e != nil {
		t.Fatal(e)
	}
	b, _ := s.Balance(ctx, 402)
	if b.Available != 0 {
		t.Fatal("old unfreeze released new cycle", b)
	}
	if old, e := s.FreezeOrderIncome(ctx, 402, o.TradeNo, "original", incomeTestActor); e != nil || old.State != "released" {
		t.Fatal("old freeze request rearmed", old, e)
	}
}
func TestOrderIncomeHoldRejectsPendingRefundAndInsufficientPreallocatedFunds(t *testing.T) {
	for _, kind := range []string{"pending", "unknown", "preallocated", "unpaid"} {
		t.Run(kind, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			if e := s.CreateMerchant(ctx, Merchant{ID: 403, Name: "income", APIKey: "synthetic-key"}); e != nil {
				t.Fatal(e)
			}
			o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 403, OutTradeNo: kind, Name: "item", Amount: 100})
			if e != nil {
				t.Fatal(e)
			}
			if kind != "unpaid" {
				if e = s.MarkPaid(ctx, o.TradeNo, "paid-"+kind); e != nil {
					t.Fatal(e)
				}
			}
			switch kind {
			case "pending", "unknown":
				r, e := s.ReserveRefund(ctx, 403, o.OutTradeNo, "pending", 10)
				if e != nil {
					t.Fatal(e)
				}
				if kind == "unknown" {
					if e = s.ResolveRefund(ctx, r.ID, "unknown"); e != nil {
						t.Fatal(e)
					}
				}
			case "preallocated":
				if _, e = s.ReserveTransfer(ctx, 403, "transfer-hold", 10); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = s.FreezeOrderIncome(ctx, 403, o.TradeNo, "cannot-freeze", incomeTestActor); e == nil {
				t.Fatal("unsafe income frozen", kind)
			}
			b, _ := s.Balance(ctx, 403)
			if b.Available < 0 {
				t.Fatal("negative balance", b)
			}
		})
	}
}
func TestOrderIncomeHoldAuditFailureRollsBackJournalAndState(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 404, Name: "income", APIKey: "synthetic-key"}); e != nil {
		t.Fatal(e)
	}
	o := incomePaidOrder(t, s, 404, "audit")
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION reject_income_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'local audit failure'; END $$; CREATE TRIGGER reject_income_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_income_audit()`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.FreezeOrderIncome(ctx, 404, o.TradeNo, "audit-fail", incomeTestActor); e == nil {
		t.Fatal("audit failure committed freeze")
	}
	b, _ := s.Balance(ctx, 404)
	if b.Available != 100 {
		t.Fatal(b)
	}
	if _, e := s.OrderIncomeHold(ctx, 404, o.TradeNo); !errors.Is(e, ErrNotFound) {
		t.Fatal("failed audit left active cycle", e)
	}
	if _, e := s.pool.Exec(ctx, "DROP TRIGGER reject_income_audit ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	h, e := s.FreezeOrderIncome(ctx, 404, o.TradeNo, "after-audit-restored", incomeTestActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "CREATE TRIGGER reject_income_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_income_audit()"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.UnfreezeOrderIncome(ctx, 404, o.TradeNo, h.ID, incomeTestActor); e == nil {
		t.Fatal("audit failure committed unfreeze")
	}
	current, e := s.OrderIncomeHold(ctx, 404, o.TradeNo)
	b, _ = s.Balance(ctx, 404)
	if e != nil || current.State != "held" || b.Available != 0 {
		t.Fatal("failed unfreeze leaked funds", current, b, e)
	}
}

func TestOrderIncomeHoldRejectsMisclassifiedCreditWithoutOriginalClearingEvidence(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, id := range []int64{405, 406} {
		if e := s.CreateMerchant(ctx, Merchant{ID: id, Name: "evidence", APIKey: "synthetic-key"}); e != nil {
			t.Fatal(e)
		}
	}
	incomePaidOrder(t, s, 405, "source")
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 406, OutTradeNo: "bad-import", Name: "item", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if e = post(ctx, tx, "payment:"+o.TradeNo, "local mislabeled internal transfer", 405, "available", 406, "available", 100); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE orders SET status='paid',paid_at=now() WHERE id=$1", o.ID); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.FreezeOrderIncome(ctx, 406, o.TradeNo, "not-original-payment", incomeTestActor); e == nil {
		t.Fatal("misclassified merchant transfer accepted as original payment credit")
	}
}

func TestOrderIncomeHoldT1ExcludesHeldGrossWithoutChangingGrossPolicy(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 407, 1000, "platform")
	old, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 407, OutTradeNo: "old-100-net", Name: "item", Amount: 111})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, old.TradeNo, "old-income"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE orders SET paid_at=now()-interval '2 days' WHERE id=$1", old.ID); e != nil {
		t.Fatal(e)
	}
	today := incomePaidOrder(t, s, 407, "today-90-net")
	if _, e = s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settle_type","value":"1"}')`); e != nil {
		t.Fatal(e)
	}
	v, e := s.FundsAvailability(ctx, 407)
	if e != nil || v.LedgerAvailable != 190 || v.TodayGross != 100 || v.Available != 90 {
		t.Fatal(v, e)
	}
	h, e := s.FreezeOrderIncome(ctx, 407, today.TradeNo, "today-freeze", incomeTestActor)
	if e != nil {
		t.Fatal(e)
	}
	v, e = s.FundsAvailability(ctx, 407)
	if e != nil || v.LedgerAvailable != 100 || v.TodayGross != 0 || v.Available != 100 {
		t.Fatal("T1 double blocked already held income", v, e)
	}
	transfer, e := s.ReserveTransfer(ctx, 407, "old-funds-remain-available", 100)
	if e != nil {
		t.Fatal("held today's net blocked old withdrawable funds", e)
	}
	if e = s.ResolveTransfer(ctx, transfer.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.UnfreezeOrderIncome(ctx, 407, today.TradeNo, h.ID, incomeTestActor); e != nil {
		t.Fatal(e)
	}
	v, e = s.FundsAvailability(ctx, 407)
	if e != nil || v.LedgerAvailable != 190 || v.TodayGross != 100 || v.Available != 90 {
		t.Fatal("T1 did not restore original gross policy", v, e)
	}
}

func TestOrderIncomeHoldRefundRaceAndSuccessfulRefundLedgerEvidence(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 408, Name: "race", APIKey: "synthetic-key"}); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 4; i++ {
		no := fmt.Sprintf("race-%d", i)
		o := incomePaidOrder(t, s, 408, no)
		start := make(chan struct{})
		results := make(chan error, 2)
		go func() { <-start; _, e := s.FreezeOrderIncome(ctx, 408, o.TradeNo, no, incomeTestActor); results <- e }()
		go func() { <-start; _, e := s.ReserveRefund(ctx, 408, o.OutTradeNo, no, 20); results <- e }()
		close(start)
		owners := 0
		for n := 0; n < 2; n++ {
			if <-results == nil {
				owners++
			}
		}
		if owners < 1 || owners > 2 {
			t.Fatal("neither original source operation completed", owners)
		}
		r, e := s.Refund(ctx, 408, no)
		if e != nil {
			t.Fatal("refund must use either original available or own held source", e)
		}
		h, e := s.OrderIncomeHold(ctx, 408, o.TradeNo)
		if e == nil {
			if owners != 2 || h.RemainingAmount != 80 || h.RefundReservedAmount != 20 {
				t.Fatal("freeze-first refund lost original held principal", h, owners)
			}
			p, e := s.RefundFundingPreview(ctx, s.pool, o.ID, no, 20)
			if e != nil || p.HoldID != h.ID || p.HeldAmount != 20 || p.AvailableAmount != 0 {
				t.Fatal(p, e)
			}
		} else if !errors.Is(e, ErrNotFound) || owners != 1 {
			t.Fatal(h, e, owners)
		}
		if r.Status != "pending" {
			t.Fatal(r)
		}
	}
	bad := incomePaidOrder(t, s, 408, "incomplete-refund-ledger")
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	r, e := scanRefund(tx.QueryRow(ctx, "INSERT INTO refunds(merchant_id,order_id,refund_no,amount,merchant_amount) VALUES(408,$1,'bad-refund-import',20,20) RETURNING "+refundColumns, bad.ID))
	if e != nil {
		t.Fatal(e)
	}
	if e = seedIncomeTestRefundProof(ctx, s, tx, bad, r, 0); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE refunds SET status='succeeded' WHERE id=$1", r.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.insertRefundResultEvidence(ctx, tx, r, providerRefundResultEvidence{RefundID: r.ID, OrderID: r.OrderID, Amount: r.Amount, RefundNo: r.RefundNo, PriorRefundState: r.Status, State: "succeeded", Source: "internal"}); e != nil {
		t.Fatal(e)
	}
	var valid bool
	if e = tx.QueryRow(ctx, "SELECT valid FROM order_income_source_evidence WHERE order_id=$1", bad.ID).Scan(&valid); e != nil || valid {
		t.Fatal("original source accepted signed refund without original ledger", valid, e)
	}
	_, e = tx.Exec(ctx, "SET CONSTRAINTS provider_refund_result_requires_ledger IMMEDIATE")
	if e == nil {
		t.Fatal("59 accepted signed missing-ledger evidence")
	}
	if commit := tx.Commit(ctx); commit == nil {
		t.Fatal("new signed refund without original ledger committed")
	}
	var commitError *pgconn.PgError
	if !errors.As(e, &commitError) || commitError.Code != "P0001" || commitError.Message != "missing new refund terminal evidence" && commitError.Message != "invalid terminal refund evidence" {
		t.Fatal("refund failed for an unrelated reason", e)
	}
	t.Log("signed missing-ledger refund rejected by terminal evidence guard; original source was independently invalid")
	var imported int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM refunds WHERE id=$1", r.ID).Scan(&imported); e != nil || imported != 0 {
		t.Fatal("rejected refund import survived rollback", imported, e)
	}
	if h, e := s.FreezeOrderIncome(ctx, 408, bad.TradeNo, "rolled-back-refund-evidence", incomeTestActor); e != nil || h.Amount != 100 {
		t.Fatal("rolled-back bad refund poisoned original income", h, e)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidJournals != 0 || report.InvalidAccounts != 0 || report.InvalidProviderRefundResults != 0 {
		t.Fatal(report, e)
	}
}

func TestOrderIncomeHoldRejectsRefundDebitedToWrongLedgerCounterparty(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, mid := range []int64{409, 410} {
		if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "refund-evidence", APIKey: "synthetic-key"}); e != nil {
			t.Fatal(e)
		}
	}
	o := incomePaidOrder(t, s, 409, "wrong-refund-destination")
	r, e := s.ReserveRefund(ctx, 409, o.OutTradeNo, "misclassified-refund", 20)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if e = post(ctx, tx, fmt.Sprintf("refund:%d:resolve", r.ID), "local wrongly classified refund", 409, "reserved", 410, "available", 20); e != nil {
		t.Fatal(e)
	}
	var wrongJournal int64
	if e = tx.QueryRow(ctx, "SELECT id FROM journals WHERE event_key=$1", fmt.Sprintf("refund:%d:resolve", r.ID)).Scan(&wrongJournal); e != nil {
		t.Fatal(e)
	}
	if e = seedIncomeTestRefundProof(ctx, s, tx, o, r, wrongJournal); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE refunds SET status='succeeded' WHERE id=$1", r.ID); e != nil {
		t.Fatal(e)
	}
	// Supply a real keyed result so the test reaches counterparty validation,
	// rather than merely failing because migration59 requires a result proof.
	if e = s.insertRefundResultEvidence(ctx, tx, r, providerRefundResultEvidence{RefundID: r.ID, OrderID: r.OrderID, Amount: r.Amount, RefundNo: r.RefundNo, PriorRefundState: r.Status, State: "succeeded", Source: "internal"}); e != nil {
		t.Fatal(e)
	}
	var sourceValid, resultValid bool
	if e = tx.QueryRow(ctx, "SELECT valid FROM order_income_source_evidence WHERE order_id=$1", o.ID).Scan(&sourceValid); e != nil || sourceValid {
		t.Fatal("original source treated merchant debit alone as refund", sourceValid, e)
	}
	if e = tx.QueryRow(ctx, "SELECT valid FROM provider_refund_result_invariants WHERE refund_id=$1", r.ID).Scan(&resultValid); e != nil || resultValid {
		t.Fatal("signed terminal result accepted wrong counterparty", resultValid, e)
	}
	_, e = tx.Exec(ctx, "SET CONSTRAINTS provider_refund_result_requires_ledger IMMEDIATE")
	if e == nil {
		t.Fatal("59 accepted signed wrong-counterparty evidence")
	}
	if commit := tx.Commit(ctx); commit == nil {
		t.Fatal("wrong refund counterparty committed")
	}
	var commitError *pgconn.PgError
	if !errors.As(e, &commitError) || commitError.Code != "P0001" || commitError.Message != "missing new refund terminal evidence" && commitError.Message != "invalid terminal refund evidence" {
		t.Fatal("wrong counterparty failed for an unrelated reason", e)
	}
	t.Log("signed wrong-counterparty refund rejected by terminal evidence guard; original source was independently invalid")
	got, e := s.Refund(ctx, 409, r.RefundNo)
	if e != nil || got.Status != "pending" {
		t.Fatal("failed terminal transaction changed refund state", got, e)
	}
	payer, e := s.Balance(ctx, 409)
	if e != nil || payer.Available != 80 || payer.Reserved != 20 {
		t.Fatal("wrong refund changed payer balance after rollback", payer, e)
	}
	payee, e := s.Balance(ctx, 410)
	if e != nil || payee.Available != 0 {
		t.Fatal("wrong counterparty retained rolled-back funds", payee, e)
	}
	if _, e = s.FreezeOrderIncome(ctx, 409, o.TradeNo, "pending-refund-still-blocks", incomeTestActor); e == nil {
		t.Fatal("rolled-back terminal result erased pending refund")
	}
	if e = s.ResolveRefund(ctx, r.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	if h, e := s.FreezeOrderIncome(ctx, 409, o.TradeNo, "failed-refund-restores-income", incomeTestActor); e != nil || h.Amount != 100 {
		t.Fatal("valid failure did not restore original source", h, e)
	}
}

// Supply all new keyed parents while deliberately retaining the original bad
// journal. These fault fixtures must fail for the missing/wrong ledger evidence,
// not merely because a newer schema introduced another required proof row.
func seedIncomeTestRefundProof(ctx context.Context, s *Store, tx pgx.Tx, o Order, r Refund, consumeJournal int64) error {
	var exists bool
	if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM refund_fundings WHERE refund_id=$1)", r.ID).Scan(&exists); e != nil {
		return e
	}
	var f refundFundingRecord
	var e error
	if exists {
		f, e = s.scanRefundFunding(ctx, tx, r)
		if e != nil {
			return e
		}
	} else {
		f = refundFundingRecord{RefundID: r.ID, OrderID: r.OrderID, MerchantID: r.MerchantID, Merchant: r.MerchantAmount, Available: r.MerchantAmount, Fee: r.FeeAmount}
		if e = tx.QueryRow(ctx, "SELECT id,transaction_timestamp() FROM accounts WHERE owner_id=$1 AND kind='reserved' AND scope_id=0", r.MerchantID).Scan(&f.AccountID, &f.At); e != nil {
			return e
		}
		f.Proof, e = s.refundFundingProof(f, r, o)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO refund_fundings(refund_id,order_id,merchant_id,merchant_amount,hold_amount,available_amount,fee_amount,reserve_account_id,request_hash,created_at) VALUES($1,$2,$3,$4,0,$4,$5,$6,$7,$8)`, r.ID, r.OrderID, r.MerchantID, r.MerchantAmount, r.FeeAmount, f.AccountID, f.Proof, f.At); e != nil {
			return e
		}
		if e = incomeAudit(ctx, tx, "refund-fundings", r.ID, "reserve", nil, map[string]string{"fixture": "signed missing journal"}); e != nil {
			return e
		}
	}
	z := refundFundingResult{RefundID: r.ID, State: "succeeded", Request: f.Proof, ConsumeJournal: consumeJournal}
	if e = tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&z.At); e != nil {
		return e
	}
	z.Proof, e = s.refundFundingResultProof(z)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO refund_funding_resolutions(refund_id,state,consume_journal_id,request_hash,result_hash,created_at) VALUES($1,'succeeded',NULLIF($2::bigint,0),$3,$4,$5)`, r.ID, z.ConsumeJournal, z.Request, z.Proof, z.At); e != nil {
		return e
	}
	return incomeAudit(ctx, tx, "refund-fundings", r.ID, "succeeded", nil, map[string]string{"fixture": "signed invalid counterparty"})
}
