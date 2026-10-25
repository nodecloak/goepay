package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"sync"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

func referralClawbackFixture(t *testing.T, payer string) (*Store, ProviderFlow, *operationsProvider, Order) {
	t.Helper()
	s := referralClawbackSetup(t, payer)
	ctx := context.Background()
	o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 902, OutTradeNo: "reward-payment", Name: "local original", Type: "wxpay", Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	p := &operationsProvider{}
	f := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://local.example.invalid"}
	if _, err = f.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Query(ctx, 902, o.TradeNo); err != nil {
		t.Fatal(err)
	}
	return s, f, p, o
}
func referralClawbackSetup(t *testing.T, payer string) *Store {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	referralBoundMerchants(t, s, 901, 902)
	var group int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"local fee group","enabled":true,"rate_bps":"1000"}') RETURNING id`).Scan(&group); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_build_object('group_id',$2::bigint::text) WHERE id=$1`, 902, group); err != nil {
		t.Fatal(err)
	}
	for _, p := range [][2]string{{"payment_fee_enabled", "true"}, {"refund_fee_payer", payer}} {
		if _, err := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))`, p[0], p[1]); err != nil {
			t.Fatal(err)
		}
	}
	yes, rate := true, int64(5000)
	if _, err := s.SetReferralPolicy(ctx, "global", 0, 0, ReferralPolicyInput{Enabled: &yes, RateBPS: &rate, Mode: "immediate", Basis: "fee", RefundPolicy: referralRefundPolicy}, TransferBatchActor{Role: "admin", Class: "super", ID: 1, Name: "local admin"}); err != nil {
		t.Fatal(err)
	}
	return s
}

func referralClawbackBalances(t *testing.T, s *Store, fees, hold, escrow, promoter int64) {
	t.Helper()
	ctx := context.Background()
	var f, h, e int64
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees'),(SELECT balance FROM accounts WHERE owner_id=0 AND kind='fee_reserved'),COALESCE((SELECT sum(balance) FROM accounts WHERE kind='referral_clawback_reserve'),0)`).Scan(&f, &h, &e); err != nil {
		t.Fatal(err)
	}
	b, err := s.Balance(ctx, 901)
	if err != nil || f != fees || h != hold || e != escrow || b.Available != promoter {
		t.Fatalf("fees=%d hold=%d escrow=%d promoter=%+v wanted %d/%d/%d/%d: %v", f, h, e, b, fees, hold, escrow, promoter, err)
	}
}

func TestReferralClawbackUnknownEscrowFailedRetryAndSuccessfulPartial(t *testing.T) {
	s, flow, p, o := referralClawbackFixture(t, "platform")
	ctx := context.Background()
	referralClawbackBalances(t, s, 5, 0, 0, 5)
	p.unknown = true
	r, err := flow.Refund(ctx, 902, o.TradeNo, "first", 98)
	if err == nil || r.FeeAmount != 8 {
		t.Fatalf("lost refund %+v %v", r, err)
	}
	referralClawbackBalances(t, s, 0, 8, 2, 0)
	if _, err = s.ReserveRefund(ctx, 902, o.OutTradeNo, "bypass", 1); !errors.Is(err, ErrState) {
		t.Fatalf("unknown reward reused %v", err)
	}
	if _, err = flow.Refund(ctx, 902, o.TradeNo, "first", 98); err == nil || p.refunds.Load() != 1 {
		t.Fatalf("unknown resubmitted %v calls%d", err, p.refunds.Load())
	}
	if err = s.ResolveRefund(ctx, r.ID, "failed"); err != nil {
		t.Fatal(err)
	}
	referralClawbackBalances(t, s, 5, 0, 0, 5)
	// A new number is allowed only after the original reward has been fully
	// restored by a keyed failed resolution. Any successful partial voids R.
	r, err = s.ReserveRefund(ctx, 902, o.OutTradeNo, "second", 1)
	if err != nil {
		t.Fatal(err)
	}
	referralClawbackBalances(t, s, 5, 0, 5, 0)
	if err = s.ResolveRefund(ctx, r.ID, "succeeded"); err != nil {
		t.Fatal(err)
	}
	referralClawbackBalances(t, s, 10, 0, 0, 0)
	if err = s.ResolveRefund(ctx, r.ID, "succeeded"); err != nil {
		t.Fatal(err)
	}
	last, err := s.ReserveRefund(ctx, 902, o.OutTradeNo, "remaining", 99)
	if err != nil || last.FeeAmount != 10 {
		t.Fatalf("remaining %+v %v", last, err)
	}
	if err = s.ResolveRefund(ctx, last.ID, "succeeded"); err != nil {
		t.Fatal(err)
	}
	referralClawbackBalances(t, s, 0, 0, 0, 0)
	var count int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM referral_refund_clawbacks").Scan(&count); err != nil || count != 2 {
		t.Fatalf("award clawed twice %d %v", count, err)
	}
}

