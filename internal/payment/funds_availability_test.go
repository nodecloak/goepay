package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	adapter "goepay/internal/adapters/payment"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestT1RejectsEveryOutgoingReservationBeforeFreezing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, id := range []int64{601, 602} {
		if e := s.CreateMerchant(ctx, Merchant{ID: id, Name: "t1", APIKey: "test-only"}); e != nil {
			t.Fatal(e)
		}
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 601, OutTradeNo: "today", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "t1-paid"); e != nil {
		t.Fatal(e)
	}
	transferSetting(t, s, "settle_type", "1")
	if _, e = s.ReserveTransfer(ctx, 601, "external", 1); !errors.Is(e, ErrInsufficient) {
		t.Fatalf("today's receipt escaped T+1 through external transfer: %v", e)
	}
	for _, kind := range []string{"settlements", "internal-transfers", "profitsharing"} {
		recipient := int64(602)
		if kind == "settlements" {
			recipient = 0
		}
		if _, e = s.RequestFunds(ctx, kind, 601, recipient, kind, 1, "merchant:601"); !errors.Is(e, ErrInsufficient) {
			t.Fatalf("today's receipt escaped T+1 through %s: %v", kind, e)
		}
	}
	var ruleID int64
	if e = s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,owner_id,body) VALUES('profitsharing-rules',601,'{"enabled":true,"recipient_id":"602","rate_bps":"10"}') RETURNING id`).Scan(&ruleID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ExecuteProfitRule(ctx, ruleID, o.TradeNo, "merchant:601"); !errors.Is(e, ErrInsufficient) {
		t.Fatalf("order profit rule bypassed T+1: %v", e)
	}
	b, e := s.Balance(ctx, 601)
	if e != nil || b.Available != 1000 || b.Reserved != 0 {
		t.Fatalf("rejected request froze money: %+v %v", b, e)
	}
	var records int
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM transfers)+(SELECT count(*) FROM ledger_requests)").Scan(&records); e != nil || records != 0 {
		t.Fatalf("rejected request persisted: %d %v", records, e)
	}
}

func t1PaidOrder(t *testing.T, s *Store, mid, amount int64, no string, paidAt time.Time, recharge bool) Order {
	t.Helper()
	ctx := context.Background()
	in := CreateOrderInput{MerchantID: mid, OutTradeNo: no, Amount: amount}
	var o Order
	var e error
	if recharge {
		o, e = s.CreateRechargeOrder(ctx, in, "local-t1-test")
	} else {
		o, e = s.CreateOrder(ctx, in)
	}
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "t1-"+no); e != nil {
		t.Fatal(e)
	}
	// Explicit historical paid times represent imported provider confirmation data.
	if _, e = s.pool.Exec(ctx, "UPDATE orders SET paid_at=$2 WHERE id=$1", o.ID, paidAt); e != nil {
		t.Fatal(e)
	}
	return o
}

func TestT1UTC8BoundaryRechargeGrossAndRefundRules(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 603, 1000, "platform")
	transferSetting(t, s, "settle_type", "1")
	at := time.Date(2026, 10, 23, 2, 0, 0, 0, time.UTC)
	midnight := time.Date(2026, 10, 22, 16, 0, 0, 0, time.UTC)
	t1PaidOrder(t, s, 603, 1000, "previous-day", midnight.Add(-time.Nanosecond*1000), false)
	today := t1PaidOrder(t, s, 603, 100, "midnight", midnight, false)
	t1PaidOrder(t, s, 603, 200, "recharge", at, true)
	check := func(ledger, gross, available int64) {
		t.Helper()
		v, e := s.readFundsAvailability(ctx, s.pool, 603, &at)
		if e != nil || v.LedgerAvailable != ledger || v.TodayGross != gross || v.Available != available || v.Blocked != ledger-available || v.SettleType != 1 {
			t.Fatalf("availability %+v error %v want ledger=%d gross=%d available=%d", v, e, ledger, gross, available)
		}
	}
	check(1170, 100, 1070) // today net90 is restricted by gross100; recharge net180 is immediately usable.
	nextDay := midnight.Add(24 * time.Hour)
	if v, e := s.readFundsAvailability(ctx, s.pool, 603, &nextDay); e != nil || v.Available != 1170 || v.TodayGross != 0 {
		t.Fatalf("next Beijing day did not mature receipts %+v %v", v, e)
	}
	r1, e := s.ReserveRefund(ctx, 603, today.OutTradeNo, "partial", 40)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r1.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	check(1130, 100, 1030) // allocation uses merchant funds first; fee reimbursement is in the final refund.
	r2, e := s.ReserveRefund(ctx, 603, today.OutTradeNo, "remaining", 60)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r2.ID, "unknown"); e != nil {
		t.Fatal(e)
	}
	check(1080, 100, 980) // an unknown refund never removes the day's restriction.
	if e = s.ResolveRefund(ctx, r2.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	check(1080, 0, 1080)
	transferSetting(t, s, "settle_type", "0")
	if v, e := s.readFundsAvailability(ctx, s.pool, 603, &at); e != nil || v.Available != 1080 || v.Blocked != 0 || v.SettleType != 0 {
		t.Fatalf("T+0 %+v %v", v, e)
	}
}

func TestT1DefaultsZeroClampsAndRejectsInvalidPolicy(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 604, Name: "default", APIKey: "test-only"}); e != nil {
		t.Fatal(e)
	}
	t1PaidOrder(t, s, 604, 100, "today", time.Now(), false)
	v, e := s.FundsAvailability(ctx, 604)
	if e != nil || v.Available != 100 || v.Blocked != 0 || v.SettleType != 0 {
		t.Fatalf("default %+v %v", v, e)
	}
	tx, e := s.ReserveTransfer(ctx, 604, "before-policy", 10)
	if e != nil {
		t.Fatal(e)
	}
	transferSetting(t, s, "settle_type", "1")
	if replay, e := s.ReserveTransfer(ctx, 604, "before-policy", 10); e != nil || replay.ID != tx.ID {
		t.Fatalf("policy change broke replay %+v %v", replay, e)
	}
	v, e = s.FundsAvailability(ctx, 604)
	if e != nil || v.LedgerAvailable != 90 || v.Available != 0 || v.Blocked != 90 {
		t.Fatalf("negative availability %+v %v", v, e)
	}
	// Imported order metadata may exceed any individual account. SUM(bigint) is
	// numeric and the read must saturate safely instead of overflowing int64.
	for i := 0; i < 2; i++ {
		if _, e = s.pool.Exec(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,status,paid_at) VALUES($1,604,$1,'historical','alipay',$2,'paid',now())`, fmt.Sprintf("metadata-only-%d", i), int64(math.MaxInt64)); e != nil {
			t.Fatal(e)
		}
	}
	v, e = s.FundsAvailability(ctx, 604)
	if e != nil || v.TodayGross != math.MaxInt64 || v.Available != 0 || v.Blocked != 90 {
		t.Fatalf("gross overflow %+v %v", v, e)
	}
	transferSetting(t, s, "settle_type", "invalid")
	if _, e = s.ReserveTransfer(ctx, 604, "invalid-policy", 1); !errors.Is(e, ErrInvalid) {
		t.Fatalf("invalid policy allowed debit %v", e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','null') WHERE resource='settings' AND body->>'key'='settle_type'`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReserveTransfer(ctx, 604, "null-policy", 1); !errors.Is(e, ErrInvalid) {
		t.Fatalf("null policy allowed debit %v", e)
	}
}

