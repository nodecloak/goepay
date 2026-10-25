package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestReferralReviewNegativeBirthCannotGainRewardAfterPolicyEnable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	referralBoundMerchants(t, s, 1810, 1811)
	referralFeeSetup(t, s, 1811, 1000, "platform")
	old := referralCreateTestOrder(t, s, 1811, 100, "review-default-off")
	enabled, rate := true, int64(5000)
	if _, err := s.SetReferralPolicy(ctx, "global", 0, 0, referralTestPolicy(&enabled, &rate), referralPolicyAdmin); err != nil {
		t.Fatal(err)
	}
	positive := referralCreateTestOrder(t, s, 1811, 100, "review-new-enabled")
	recharge, err := s.CreateRechargeOrder(ctx, CreateOrderInput{MerchantID: 1811, OutTradeNo: "review-recharge", Amount: 100, Name: "local recharge", Type: "wxpay"}, "local-review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, "UPDATE backoffice_records SET body=jsonb_set(body,'{value}','\"false\"') WHERE resource='settings' AND body->>'key'='payment_fee_enabled'"); err != nil {
		t.Fatal(err)
	}
	zero := referralCreateTestOrder(t, s, 1811, 100, "review-zero-fee")
	for _, o := range []Order{old, positive, recharge, zero} {
		referralPayTestOrder(t, s, o)
	}
	var rewards, amount, fees int64
	if err = s.pool.QueryRow(ctx, "SELECT count(*),COALESCE(sum(amount),0) FROM referral_rewards").Scan(&rewards, &amount); err != nil || rewards != 1 || amount != 5 {
		t.Fatal("negative/zero/recharge snapshot was retroactively awarded", rewards, amount, err)
	}
	if err = s.pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees' AND scope_id=0").Scan(&fees); err != nil || fees != 25 {
		t.Fatal("reward exceeded the original positive order fee", fees, err)
	}
	for mid, want := range map[int64]int64{1810: 5, 1811: 370} {
		b, e := s.Balance(ctx, mid)
		if e != nil || b.Available != want || b.Reserved != 0 {
			t.Fatal(mid, b, want, e)
		}
	}
	report, err := s.CheckLedger(ctx)
	if err != nil || report.InvalidReferralRewards != 0 || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
		t.Fatal(report, err)
	}
}

func TestReferralReviewPromoterGroupOverrideAndNilInheritanceAreFrozen(t *testing.T) {
	s, promoter, mid, payerGroup := referralRewardFixture(t)
	ctx := context.Background()
	var promoterGroup int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"review promoter group","enabled":true,"rate_bps":"0"}') RETURNING id`).Scan(&promoterGroup); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE merchants SET profile=jsonb_build_object('group_id',$2::bigint::text) WHERE id=$1", promoter, promoterGroup); err != nil {
		t.Fatal(err)
	}
	disabled, quarter := false, int64(2500)
	if _, err := s.SetReferralPolicy(ctx, "group", payerGroup, 0, referralTestPolicy(&disabled, &quarter), referralPolicyAdmin); err != nil {
		t.Fatal(err)
	}
	enabled, eighty := true, int64(8000)
	if _, err := s.SetReferralPolicy(ctx, "group", promoterGroup, 0, referralTestPolicy(&enabled, &eighty), referralPolicyAdmin); err != nil {
		t.Fatal(err)
	}
	positive := referralCreateTestOrder(t, s, mid, 100, "review-promoter-overrides")
	if _, err := s.SetReferralPolicy(ctx, "group", promoterGroup, 1, referralTestPolicy(nil, nil), referralPolicyAdmin); err != nil {
		t.Fatal(err)
	}
	negative := referralCreateTestOrder(t, s, mid, 100, "review-promoter-inherits")
	for i, o := range []Order{positive, negative} {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		terms, err := s.readReferralTerms(ctx, tx, o)
		tx.Rollback(ctx)
		wantRate, wantReward := int64(8000), int64(8)
		if i == 1 {
			wantRate, wantReward = 2500, 0
		}
		if err != nil || terms.RateBPS != wantRate || terms.RewardAmount != wantReward || terms.Enabled != (i == 0) || terms.PolicyVersions[3] != payerGroup || terms.PolicyVersions[4] != promoterGroup {
			t.Fatal("effective original group policy was not preserved", terms, err)
		}
		referralPayTestOrder(t, s, o)
	}
	b, err := s.Balance(ctx, promoter)
	if err != nil || b.Available != 8 {
		t.Fatal("late payment re-read the current group policy", b, err)
	}
	var fees int64
	if err = s.pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees' AND scope_id=0").Scan(&fees); err != nil || fees != 12 {
		t.Fatal(fees, err)
	}
}

