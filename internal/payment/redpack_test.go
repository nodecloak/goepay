package payment

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
)

func redpackInput(no string, amount int64) RedpackInput {
	return RedpackInput{MerchantID: 501, ChannelID: 7, ActorID: 501, OutBizNo: no, Type: "wxpay", Remark: "恭喜发财", Role: "merchant", Amount: amount}
}

func TestRedpackCreateIsIdempotentChecksFundsAndReservesNothing(t *testing.T) {
	s, _, _ := transferFixture(t)
	ctx := context.Background()
	r, err := s.CreateRedpack(ctx, redpackInput("2026102800000000001", 100))
	if err != nil || r.Status != "claimable" || r.ExpiresAt.Sub(r.CreatedAt) < RedpackLifetime-1e9 {
		t.Fatal("red packet not created claimable for 24h", r, err)
	}
	again, err := s.CreateRedpack(ctx, redpackInput("2026102800000000001", 100))
	if err != nil || again.ID != r.ID {
		t.Fatal("same 交易号 was not idempotent", again, err)
	}
	if _, err = s.CreateRedpack(ctx, redpackInput("2026102800000000001", 200)); !errors.Is(err, ErrConflict) {
		t.Fatal("changed red packet under the same 交易号 accepted", err)
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 1000 || b.Reserved != 0 {
		t.Fatal("creating a red packet moved balance", b)
	}
	// 1000 available, 10% fee: 950 would cost 1045.
	if _, err = s.CreateRedpack(ctx, redpackInput("2026102800000000002", 950)); !errors.Is(err, ErrInsufficient) {
		t.Fatal("red packet above available balance accepted", err)
	}
	for _, bad := range []RedpackInput{
		{MerchantID: 501, ChannelID: 7, ActorID: 501, OutBizNo: "123", Type: "wxpay", Role: "merchant", Amount: 1},
		{MerchantID: 501, ChannelID: 7, ActorID: 501, OutBizNo: "2026102800000000003", Type: "bank", Role: "merchant", Amount: 1},
		{MerchantID: 501, ChannelID: 7, ActorID: 501, OutBizNo: "2026102800000000004", Type: "wxpay", Role: "merchant", Amount: 1, Remark: "一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十一二三"},
	} {
		if _, err = s.CreateRedpack(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Error("invalid red packet accepted", bad, err)
		}
	}
}

func TestRedpackClaimPaysOnceToOneClaimer(t *testing.T) {
	s, flow, p := transferFixture(t)
	ctx := context.Background()
	r, err := s.CreateRedpack(ctx, redpackInput("2026102800000000011", 100))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			account := "claimer-a"
			if i%2 == 1 {
				account = "claimer-b"
			}
			_, _, errs[i] = flow.ClaimRedpack(ctx, r.ID, account)
		}(i)
	}
	wg.Wait()
	if p.transfers.Load() != 1 {
		t.Fatalf("red packet sent %d transfers", p.transfers.Load())
	}
	p.seenMu.Lock()
	sent := p.seen[0]
	p.seenMu.Unlock()
	if sent.Amount != 100 || sent.Remark != "恭喜发财" || (sent.Account != "claimer-a" && sent.Account != "claimer-b") {
		t.Fatal("claim transfer wrong", sent)
	}
	loser := "claimer-a"
	if sent.Account == "claimer-a" {
		loser = "claimer-b"
	}
	if _, _, err = flow.ClaimRedpack(ctx, r.ID, loser); !errors.Is(err, ErrRedpackClaimed) {
		t.Fatal("a second claimer was not refused", err)
	}
	// PROCESSING keeps the packet claiming; the query resolves it.
	got, _, err := flow.QueryRedpack(ctx, r.ID)
	if err != nil || got.Status != "succeeded" || p.transfers.Load() != 1 {
		t.Fatal("query did not settle the claim without resending", got, err, p.transfers.Load())
	}
	tr, err := s.Transfer(ctx, 501, RedpackTransferNo(r.ID))
	if err != nil || tr.CostAmount != 110 {
		t.Fatal("claim transfer did not reserve amount+fee", tr, err)
	}
}

func TestRedpackUnknownClaimNeverResends(t *testing.T) {
	s, flow, p := transferFixture(t)
	ctx := context.Background()
	p.unknown = true
	r, err := s.CreateRedpack(ctx, redpackInput("2026102800000000021", 100))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = flow.ClaimRedpack(ctx, r.ID, "claimer-a"); err == nil {
		t.Fatal("unknown claim outcome reported success")
	}
	if _, _, err = flow.ClaimRedpack(ctx, r.ID, "claimer-a"); err == nil && p.transfers.Load() != 1 {
		t.Fatal("repeat claim resent the transfer")
	}
	if p.transfers.Load() != 1 {
		t.Fatalf("unknown claim resent: %d", p.transfers.Load())
	}
	got, _ := s.Redpack(ctx, r.ID)
	if got.Status != "claiming" {
		t.Fatal("unknown claim did not stay claiming", got.Status)
	}
}

func TestRedpackExpiryStopsClaimsAndHoldsNoFunds(t *testing.T) {
	s, flow, p := transferFixture(t)
	ctx := context.Background()
	r, err := s.CreateRedpack(ctx, redpackInput("2026102800000000031", 100))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`ALTER TABLE redpacks DISABLE TRIGGER redpacks_original`, `UPDATE redpacks SET expires_at=clock_timestamp()-interval '1 minute' WHERE id=` + strconv.FormatInt(r.ID, 10), `ALTER TABLE redpacks ENABLE TRIGGER redpacks_original`} {
		if _, err = s.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = flow.ClaimRedpack(ctx, r.ID, "claimer-a"); !errors.Is(err, ErrRedpackExpired) || p.transfers.Load() != 0 {
		t.Fatal("expired packet was claimable", err)
	}
	n, err := s.ExpireRedpacks(ctx)
	got, _ := s.Redpack(ctx, r.ID)
	if err != nil || n != 1 || got.Status != "expired" {
		t.Fatal("expiry job did not expire the packet", n, err, got.Status)
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 1000 || b.Reserved != 0 {
		t.Fatal("expired packet left funds held", b)
	}
}
