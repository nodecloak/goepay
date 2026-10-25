package payment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func incomeSharingRule(t *testing.T, s *Store, rate int64) int64 {
	t.Helper()
	ctx := context.Background()
	if _, e := s.Merchant(ctx, 752); errors.Is(e, ErrNotFound) {
		if e = s.CreateMerchant(ctx, Merchant{ID: 752, Name: "local income recipient", APIKey: "income-local-only"}); e != nil {
			t.Fatal(e)
		}
	} else if e != nil {
		t.Fatal(e)
	}
	var id int64
	if e := s.pool.QueryRow(ctx, "INSERT INTO backoffice_records(resource,owner_id,body) VALUES('profitsharing-rules',751,jsonb_build_object('enabled',true,'rate_bps',$1::bigint::text,'recipient_id','752')) RETURNING id", rate).Scan(&id); e != nil {
		t.Fatal(e)
	}
	return id
}

func TestOrderIncomeChannelSharingRejectedAllocationRestoresOnlyProvedOriginalBudget(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	o := f.paid(t, "income-allocated", false)
	completeIncomeChannelSharing(t, f, o, "income-allocated-ps")
	request, e := f.Store.ExecuteProfitRule(ctx, incomeSharingRule(t, f.Store, 6000), o.TradeNo, "merchant:751")
	if e != nil {
		t.Fatal(e)
	}
	// Changing a decision plus adding its event cannot release money or restore the
	// order's source budget without the corresponding original request journal.
	tx, e := f.Store.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE ledger_requests SET status='rejected' WHERE id=$1", request.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "INSERT INTO ledger_request_events(request_id,actor,status) VALUES($1,'admin:1','rejected')", request.ID); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e == nil {
		t.Fatal("a rejected status without its return journal restored the original budget")
	}
	amount, e := f.Store.OrderIncomeFreezable(ctx, 751, o.TradeNo)
	if e != nil || amount != 20 {
		t.Fatal(amount, e)
	}
	if _, e = f.Store.ReviewFundsRequest(ctx, request.ID, false, "admin:1"); e != nil {
		t.Fatal(e)
	}
	amount, e = f.Store.OrderIncomeFreezable(ctx, 751, o.TradeNo)
	if e != nil || amount != 80 {
		t.Fatal("proved rejected allocation was still consumed", amount, e)
	}
	if _, e = f.Store.ExecuteProfitRule(ctx, incomeSharingRule(t, f.Store, 7000), o.TradeNo, "merchant:751"); e != nil {
		t.Fatal(e)
	}
	amount, e = f.Store.OrderIncomeFreezable(ctx, 751, o.TradeNo)
	if e != nil || amount != 10 {
		t.Fatal(amount, e)
	}
}

func TestOrderIncomeChannelSharingConcurrentRulesCannotBorrowAnotherOrder(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	o := f.paid(t, "income-rule-race", false)
	completeIncomeChannelSharing(t, f, o, "income-rule-race-ps")
	other := f.paid(t, "income-rule-race-other", false)
	completeIncomeChannelSharing(t, f, other, "income-rule-race-other-ps")
	ids := []int64{incomeSharingRule(t, f.Store, 6000), incomeSharingRule(t, f.Store, 6000)}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			<-start
			_, e := f.Store.ExecuteProfitRule(ctx, id, o.TradeNo, "merchant:751")
			results <- e
		}(id)
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if !errors.Is(e, ErrInsufficient) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal("concurrent rules reused the original source budget", success)
	}
	amount, e := f.Store.OrderIncomeFreezable(ctx, 751, o.TradeNo)
	if e != nil || amount != 20 {
		t.Fatal(amount, e)
	}
}