func TestReferralReviewUnkeyedNegativeTermsFailClosedBeforeAnyPaymentJournal(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 1911, Name: "local negative proof", APIKey: "local-only"}); err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	o, err := scanOrder(tx.QueryRow(ctx, "INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount) VALUES('LOCAL_REVIEW_NEGATIVE',1911,'review-unkeyed-negative','local item','wxpay',100,100) RETURNING "+orderColumns))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO referral_order_terms(order_id,merchant_id,purpose,enabled,reason,rate_bps,platform_fee,reward_amount,refund_policy,policy_versions,proof_hash) VALUES($1,1911,'payment',false,'no_relation',0,0,0,$2,'[0,0,0,0,0]',$3)`, o.ID, referralRefundPolicy, make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal("SQL-shaped negative fixture failed before keyed read", err)
	}
	if err = s.MarkPaid(ctx, o.TradeNo, "review-unkeyed-negative"); !errors.Is(err, ErrConflict) {
		t.Fatal("negative proof was mistaken for a historical missing snapshot", err)
	}
	var journals, inbox int
	var status string
	if err = s.pool.QueryRow(ctx, "SELECT status,(SELECT count(*) FROM journals),(SELECT count(*) FROM inbox) FROM orders WHERE id=$1", o.ID).Scan(&status, &journals, &inbox); err != nil || status != "created" || journals != 0 || inbox != 0 {
		t.Fatal(fmt.Sprintf("status=%s journals=%d inbox=%d", status, journals, inbox), err)
	}
	report, err := s.CheckLedger(ctx)
	if err != nil || report.InvalidReferralTerms != 1 || report.InvalidReferralRewards != 0 || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
		t.Fatal("balanced empty ledger concealed an unauthenticated negative snapshot", report, err)
	}
}

func TestReferralReviewAwardAuditFaultRollsBackOriginalPaymentAndReceipt(t *testing.T) {
	s, promoter, mid, _ := referralRewardFixture(t)
	ctx := context.Background()
	o := referralCreateTestOrder(t, s, mid, 100, "review-payment-audit-fault")
	if _, _, err := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_review_reward_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'local reward audit failure'; END $$;
	CREATE TRIGGER reject_review_reward_audit BEFORE INSERT ON referral_reward_audits FOR EACH ROW EXECUTE FUNCTION reject_review_reward_audit()`); err != nil {
		t.Fatal(err)
	}
	receipt := PaidReceipt{ChannelID: 7, ProviderTradeNo: "LOCAL_REVIEW_" + o.TradeNo}
	if err := s.markPaid(ctx, o.TradeNo, "review-original-retry", true, receipt); err == nil {
		t.Fatal("reward without original audit was committed")
	}
	var receipts, rewards, journals, inbox int
	var status string
	if err := s.pool.QueryRow(ctx, `SELECT status,(SELECT count(*) FROM verified_payment_receipts),(SELECT count(*) FROM referral_rewards),
	(SELECT count(*) FROM journals),(SELECT count(*) FROM inbox) FROM orders WHERE id=$1`, o.ID).Scan(&status, &receipts, &rewards, &journals, &inbox); err != nil || status != "submitting" || receipts+rewards+journals+inbox != 0 {
		t.Fatal("reward fault left a partial original payment", status, receipts, rewards, journals, inbox, err)
	}
	for _, id := range []int64{mid, promoter} {
		b, err := s.Balance(ctx, id)
		if err != nil || b.Available != 0 || b.Reserved != 0 {
			t.Fatal("reward fault minted unbacked wallet balance", id, b, err)
		}
	}
	if _, err := s.pool.Exec(ctx, "DROP TRIGGER reject_review_reward_audit ON referral_reward_audits"); err != nil {
		t.Fatal(err)
	}
	if err := s.markPaid(ctx, o.TradeNo, "review-original-retry", true, receipt); err != nil {
		t.Fatal("same authenticated original callback could not recover atomically", err)
	}
	b, err := s.Balance(ctx, promoter)
	if err != nil || b.Available != 5 {
		t.Fatal(b, err)
	}
	report, err := s.CheckLedger(ctx)
	if err != nil || report.InvalidReferralTerms != 0 || report.InvalidReferralRewards != 0 || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
		t.Fatal(report, err)
	}
}
