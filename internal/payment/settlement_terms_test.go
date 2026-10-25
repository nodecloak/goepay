package payment

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSettlementTermsRoundingCapsAndZeroRate(t *testing.T) {
	for _, tc := range []struct {
		p           SettlementPolicy
		amount, fee int64
	}{
		{SettlementPolicy{FeeMinimumCents: 9}, 100, 0},
		{SettlementPolicy{FeeRateBPS: 250}, 100, 3},
		{SettlementPolicy{FeeRateBPS: 250, FeeMinimumCents: 4}, 100, 4},
		{SettlementPolicy{FeeRateBPS: 250, FeeMaximumCents: 10}, 10000, 10},
		{SettlementPolicy{FeeRateBPS: 1}, math.MaxInt64, math.MaxInt64/10000 + (math.MaxInt64%10000+5000)/10000},
	} {
		terms, e := tc.p.Quote(tc.amount)
		if e != nil || terms.FeeAmount != tc.fee || terms.NetAmount != tc.amount-tc.fee {
			t.Fatalf("quote %+v/%d %+v %v", tc.p, tc.amount, terms, e)
		}
	}
	for _, p := range []SettlementPolicy{{FeeRateBPS: 10001}, {FeeRateBPS: 10000}, {FeeMinimumCents: 20, FeeMaximumCents: 10}, {MinimumAmount: 20, MaximumAmount: 10}, {RetainedCents: -1}, {DailyLimit: -1}} {
		if _, e := p.Quote(10); !errors.Is(e, ErrInvalid) {
			t.Fatalf("invalid/zero-net policy allowed %+v", p)
		}
	}
}

func TestSettlementTermsRejectsChangedFeeAfterUserConfirmation(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	transferSetting(t, s, "settlement_fee_rate_bps", "250")
	q, e := s.SettlementQuote(ctx, 681, 100)
	if e != nil {
		t.Fatal(e)
	}
	expected := &SettlementExpectation{FeeAmount: q.Terms.FeeAmount, NetAmount: q.Terms.NetAmount}
	transferSetting(t, s, "settlement_fee_rate_bps", "500")
	if _, e = s.RequestFundsExpected(ctx, "settlements", 681, 0, "changed-fee", 100, "merchant:681", expected); !errors.Is(e, ErrConflict) {
		t.Fatal("changed fee silently accepted", e)
	}
	balance, e := s.Balance(ctx, 681)
	if e != nil || balance.Available != 10000 || balance.Reserved != 0 {
		t.Fatal("fee conflict reserved funds", balance, e)
	}
}

func TestSettlementTermsInvalidPolicyAndSnapshotFailureRollback(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	transferSetting(t, s, "settlement_fee_rate_bps", "invalid")
	if _, e := s.RequestFunds(ctx, "settlements", 681, 0, "invalid", 100, "merchant:681"); !errors.Is(e, ErrInvalid) {
		t.Fatal("invalid policy failed open", e)
	}
	transferSetting(t, s, "settlement_fee_rate_bps", "250")
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION reject_settlement_terms_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected terms failure'; END $$; CREATE TRIGGER reject_settlement_terms_test BEFORE INSERT ON settlement_request_terms FOR EACH ROW EXECUTE FUNCTION reject_settlement_terms_test()`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.RequestFunds(ctx, "settlements", 681, 0, "atomic", 100, "merchant:681"); e == nil {
		t.Fatal("snapshot failure accepted request")
	}
	b, e := s.Balance(ctx, 681)
	if e != nil || b.Available != 10000 || b.Reserved != 0 {
		t.Fatal("snapshot failure left reservation", b, e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM ledger_requests WHERE merchant_id=681").Scan(&count); e != nil || count != 0 {
		t.Fatal("snapshot failure left request", count, e)
	}
}