func TestT1ConcurrentRefundAndMixedOutgoingsCannotSpendToday(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, id := range []int64{605, 606} {
		if e := s.CreateMerchant(ctx, Merchant{ID: id, Name: "concurrent", APIKey: "test-only"}); e != nil {
			t.Fatal(e)
		}
	}
	t1PaidOrder(t, s, 605, 1000, "matured", time.Now().Add(-48*time.Hour), false)
	today := t1PaidOrder(t, s, 605, 1000, "today", time.Now(), false)
	transferSetting(t, s, "settle_type", "1")
	var total atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var e error
			if i%4 == 0 {
				_, e = s.ReserveTransfer(ctx, 605, fmt.Sprintf("out-%d", i), 50)
			} else {
				kind, recipient := []string{"settlements", "internal-transfers", "profitsharing"}[(i-1)%3], int64(606)
				if kind == "settlements" {
					recipient = 0
				}
				_, e = s.RequestFunds(ctx, kind, 605, recipient, fmt.Sprintf("out-%d", i), 50, "merchant:605")
			}
			if e == nil {
				total.Add(50)
			} else if !errors.Is(e, ErrInsufficient) {
				t.Error(e)
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		r, e := s.ReserveRefund(ctx, 605, today.OutTradeNo, "full", 1000)
		if e != nil {
			t.Error(e)
			return
		}
		if e = s.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
			t.Error(e)
		}
	}()
	wg.Wait()
	b, e := s.Balance(ctx, 605)
	// While the refund is reserved but not yet confirmed, today's gross still
	// blocks outgoing funds. Some one-shot requests may legitimately fail then;
	// completion releases that policy restriction, it does not retry those calls.
	spent := total.Load()
	if e != nil || spent < 0 || spent > 1000 || b.Available != 1000-spent || b.Reserved != spent {
		t.Fatalf("mixed debits spent restricted income total=%d balance=%+v err=%v", total.Load(), b, e)
	}
	if remaining := 1000 - spent; remaining > 0 {
		if _, e = s.ReserveTransfer(ctx, 605, "after-refund-confirmed", remaining); e != nil {
			t.Fatal("matured remainder still unavailable after confirmed full refund", e)
		}
	}
	b, e = s.Balance(ctx, 605)
	if e != nil || b.Available != 0 || b.Reserved != 1000 {
		t.Fatalf("post-confirmation funds %+v %v", b, e)
	}
	if report, e := s.CheckLedger(ctx); e != nil || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
		t.Fatalf("concurrent accounting %+v %v", report, e)
	}
}

