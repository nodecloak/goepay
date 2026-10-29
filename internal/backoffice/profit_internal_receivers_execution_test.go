package backoffice

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"goepay/internal/payment"
)

// 站内分账 multi-receiver EXECUTION (a4 N3): each receiver leg pays its own
// floor(order.amount * rate / 10000) share as an immutable journal, the whole
// set is one idempotent transaction (replay pays nothing twice), the legs never
// sum above the retained source, and a mid-leg failure moves no money.
func TestProfitRuleMultiReceiverExecutionPerLegRoundingReplayAndAtomicFailure(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, id := range []int64{103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113} {
		if e := s.store.CreateMerchant(ctx, payment.Merchant{ID: id, Name: fmt.Sprintf("r%d", id), APIKey: fmt.Sprintf("r%d-secret-long", id)}); e != nil {
			t.Fatal(e)
		}
	}
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "m1"}

	// --- 1 receiver: legacy single leg still pays (NULL recipient row). ---
	single, e := s.Create(ctx, merchant, "profitsharing-rules", input(`{"name":"one","receivers":[{"recipient_id":"103","rate_bps":"2000"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	o1 := paidOrder(t, s, 101, "multi-one", 1000)
	v1, e := s.store.ExecuteProfitRule(ctx, single.ID, o1.TradeNo, "merchant:101")
	if e != nil || v1.Amount != 200 { // floor(1000*2000/10000)=200
		t.Fatalf("single leg: %+v %v", v1, e)
	}
	// Disable the single rule so it does not consume the owner's budget below.
	if _, e = s.Update(ctx, merchant, "profitsharing-rules", single.ID, input(`{"enabled":false}`)); e != nil {
		t.Fatal(e)
	}

	// --- 2 receivers: per-leg floor rounding + two immutable journals. ---
	// Order amount 999: leg 3000bps -> floor(999*0.3)=299, leg 2001bps ->
	// floor(999*0.2001)=199 (199.8999 floored); remainder dropped, not moved.
	rule2, e := s.Create(ctx, merchant, "profitsharing-rules", input(`{"name":"two","receivers":[{"recipient_id":"104","rate_bps":"3000"},{"recipient_id":"105","rate_bps":"2001"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	o2 := paidOrder(t, s, 101, "multi-two", 999)
	before := count(t, s, "SELECT (SELECT count(*) FROM order_profit_allocations),(SELECT count(*) FROM ledger_requests)")
	first, e := s.store.ExecuteProfitRule(ctx, rule2.ID, o2.TradeNo, "merchant:101")
	if e != nil {
		t.Fatal("two legs", e)
	}
	after := count(t, s, "SELECT (SELECT count(*) FROM order_profit_allocations),(SELECT count(*) FROM ledger_requests)")
	if after[0]-before[0] != 2 || after[1]-before[1] != 2 {
		t.Fatalf("expected 2 new allocations + 2 requests, got +%d/+%d", after[0]-before[0], after[1]-before[1])
	}
	// Per-leg amounts (floor, remainder dropped).
	amts := amounts(t, s, o2.ID, rule2.ID)
	if len(amts) != 2 || amts[104] != 299 || amts[105] != 199 {
		t.Fatalf("per-leg floor rounding wrong: %v", amts)
	}
	// The returned request is the first leg.
	if first.Amount != 299 && first.Amount != 199 {
		t.Fatalf("returned request not a leg: %+v", first)
	}
	// Reserved funds equal the legs' sum (498) plus the earlier single-rule
	// allocation (200 on o1, still reserved after its rule was disabled) = 698.
	ownerBal, _ := s.store.Balance(ctx, 101)
	if ownerBal.Reserved != 698 {
		t.Fatalf("owner reserved=%d want 698 (200 + 299 + 199)", ownerBal.Reserved)
	}

	// --- Replay pays nothing twice. ---
	replay, e := s.store.ExecuteProfitRule(ctx, rule2.ID, o2.TradeNo, "merchant:101")
	if e != nil {
		t.Fatal("replay", e)
	}
	again := count(t, s, "SELECT (SELECT count(*) FROM order_profit_allocations),(SELECT count(*) FROM ledger_requests)")
	if again[0] != after[0] || again[1] != after[1] {
		t.Fatalf("replay created new rows: %v vs %v", again, after)
	}
	if replay.ID != first.ID {
		t.Fatalf("replay returned a different request %d vs %d", replay.ID, first.ID)
	}

	// --- Ratio sum exactly 100% across 2 legs on a fresh order. ---
	full, e := s.Create(ctx, Principal{Role: "merchant", SubjectID: 102, Username: "m2"}, "profitsharing-rules", input(`{"name":"full","receivers":[{"recipient_id":"106","rate_bps":"7000"},{"recipient_id":"107","rate_bps":"3000"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	oFull := paidOrder(t, s, 102, "multi-full", 1000)
	if _, e = s.store.ExecuteProfitRule(ctx, full.ID, oFull.TradeNo, "merchant:102"); e != nil {
		t.Fatal("100% sum", e)
	}
	fullAmts := amounts(t, s, oFull.ID, full.ID)
	if fullAmts[106] != 700 || fullAmts[107] != 300 || fullAmts[106]+fullAmts[107] != 1000 {
		t.Fatalf("100%% legs wrong: %v", fullAmts)
	}

	// --- 10 receivers, each 1000bps => Σ=100%, ten legs of 100 on amount 1000. ---
	var legs []string
	for _, rid := range []int64{103, 104, 105, 106, 107, 108, 109, 110, 111, 112} {
		legs = append(legs, fmt.Sprintf(`{"recipient_id":"%d","rate_bps":"1000"}`, rid))
	}
	ten, e := s.Create(ctx, Principal{Role: "merchant", SubjectID: 113, Username: "m-ten"}, "profitsharing-rules", input(`{"name":"ten","receivers":[`+strings.Join(legs, ",")+`]}`))
	if e != nil {
		t.Fatal("ten create", e)
	}
	o10 := paidOrder(t, s, 113, "multi-ten", 1000)
	b10 := count(t, s, "SELECT (SELECT count(*) FROM order_profit_allocations),(SELECT count(*) FROM ledger_requests)")
	if _, e = s.store.ExecuteProfitRule(ctx, ten.ID, o10.TradeNo, "merchant:113"); e != nil {
		t.Fatal("ten exec", e)
	}
	a10 := count(t, s, "SELECT (SELECT count(*) FROM order_profit_allocations),(SELECT count(*) FROM ledger_requests)")
	if a10[0]-b10[0] != 10 || a10[1]-b10[1] != 10 {
		t.Fatalf("ten legs: +%d allocations +%d requests", a10[0]-b10[0], a10[1]-b10[1])
	}
	ownerTen, _ := s.store.Balance(ctx, 113)
	if ownerTen.Reserved != 1000 {
		t.Fatalf("ten legs reserved=%d want 1000", ownerTen.Reserved)
	}

	// --- One leg fails -> no partial money movement (atomic rollback). ---
	// Pre-insert a conflicting request for the SECOND leg's external_no with a
	// different amount so requestFundsTx returns ErrConflict on leg 2; the whole
	// transaction (including the already-posted first leg) must roll back.
	fail, e := s.Create(ctx, Principal{Role: "merchant", SubjectID: 104, Username: "m4"}, "profitsharing-rules", input(`{"name":"fail","receivers":[{"recipient_id":"105","rate_bps":"3000"},{"recipient_id":"106","rate_bps":"2000"}]}`))
	if e != nil {
		t.Fatal("fail rule create", e)
	}
	oFail := paidOrder(t, s, 104, "multi-fail", 1000)
	// Collide leg 2 (recipient 106): same kind+merchant+external_no, wrong amount.
	collide := fmt.Sprintf("order:%s:rule:%d:recipient:106", oFail.TradeNo, fail.ID)
	if _, e = s.pool.Exec(ctx, "INSERT INTO ledger_requests(kind,merchant_id,recipient_id,external_no,amount,status)VALUES('profitsharing',104,106,$1,1,'pending')", collide); e != nil {
		t.Fatal("seed colliding request", e)
	}
	bf := count(t, s, "SELECT (SELECT count(*) FROM order_profit_allocations WHERE order_id="+itoa(oFail.ID)+"),(SELECT count(*) FROM journals)")
	if _, e = s.store.ExecuteProfitRule(ctx, fail.ID, oFail.TradeNo, "merchant:104"); e == nil {
		t.Fatal("expected a leg conflict to fail the whole execution")
	}
	af := count(t, s, "SELECT (SELECT count(*) FROM order_profit_allocations WHERE order_id="+itoa(oFail.ID)+"),(SELECT count(*) FROM journals)")
	if af[0] != bf[0] || af[1] != bf[1] {
		t.Fatalf("failed execution left partial movement: allocations %d->%d journals %d->%d", bf[0], af[0], bf[1], af[1])
	}
	failBal, _ := s.store.Balance(ctx, 104)
	if failBal.Reserved != 0 {
		t.Fatalf("failed execution reserved funds: %+v", failBal)
	}
}

func paidOrder(t *testing.T, s *Service, mid int64, no string, amount int64) payment.Order {
	t.Helper()
	ctx := context.Background()
	o, e := s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: mid, OutTradeNo: no, Name: "goods", Amount: amount})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.store.MarkPaid(ctx, o.TradeNo, no+"-paid"); e != nil {
		t.Fatal(e)
	}
	return o
}

func count(t *testing.T, s *Service, q string) []int64 {
	t.Helper()
	var a, b int64
	if e := s.pool.QueryRow(context.Background(), q).Scan(&a, &b); e != nil {
		t.Fatal(e)
	}
	return []int64{a, b}
}

func amounts(t *testing.T, s *Service, orderID, ruleID int64) map[int64]int64 {
	t.Helper()
	rows, e := s.pool.Query(context.Background(), "SELECT r.recipient_id,a.amount FROM order_profit_allocations a JOIN ledger_requests r ON r.id=a.request_id WHERE a.order_id=$1 AND a.rule_id=$2", orderID, ruleID)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var recipient, amt int64
		if e = rows.Scan(&recipient, &amt); e != nil {
			t.Fatal(e)
		}
		out[recipient] = amt
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	return out
}
