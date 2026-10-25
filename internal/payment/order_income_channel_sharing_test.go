package payment

import (
	"context"
	"encoding/json"
	"testing"
)

func completeIncomeChannelSharing(t *testing.T, f *sharingExecutionFixture, o Order, no string) {
	t.Helper()
	f.Phase.Store(1)
	v, e := f.Flow.SubmitChannelSharing(context.Background(), o.MerchantID, o.TradeNo, no, channelSharingActor)
	if e != nil || !v.Completed || !v.FundsReleased || v.ReleasedAmount != 80 {
		t.Fatal("local original PS did not release its retained income", v, e)
	}
}

func TestOrderIncomeChannelSharingCompletedRefundAndIndependentSourceSnapshots(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	o := f.paid(t, "income-sharing-original", false)
	completeIncomeChannelSharing(t, f, o, "income-sharing-ps")
	other := f.paid(t, "income-sharing-other", false)
	completeIncomeChannelSharing(t, f, other, "income-sharing-other-ps")
	r, e := f.Store.ReserveRefund(ctx, 751, o.OutTradeNo, "income-sharing-refund", 10)
	if e != nil {
		t.Fatal("trusted sharing completion must allow a merchant-funded refund", e)
	}
	if e = f.Store.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if e = f.Store.CreateMerchant(ctx, Merchant{ID: 752, Name: "internal-recipient", APIKey: "local-recipient"}); e != nil {
		t.Fatal(e)
	}
	createRule := func(rate string) int64 {
		t.Helper()
		var id int64
		if e := f.Store.pool.QueryRow(ctx, "INSERT INTO backoffice_records(resource,owner_id,body) VALUES('profitsharing-rules',751,jsonb_build_object('enabled',true,'rate_bps',$1::text,'recipient_id','752')) RETURNING id", rate).Scan(&id); e != nil {
			t.Fatal(e)
		}
		return id
	}
	if _, e = f.Store.ExecuteProfitRule(ctx, createRule("3000"), o.TradeNo, "merchant:751"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Store.ExecuteProfitRule(ctx, createRule("5000"), o.TradeNo, "merchant:751"); e == nil {
		t.Fatal("a second rule borrowed another order's wallet money beyond this order's retained income")
	}
	amount, e := f.Store.OrderIncomeFreezable(ctx, 751, o.TradeNo)
	if e != nil || amount != 40 {
		t.Fatal("original income must deduct PS20, refund10 and internal allocation30", amount, e)
	}
	h, e := f.Store.FreezeOrderIncome(ctx, 751, o.TradeNo, "income-sharing-freeze", IncomeHoldActor{ID: 1, Name: "local-admin", Class: "super"})
	if e != nil || h.Amount != 40 || h.RefundMerchantAmount != 10 {
		t.Fatal("PS/internal money was mixed into the old refund field", h, e)
	}
	raw, e := json.Marshal(h)
	if e != nil {
		t.Fatal(e)
	}
	var fields map[string]any
	if e = json.Unmarshal(raw, &fields); e != nil {
		t.Fatal(e)
	}
	if fields["channel_sharing_amount"] != "20" || fields["internal_allocated_amount"] != "30" {
		t.Fatal("new freeze omitted its explicit original source snapshots", string(raw))
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 80 {
		t.Fatal("freeze borrowed or blocked the other original order's money", b, e)
	}
}

func TestOrderIncomeChannelSharingT1ProtectsReleasedTodayIncomeButNotActiveReserveTwice(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	old := f.paid(t, "income-sharing-matured", false)
	completeIncomeChannelSharing(t, f, old, "income-sharing-matured-ps")
	if _, e := f.Store.pool.Exec(ctx, "UPDATE orders SET paid_at=statement_timestamp()-interval '2 days' WHERE id=$1", old.ID); e != nil {
		t.Fatal(e)
	}
	today := f.paid(t, "income-sharing-today", false)
	if _, e := f.Store.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settle_type","value":"1"}')`); e != nil {
		t.Fatal(e)
	}
	active, e := f.Store.FundsAvailability(ctx, 751)
	if e != nil || active.Available != 80 || active.TodayGross != 0 {
		t.Fatal("active dedicated reserve double-blocked old income", active, e)
	}
	completeIncomeChannelSharing(t, f, today, "income-sharing-today-ps")
	final, e := f.Store.FundsAvailability(ctx, 751)
	if e != nil || final.LedgerAvailable != 160 || final.TodayGross != 80 || final.Blocked != 80 || final.Available != 80 {
		t.Fatal("completed release bypassed T1 or blocked old income twice", final, e)
	}
}