func TestOrderIncomeChannelSharingMerchantFundedFullRefundNeverCreatesRetainedIncome(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	o := f.paid(t, "income-full-refund", false)
	completeIncomeChannelSharing(t, f, o, "income-full-refund-ps")
	other := f.paid(t, "income-full-refund-funding", false)
	completeIncomeChannelSharing(t, f, other, "income-full-refund-funding-ps")
	r, e := f.Store.ReserveRefund(ctx, 751, o.OutTradeNo, "income-full-refund", 100)
	if e != nil {
		t.Fatal("merchant own wallet must fund ordinary refund after trusted sharing completion", e)
	}
	if e = f.Store.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Store.OrderIncomeFreezable(ctx, 751, o.TradeNo); !errors.Is(e, ErrState) {
		t.Fatal("full refund retained an original order budget", e)
	}
	if _, e = f.Store.ExecuteProfitRule(ctx, incomeSharingRule(t, f.Store, 1000), o.TradeNo, "merchant:751"); !errors.Is(e, ErrState) {
		t.Fatal("other wallet funds recreated fully consumed order income", e)
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 60 {
		t.Fatal(b, e)
	}
}

func TestOrderIncomeChannelSharingSnapshotAndHoldingLedgerCannotBeForged(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	o := f.paid(t, "income-hold-proof", false)
	completeIncomeChannelSharing(t, f, o, "income-hold-proof-ps")
	// A v1 marker cannot opt a new hold out of the new original-source evidence.
	for _, version := range []int{1, 2} {
		_, e := f.Store.pool.Exec(ctx, "INSERT INTO order_income_holds(order_id,merchant_id,request_no,merchant_net,refund_merchant_amount,channel_sharing_amount,amount,evidence_version) VALUES($1,751,$2,100,20,0,80,$3)", o.ID, fmt.Sprintf("forged-v%d", version), version)
		if e == nil {
			t.Fatal("PS outflow could be disguised as a refund or old evidence", version)
		}
	}
	if _, e := f.Store.pool.Exec(ctx, `CREATE FUNCTION reject_sharing_income_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation='freeze-income' THEN RAISE EXCEPTION 'local actor audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_sharing_income_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_sharing_income_audit()`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Store.FreezeOrderIncome(ctx, 751, o.TradeNo, "audit-unavailable", incomeTestActor); e == nil {
		t.Fatal("freeze committed without its actor audit")
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 80 {
		t.Fatal(b, e)
	}
	if _, e = f.Store.pool.Exec(ctx, "DROP TRIGGER reject_sharing_income_audit ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	h, e := f.Store.FreezeOrderIncome(ctx, 751, o.TradeNo, "trusted-hold", incomeTestActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Store.pool.Exec(ctx, "UPDATE order_income_holds SET channel_sharing_amount=19,refund_merchant_amount=1 WHERE id=$1", h.ID); e == nil {
		t.Fatal("immutable source components changed")
	}
	// A balanced transfer from this hold's dedicated account must still be rejected:
	// only the original hold's own release operation may consume these funds.
	tx, e := f.Store.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	var sourceAccount, targetAccount int64
	if e = tx.QueryRow(ctx, "SELECT f.account_id,a.id FROM order_income_hold_fundings f JOIN accounts a ON(a.owner_id,a.kind,a.scope_id)=(751,'available',0) WHERE f.hold_id=$1", h.ID).Scan(&sourceAccount, &targetAccount); e != nil {
		t.Fatal(e)
	}
	if e = postByAccountIDs(ctx, tx, "local:unrelated:hold-drain", "local invalid holding spend", sourceAccount, targetAccount, 1); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e == nil {
		t.Fatal("an unrelated balanced journal drained a still-held order")
	}
	if _, e = f.Store.pool.Exec(ctx, "UPDATE order_income_holds SET state='released',released_at=statement_timestamp() WHERE id=$1", h.ID); e == nil {
		t.Fatal("hold released without original release journal and actor proof")
	}
	if _, e = f.Store.UnfreezeOrderIncome(ctx, 751, o.TradeNo, h.ID, incomeTestActor); e != nil {
		t.Fatal(e)
	}
	b, e = f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 80 {
		t.Fatal(b, e)
	}
}

func TestOrderIncomeChannelSharingT1KeepsGrossFeeAndOrdinaryRefundPolicy(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	old := f.paid(t, "income-t1-fee-old", false)
	completeIncomeChannelSharing(t, f, old, "income-t1-fee-old-ps")
	if _, e := f.Store.pool.Exec(ctx, "UPDATE orders SET paid_at=statement_timestamp()-interval '2 days' WHERE id=$1", old.ID); e != nil {
		t.Fatal(e)
	}
	var group int64
	if e := f.Store.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"local fee","enabled":true,"rate_bps":"1000"}') RETURNING id`).Scan(&group); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Store.pool.Exec(ctx, "UPDATE merchants SET profile=jsonb_build_object('group_id',$1::bigint::text) WHERE id=751", group); e != nil {
		t.Fatal(e)
	}
	for _, pair := range [][2]string{{"payment_fee_enabled", "true"}, {"refund_fee_payer", "platform"}} {
		if _, e := f.Store.pool.Exec(ctx, "INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))", pair[0], pair[1]); e != nil {
			t.Fatal(e)
		}
	}
	today := f.paid(t, "income-t1-fee-today", false)
	if today.MerchantAmount != 90 || today.PlatformFee != 10 {
		t.Fatal("fee snapshot absent", today)
	}
	f.Phase.Store(1)
	v, e := f.Flow.SubmitChannelSharing(ctx, 751, today.TradeNo, "income-t1-fee-today-ps", channelSharingActor)
	if e != nil || !v.FundsReleased || v.ReleasedAmount != 70 {
		t.Fatal(v, e)
	}
	if _, e = f.Store.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settle_type","value":"1"}')`); e != nil {
		t.Fatal(e)
	}
	check := func(ledger, gross, available int64) {
		t.Helper()
		a, e := f.Store.FundsAvailability(ctx, 751)
		if e != nil || a.LedgerAvailable != ledger || a.TodayGross != gross || a.Available != available {
			t.Fatal(a, e, ledger, gross, available)
		}
	}
	check(150, 80, 70) // gross100 minus successful PS20; platform fee10 remains conservatively blocked.
	r, e := f.Store.ReserveRefund(ctx, 751, today.OutTradeNo, "income-t1-partial", 20)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Store.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	check(130, 80, 50) // a partial ordinary refund does not alter the original gross policy.
	r, e = f.Store.ReserveRefund(ctx, 751, today.OutTradeNo, "income-t1-final", 80)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Store.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	check(60, 0, 60) // only a fully refunded gross order leaves today's restriction.
}

