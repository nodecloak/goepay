package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func setGroupSettlementFee155(t *testing.T, s *Store, id int64, rate string) {
	t.Helper()
	if _, e := s.pool.Exec(context.Background(), `UPDATE backoffice_records SET body=jsonb_set(body,'{settle_rate}',to_jsonb($2::text)) WHERE resource='groups' AND id=$1`, id, rate); e != nil {
		t.Fatal(e)
	}
}

func TestOriginalGroupSettlementFeeQuoteManualRequestZeroAndHistoricalReplay(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	transferSetting(t, s, "settlement_fee_rate_bps", "100")
	setGroupSettlementFee155(t, s, 0, "2.55")
	q, e := s.SettlementQuote(ctx, 681, 1000)
	if e != nil || q.Terms == nil || q.Terms.FeeAmount != 26 || q.Terms.NetAmount != 974 {
		t.Fatal("manual quote ignored original group fee", q, e)
	}
	expected := &SettlementExpectation{FeeAmount: 26, NetAmount: 974}
	r, e := s.RequestFundsExpected(ctx, "settlements", 681, 0, "original-group-fee155", 1000, "merchant:681", expected)
	if e != nil || r.Settlement == nil || r.Settlement.FeeAmount != 26 {
		t.Fatal("quote and new request differ", r, e)
	}
	setGroupSettlementFee155(t, s, 0, "0")
	q, e = s.SettlementQuote(ctx, 681, 1000)
	if e != nil || q.Terms == nil || q.Terms.FeeAmount != 0 {
		t.Fatal("explicit group zero inherited a fee", q, e)
	}
	if replay, e := s.RequestFundsExpected(ctx, "settlements", 681, 0, "original-group-fee155", 1000, "merchant:681", expected); e != nil || replay.ID != r.ID || replay.Settlement.FeeAmount != 26 {
		t.Fatal("group fee change rewrote original", replay, e)
	}
	if free, e := s.RequestFunds(ctx, "settlements", 681, 0, "new-free155", 1000, "merchant:681"); e != nil || free.Settlement == nil || free.Settlement.FeeAmount != 0 {
		t.Fatal("new explicit free group charged fee", free, e)
	}
	setGroupSettlementFee155(t, s, 0, "")
	q, e = s.SettlementQuote(ctx, 681, 1000)
	if e != nil || q.Terms == nil || q.Terms.FeeAmount != 10 {
		t.Fatal("empty group rate did not inherit", q, e)
	}
}

func TestOriginalGroupSettlementFeeEffectiveMembershipAndFeeCaps(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	transferSetting(t, s, "settlement_fee_rate_bps", "100")
	transferSetting(t, s, "settlement_fee_minimum_cents", "5")
	transferSetting(t, s, "settlement_fee_maximum_cents", "20")
	setGroupSettlementFee155(t, s, 0, "0")
	var vip int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"settlement fee VIP155","enabled":true,"settle_rate":"3"}') RETURNING id`).Scan(&vip); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(681,$1,clock_timestamp()+interval '1 day')`, vip); e != nil {
		t.Fatal(e)
	}
	for _, c := range []struct{ amount, fee int64 }{{100, 5}, {1000, 20}} {
		q, e := s.SettlementQuote(ctx, 681, c.amount)
		if e != nil || q.Terms == nil || q.Terms.FeeAmount != c.fee || q.Policy.FeeRateBPS != 300 {
			t.Fatal("effective group or global caps lost", q, c, e)
		}
	}
	if _, e := s.pool.Exec(ctx, `UPDATE memberships SET expires_at=clock_timestamp()-interval '1 second' WHERE merchant_id=681`); e != nil {
		t.Fatal(e)
	}
	q, e := s.SettlementQuote(ctx, 681, 100)
	if e != nil || q.Terms == nil || q.Terms.FeeAmount != 0 {
		t.Fatal("expired group fee or global minimum survived explicit zero", q, e)
	}
}