func TestReferralClawbackEveryFeeBoundaryBalances(t *testing.T) {
	for _, g := range []int64{0, 3, 5, 8, 10} {
		t.Run(fmt.Sprint(g), func(t *testing.T) {
			s, _, _, o := referralClawbackFixture(t, "platform")
			ctx := context.Background()
			amount := int64(1)
			if g > 0 {
				amount = 90 + g
			}
			r, err := s.ReserveRefund(ctx, 902, o.OutTradeNo, "boundary", amount)
			if err != nil {
				t.Fatal(err)
			}
			k := int64(0)
			if g > 5 {
				k = g - 5
			}
			referralClawbackBalances(t, s, 5+k-g, g, 5-k, 0)
			if err = s.ResolveRefund(ctx, r.ID, "unknown"); err != nil {
				t.Fatal(err)
			}
			referralClawbackBalances(t, s, 5+k-g, g, 5-k, 0)
			if err = s.ResolveRefund(ctx, r.ID, "failed"); err != nil {
				t.Fatal(err)
			}
			referralClawbackBalances(t, s, 5, 0, 0, 5)
		})
	}
}

func TestReferralClawbackSpentRewardStopsBeforeProviderIOAndConcurrentClaim(t *testing.T) {
	s, flow, p, o := referralClawbackFixture(t, "merchant")
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, "CREATE TABLE local_referral_grants(action_key text PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	guarded := WithFundsActionGuard(ctx, func(ctx context.Context, tx pgx.Tx, kind, key string) error {
		if kind != "refund" {
			return ErrInvalid
		}
		_, err := tx.Exec(ctx, "INSERT INTO local_referral_grants(action_key) VALUES($1)", key)
		return err
	})
	if _, err := s.ReserveTransfer(ctx, 901, "promoter-spent", 5); err != nil {
		t.Fatal(err)
	}
	if _, err := flow.Refund(guarded, 902, o.TradeNo, "cannot-claw", 1); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("spent reward accepted %v", err)
	}
	if p.refunds.Load() != 0 {
		t.Fatal("called provider before reward recovered")
	}
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM local_referral_grants").Scan(&n); err != nil || n != 0 {
		t.Fatal("insufficient reward consumed authorization", n, err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM refunds").Scan(&n); err != nil || n != 0 {
		t.Fatal("failed clawback retained intent", n, err)
	}
	var transfer int64
	if err := s.pool.QueryRow(ctx, "SELECT id FROM transfers WHERE transfer_no='promoter-spent'").Scan(&transfer); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveTransfer(ctx, transfer, "failed"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = s.ReserveRefund(guarded, 902, o.OutTradeNo, "same", 1) }()
	}
	wg.Wait()
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM referral_refund_clawbacks").Scan(&n); err != nil || n != 1 {
		t.Fatalf("duplicate reward reserve %d %v", n, err)
	}
	referralClawbackBalances(t, s, 5, 0, 5, 0)
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM local_referral_grants").Scan(&n); err != nil || n != 1 {
		t.Fatal("replay consumed repeated authorization", n, err)
	}
}