func TestOrderIncomeChannelSharingOnlyCredibleSuccessfulReturnRestoresSourceAndTodayRestriction(t *testing.T) {
	f := newSharingReturnFixture(t)
	ctx := context.Background()
	var trade string
	if e := f.Store.pool.QueryRow(ctx, "SELECT trade_no FROM orders WHERE id=$1", f.Sharing.OrderID).Scan(&trade); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Store.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settle_type","value":"1"}')`); e != nil {
		t.Fatal(e)
	}
	check := func(amount int64) {
		t.Helper()
		n, e := f.Store.OrderIncomeFreezable(ctx, 751, trade)
		if e != nil || n != amount {
			t.Fatal(n, e, amount)
		}
		v, e := f.Store.FundsAvailability(ctx, 751)
		if e != nil || v.LedgerAvailable != amount || v.TodayGross != amount || v.Available != 0 {
			t.Fatal("returned today's money bypassed T1", v, e, amount)
		}
	}
	check(80)
	r, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, f.Sharing.Items[0].ReceiverID, "income-budget-return", 12, channelSharingActor)
	if e != nil || r.State != "pending" {
		t.Fatal(r, e)
	}
	check(80) // acceptance does not restore the source budget or ledger.
	f.Phase.Store(1)
	r, e = f.Flow.QueryChannelSharingReturn(ctx, 751, "income-budget-return", channelSharingActor)
	if e != nil || r.State != "succeeded" {
		t.Fatal(r, e)
	}
	check(92)
	h, e := f.Store.FreezeOrderIncome(ctx, 751, trade, "income-returned-source", incomeTestActor)
	if e != nil || h.Amount != 92 || h.ChannelSharingAmount != 8 || h.RefundMerchantAmount != 0 || h.InternalAllocatedAmount != 0 {
		t.Fatal("successful PR did not reduce only net PS outflow", h, e)
	}
	if _, e = f.Store.UnfreezeOrderIncome(ctx, 751, trade, h.ID, incomeTestActor); e != nil {
		t.Fatal(e)
	}
	check(92)
}
