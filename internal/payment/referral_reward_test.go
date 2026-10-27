package payment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
)

var referralPolicyAdmin = TransferBatchActor{Role: "admin", Class: "super", ID: 1, Name: "local-referral-policy"}

func referralTestPolicy(enabled *bool, rate *int64) ReferralPolicyInput {
	return ReferralPolicyInput{Enabled: enabled, RateBPS: rate, Mode: "immediate", Basis: "fee", RefundPolicy: referralRefundPolicy}
}

func referralFeeSetup(t *testing.T, s *Store, mid, rate int64, payer string) int64 {
	t.Helper()
	ctx := context.Background()
	var gid int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups',jsonb_build_object('name','local referral fee','enabled',true,'rate_bps',$1::bigint::text)) RETURNING id`, rate).Scan(&gid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE merchants SET profile=jsonb_build_object('group_id',$2::bigint::text) WHERE id=$1", mid, gid); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"payment_fee_enabled", "true"}, {"refund_fee_payer", payer}} {
		if _, err := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text)) ON CONFLICT DO NOTHING`, pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	return gid
}

func referralRewardFixture(t *testing.T) (*Store, int64, int64, int64) {
	t.Helper()
	s := testStore(t)
	referralBoundMerchants(t, s, 1810, 1811)
	g := referralFeeSetup(t, s, 1811, 1000, "platform")
	enabled, rate := true, int64(5000)
	if _, err := s.SetReferralPolicy(context.Background(), "global", 0, 0, referralTestPolicy(&enabled, &rate), referralPolicyAdmin); err != nil {
		t.Fatal(err)
	}
	return s, 1810, 1811, g
}

func referralCreateTestOrder(t *testing.T, s *Store, mid, amount int64, no string) Order {
	t.Helper()
	o, err := s.CreateOrder(context.Background(), CreateOrderInput{MerchantID: mid, OutTradeNo: no, Amount: amount, Name: "local referral item", Type: "wxpay"})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func referralPayTestOrder(t *testing.T, s *Store, o Order) {
	t.Helper()
	ctx := context.Background()
	if _, _, err := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); err != nil {
		t.Fatal(err)
	}
	if err := s.markPaid(ctx, o.TradeNo, "local-referral-paid:"+o.TradeNo, true, PaidReceipt{RealMoney: o.Amount, ChannelID: 7, ProviderTradeNo: "LOCAL_" + o.TradeNo}); err != nil {
		t.Fatal(err)
	}
}

func TestReferralOriginalFeeAwardSnapshotAndConcurrentCallbacks(t *testing.T) {
	s, promoter, mid, _ := referralRewardFixture(t)
	ctx := context.Background()
	o := referralCreateTestOrder(t, s, mid, 100, "original-rate")
	disabled, rate := false, int64(10000)
	if _, err := s.SetReferralPolicy(ctx, "global", 0, 1, referralTestPolicy(&disabled, &rate), referralPolicyAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE merchants SET enabled=false WHERE id=$1", promoter); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkPaid(ctx, o.TradeNo, "unverified-positive"); !errors.Is(err, ErrState) {
		t.Fatal("positive reward without authenticated receipt", err)
	}
	state, err := s.OrderByTradeNo(ctx, o.TradeNo)
	if err != nil || state.Status != "created" {
		t.Fatal("unverified callback partially credited", state, err)
	}
	referralPayTestOrder(t, s, o)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.markPaid(ctx, o.TradeNo, fmt.Sprintf("local-duplicate:%d", i), true, PaidReceipt{RealMoney: o.Amount, ChannelID: 7, ProviderTradeNo: "LOCAL_" + o.TradeNo}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	b, err := s.Balance(ctx, promoter)
	if err != nil || b.Available != 5 {
		t.Fatal("original promoter entitlement was changed or duplicated", b, err)
	}
	b, err = s.Balance(ctx, mid)
	if err != nil || b.Available != 90 {
		t.Fatal(b, err)
	}
	var rewards, amount, fees int64
	if err = s.pool.QueryRow(ctx, "SELECT count(*),COALESCE(sum(amount),0) FROM referral_rewards WHERE order_id=$1", o.ID).Scan(&rewards, &amount); err != nil || rewards != 1 || amount != 5 {
		t.Fatal(rewards, amount, err)
	}
	if err = s.pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees' AND scope_id=0").Scan(&fees); err != nil || fees != 5 {
		t.Fatal(fees, err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	source, err := s.referralRefundSource(ctx, tx, o.ID)
	if err != nil || source == nil || source.Amount != 5 || source.PlatformFee != 10 || source.PromoterID != promoter {
		t.Fatal(source, err)
	}
}

func TestReferralPolicyGroupInheritanceZeroOverrideAndCAS(t *testing.T) {
	s, _, mid, payerGroup := referralRewardFixture(t)
	ctx := context.Background()
	zero := int64(0)
	if _, err := s.SetReferralPolicy(ctx, "group", payerGroup, 0, referralTestPolicy(nil, &zero), referralPolicyAdmin); err != nil {
		t.Fatal(err)
	}
	o := referralCreateTestOrder(t, s, mid, 100, "explicit-zero")
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	terms, err := s.readReferralTerms(ctx, tx, o)
	tx.Rollback(ctx)
	if err != nil || terms.Enabled || terms.RateBPS != 0 || terms.Reason != "zero_rate" {
		t.Fatal(terms, err)
	}
	if _, err = s.SetReferralPolicy(ctx, "group", payerGroup, 0, referralTestPolicy(nil, nil), referralPolicyAdmin); !errors.Is(err, ErrConflict) {
		t.Fatal("stale policy version accepted", err)
	}
	if _, err = s.SetReferralPolicy(ctx, "group", payerGroup, 1, referralTestPolicy(nil, nil), referralPolicyAdmin); err != nil {
		t.Fatal(err)
	}
	o = referralCreateTestOrder(t, s, mid, 100, "nil-inherits")
	tx, err = s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	terms, err = s.readReferralTerms(ctx, tx, o)
	tx.Rollback(ctx)
	if err != nil || !terms.Enabled || terms.RateBPS != 5000 || terms.RewardAmount != 5 {
		t.Fatal(terms, err)
	}
	input := referralTestPolicy(nil, nil)
	input.Mode = "weekly"
	if _, err = s.SetReferralPolicy(ctx, "group", payerGroup, 2, input, referralPolicyAdmin); !errors.Is(err, ErrInvalid) {
		t.Fatal("unsupported weekly policy enabled", err)
	}
	operator := referralPolicyAdmin
	operator.Class = "operator"
	if _, err = s.SetReferralPolicy(ctx, "group", payerGroup, 2, referralTestPolicy(nil, nil), operator); !errors.Is(err, ErrInvalid) {
		t.Fatal("operator changed financial policy", err)
	}
	denied := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { return ErrState })
	if _, err = s.SetReferralPolicy(denied, "group", payerGroup, 2, referralTestPolicy(nil, nil), referralPolicyAdmin); !errors.Is(err, ErrState) {
		t.Fatal("financial guard bypass", err)
	}
	view, err := s.ReferralPolicy(ctx, "group", payerGroup, referralPolicyAdmin)
	if err != nil || view.Revision != 2 {
		t.Fatal("denied grant changed policy", view, err)
	}
}