func TestSettlementTermsQuoteScopeAndDailyRejectionCount(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	transferSetting(t, s, "settlement_daily_limit", "1")
	transferSetting(t, s, "settlement_fee_rate_bps", "250")
	q, e := s.SettlementQuote(ctx, 681, 100)
	if e != nil || q.Terms == nil || q.Terms.FeeAmount != 3 || !q.CanApply || q.DailyUsed != 0 {
		t.Fatal(q, e)
	}
	r, e := s.RequestFunds(ctx, "settlements", 681, 0, "one-day", 100, "merchant:681")
	if e != nil || r.Settlement == nil || r.Settlement.NetAmount != 97 {
		t.Fatal(r, e)
	}
	if _, e = s.ReviewFundsRequest(ctx, r.ID, false, "admin:1"); e != nil {
		t.Fatal(e)
	}
	q, e = s.SettlementQuote(ctx, 681, 100)
	if e != nil || q.CanApply || q.DailyUsed != 1 {
		t.Fatal("rejected request removed daily count", q, e)
	}
	if _, e = s.RequestFunds(ctx, "settlements", 681, 0, "second-day", 100, "admin:1"); !errors.Is(e, ErrState) {
		t.Fatal("admin bypassed daily limit", e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE ledger_requests SET created_at=((clock_timestamp() AT TIME ZONE 'Asia/Shanghai')::date::timestamp AT TIME ZONE 'Asia/Shanghai')-interval '1 microsecond' WHERE id=$1", r.ID); e != nil {
		t.Fatal(e)
	}
	q, e = s.SettlementQuote(ctx, 681, 100)
	if e != nil || !q.CanApply || q.DailyUsed != 0 {
		t.Fatal("prior UTC8 day still counted", q, e)
	}
}

func TestSettlementTermsRetainedFundsAndDailyLimit(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	transferSetting(t, s, "settlement_retained_cents", "9000")
	if _, e := s.RequestFunds(ctx, "settlements", 681, 0, "too-much-after-retain", 1001, "merchant:681"); !errors.Is(e, ErrInsufficient) {
		t.Fatalf("retained funds ignored: %v", e)
	}
	transferSetting(t, s, "settlement_retained_cents", "0")
	transferSetting(t, s, "settlement_daily_limit", "3")
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := s.RequestFunds(ctx, "settlements", 681, 0, fmt.Sprintf("daily-%d", i), 100, "merchant:681")
			if e == nil {
				accepted.Add(1)
			} else if !errors.Is(e, ErrState) {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 3 {
		t.Fatalf("daily limit ignored: accepted %d", accepted.Load())
	}
	var count int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM settlement_request_terms").Scan(&count); e != nil || count != 3 {
		t.Fatalf("atomic settlement terms missing %d %v", count, e)
	}
}

func TestSettlementTermsExactFeeSnapshotAndHistoricalReplay(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	transferSetting(t, s, "settlement_fee_rate_bps", "250")
	transferSetting(t, s, "settlement_fee_minimum_cents", "3")
	transferSetting(t, s, "settlement_fee_maximum_cents", "10")
	request, e := s.RequestFunds(ctx, "settlements", 681, 0, "snapshot", 123, "merchant:681")
	if e != nil {
		t.Fatal(e)
	}
	var gross, fee, net int64
	if e = s.pool.QueryRow(ctx, "SELECT gross_amount,fee_amount,net_amount FROM settlement_request_terms WHERE request_id=$1", request.ID).Scan(&gross, &fee, &net); e != nil || gross != 123 || fee != 3 || net != 120 {
		t.Fatalf("wrong exact fee snapshot %d/%d/%d %v", gross, fee, net, e)
	}
	transferSetting(t, s, "settlement_fee_rate_bps", "10000")
	transferSetting(t, s, "settlement_minimum_cents", "1000")
	replay, e := s.RequestFunds(ctx, "settlements", 681, 0, "snapshot", 123, "admin:1")
	if e != nil || replay.ID != request.ID {
		t.Fatal("policy recalculated historical intent", e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE settlement_request_terms SET fee_amount=0,net_amount=gross_amount WHERE request_id=$1", request.ID); e == nil {
		t.Fatal("mutable financial terms")
	}
	if _, e = s.ReviewFundsRequest(ctx, request.ID, false, "admin:1"); e != nil {
		t.Fatal(e)
	}
	balance, e := s.Balance(ctx, 681)
	if e != nil || balance.Available != 10000 || balance.Reserved != 0 {
		t.Fatal("reject did not restore original gross", balance, e)
	}
}