func TestT1PendingFullRefundCanTemporarilyBlockMaturedFunds(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 610, Name: "refund phases", APIKey: "test-only"}); e != nil {
		t.Fatal(e)
	}
	t1PaidOrder(t, s, 610, 1000, "matured", time.Now().Add(-48*time.Hour), false)
	today := t1PaidOrder(t, s, 610, 1000, "today", time.Now(), false)
	transferSetting(t, s, "settle_type", "1")
	refund, e := s.ReserveRefund(ctx, 610, today.OutTradeNo, "refund-in-flight", 1000)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReserveTransfer(ctx, 610, "during-reservation", 50); !errors.Is(e, ErrInsufficient) {
		t.Fatal("pending refund removed conservative gross restriction", e)
	}
	if e = s.ResolveRefund(ctx, refund.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReserveTransfer(ctx, 610, "after-confirmation", 1000); e != nil {
		t.Fatal("confirmed full refund did not release gross restriction", e)
	}
}

func TestT1TransferFeeCountsBeforeProviderSubmission(t *testing.T) {
	s, flow, upstream := transferFixture(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, "UPDATE orders SET paid_at=now()-interval '2 days' WHERE merchant_id=501"); e != nil {
		t.Fatal(e)
	}
	t1PaidOrder(t, s, 501, 1000, "restricted", time.Now(), false)
	transferSetting(t, s, "settle_type", "1")
	if _, e := flow.Transfer(ctx, 501, 7, "fee-exceeds-matured", transferRequestForT1(1000)); !errors.Is(e, ErrInsufficient) {
		t.Fatalf("fee bypassed cap %v", e)
	}
	if upstream.transfers.Load() != 0 {
		t.Fatal("rejected transfer called upstream")
	}
	if _, e := flow.Transfer(ctx, 501, 7, "within-matured", transferRequestForT1(900)); e != nil {
		t.Fatal(e)
	}
	v, e := s.FundsAvailability(ctx, 501)
	if e != nil || v.Available != 10 || v.LedgerAvailable != 1010 || upstream.transfers.Load() != 1 {
		t.Fatalf("fee accounting %+v %v", v, e)
	}
}

func transferRequestForT1(amount int64) adapter.TransferRequest {
	return adapter.TransferRequest{Type: "wxpay", Account: "test-only-recipient", Amount: amount, Remark: "test"}
}

func TestT1ReservationRefreshesSnapshotAfterAccountLockWait(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := s.CreateMerchant(ctx, Merchant{ID: 607, Name: "lock-snapshot", APIKey: "test-only"}); e != nil {
		t.Fatal(e)
	}
	t1PaidOrder(t, s, 607, 1000, "matured", time.Now().Add(-48*time.Hour), false)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 607, OutTradeNo: "concurrent-paid", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	transferSetting(t, s, "settle_type", "1")
	writer, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer writer.Rollback(context.Background())
	// Hold canonical clearing -> available locks as payment posting does.
	if e = post(ctx, writer, "t1-snapshot-payment", "payment", 0, "clearing", 607, "available", 1000); e != nil {
		t.Fatal(e)
	}
	if _, e = writer.Exec(ctx, "UPDATE orders SET status='paid',paid_at=now() WHERE id=$1", o.ID); e != nil {
		t.Fatal(e)
	}
	reader, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer reader.Rollback(context.Background())
	var pid int
	if e = reader.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- s.requireOutgoingFunds(ctx, reader, 607, 1001) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting bool
		if e = s.pool.QueryRow(ctx, "SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1", pid).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reservation never waited for the payment account lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e = writer.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-done; !errors.Is(e, ErrInsufficient) {
		t.Fatalf("used aggregate snapshot preceding account lock: %v", e)
	}
	v, e := s.FundsAvailability(ctx, 607)
	if e != nil || v.Available != 1000 || v.LedgerAvailable != 2000 || v.TodayGross != 1000 {
		t.Fatalf("concurrent paid snapshot %+v %v", v, e)
	}
}

func TestT1PaidTimestampFollowsAccountLockWait(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := s.CreateMerchant(ctx, Merchant{ID: 608, Name: "paid-clock", APIKey: "test-only"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 608, OutTradeNo: "paid-clock", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	cfg := s.pool.Config()
	app := "t1-paid-" + cfg.ConnConfig.RuntimeParams["search_path"]
	cfg.ConnConfig.RuntimeParams["application_name"] = app
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	callback, e := NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	locker, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer locker.Rollback(context.Background())
	var account int64
	if e = locker.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=608 AND kind='available' FOR UPDATE").Scan(&account); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- callback.MarkPaid(ctx, o.TradeNo, "t1-clock") }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting bool
		if e = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')", app).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("callback never waited for the account lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var releaseAfter time.Time
	if e = locker.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&releaseAfter); e != nil {
		t.Fatal(e)
	}
	if e = locker.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	var paidAt time.Time
	if e = s.pool.QueryRow(ctx, "SELECT paid_at FROM orders WHERE id=$1", o.ID).Scan(&paidAt); e != nil {
		t.Fatal(e)
	}
	if paidAt.Before(releaseAfter) {
		t.Fatalf("payment time %s precedes releasing the account lock at %s; a midnight wait would mature today's income early", paidAt, releaseAfter)
	}
}