func TestOriginalGroupSettlementFeeCannotSilentlyChangeAfterAccountLockExpiry(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	transferSetting(t, s, "settlement_fee_rate_bps", "100")
	setGroupSettlementFee155(t, s, 0, "1")
	var vip int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"fee expiry155","enabled":true,"settle_rate":"3"}') RETURNING id`).Scan(&vip); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(681,$1,clock_timestamp()+interval '2 seconds')`, vip); e != nil {
		t.Fatal(e)
	}
	block, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(context.Background())
	if _, e = block.Exec(ctx, `SELECT id FROM accounts WHERE owner_id=681 AND kind='available' FOR UPDATE`); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, e := s.RequestFunds(ctx, "settlements", 681, 0, "fee-expiry155", 1000, "merchant:681")
		done <- e
	}()
	for {
		var waiting bool
		if e = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM accounts%FOR UPDATE%')`).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case early := <-done:
			t.Fatal("account wait not reached", early)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, e = s.pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM expires_at-clock_timestamp()))::double precision+0.05) FROM memberships WHERE merchant_id=681`); e != nil {
		t.Fatal(e)
	}
	if e = block.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, ErrConflict) && !errors.Is(e, ErrState) {
			t.Fatal("expired fee selection silently committed", e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if b, e := s.Balance(ctx, 681); e != nil || b.Available != 10000 || b.Reserved != 0 {
		t.Fatal("failed selection moved funds", b, e)
	}
}

func TestOriginalGroupSettlementFeeScheduledIntentUsesAuthorizedMerchantRate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 901)
	transferSetting(t, s, "settlement_fee_rate_bps", "100")
	setGroupSettlementFee155(t, s, 0, "2.55")
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 901, OutTradeNo: "fee-plan-income155", Name: "local", Amount: 2000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "fee-plan-receipt155"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), p.ID, p.Revision, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	run, e := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.GenerateSettlementPlanRun(ctx, run.ID); e != nil {
		t.Fatal(e)
	}
	v, e := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if e != nil || len(v.Items) != 1 || v.Items[0].Gross != 1000 || v.Items[0].Fee != 26 || v.Items[0].Net != 974 || v.CreatedCount != 1 {
		t.Fatal("scheduled intent used global rather than original merchant group fee", v, e)
	}
}

func TestOriginalGroupSettlementFeeScheduledMerchantsKeepDistinctAuthorizedRates(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_ = settlementPlanReady(t, s, 901)
	_ = settlementPlanReady(t, s, 902)
	setGroupSettlementFee155(t, s, 0, "2.55")
	var gid int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"second scheduled fee155","enabled":true,"settle_rate":"4"}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=902`, gid); e != nil {
		t.Fatal(e)
	}
	for _, mid := range []int64{901, 902} {
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "fee-multi155", Amount: 2000})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.MarkPaid(ctx, o.TradeNo, fmt.Sprintf("fee-multi155-%d", mid)); e != nil {
			t.Fatal(e)
		}
	}
	in := settlementPlanInputFor(901)
	in.MerchantIDs = []int64{902, 901}
	p, e := s.CreateSettlementPlan(ctx, in, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	limit := settlementPlanAuthorizationInput(t, s)
	limit.MaxRunGross = 2000
	limit.MaxDayGross = 2000
	limit.MaxDayItems = 2
	if _, e = s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), p.ID, p.Revision, limit, settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	run, e := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.GenerateSettlementPlanRun(ctx, run.ID); e != nil {
		t.Fatal(e)
	}
	v, e := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if e != nil || v.CreatedCount != 2 || len(v.Items) != 2 {
		t.Fatal("mixed group plan did not preserve both intents", v, e)
	}
	fees := map[int64]int64{}
	for _, item := range v.Items {
		fees[item.MerchantID] = item.Fee
		if item.Gross != 1000 || item.Net != 1000-item.Fee {
			t.Fatal("mixed plan amount drift", item)
		}
	}
	if fees[901] != 26 || fees[902] != 40 {
		t.Fatal("one merchant's group priced another intent", fees)
	}
	for _, mid := range []int64{901, 902} {
		b, e := s.Balance(ctx, mid)
		if e != nil || b.Available != 1000 || b.Reserved != 1000 {
			t.Fatal("mixed fee changed gross reservation", mid, b, e)
		}
	}
}

func TestOriginalGroupSettlementFeeChangedGroupCannotRepriceExistingPlanAuthority(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 901)
	setGroupSettlementFee155(t, s, 0, "2.55")
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 901, OutTradeNo: "fee-changed-plan155", Amount: 2000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "fee-changed-plan155"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AuthorizeSettlementPlan(settlementPlanGuard(t, s), p.ID, p.Revision, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	run, e := s.CreateSettlementPlanRun(ctx, p.ID, 1, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	setGroupSettlementFee155(t, s, 0, "4")
	if e = s.GenerateSettlementPlanRun(ctx, run.ID); e != nil {
		t.Fatal(e)
	}
	v, e := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if e != nil || v.CreatedCount != 0 || v.BlockedCount != 1 || len(v.Items) != 1 || v.Items[0].Reason != "scope_changed" {
		t.Fatal("old authority silently repriced", v, e)
	}
	if b, e := s.Balance(ctx, 901); e != nil || b.Available != 2000 || b.Reserved != 0 {
		t.Fatal("invalidated group fee authority reserved funds", b, e)
	}
}
