package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestReferralCachedReceiptCannotTurnSandboxMarkPaidIntoTrustedAward(t *testing.T) {
	s, _, mid, _ := referralRewardFixture(t)
	ctx := context.Background()
	o := referralCreateTestOrder(t, s, mid, 100, "cached-receipt-is-not-callback")
	if _, _, err := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = recordPaidReceipt(ctx, tx, o.ID, PaidReceipt{ChannelID: 7, ProviderTradeNo: "UNAUTHENTICATED_SQL_PROJECTION"}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.MarkPaid(ctx, o.TradeNo, "sandbox-cannot-attest-old-receipt"); !errors.Is(err, ErrState) {
		t.Fatal("unsigned cached receipt was promoted into trusted award", err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM referral_rewards)+(SELECT count(*) FROM journals)+(SELECT count(*) FROM inbox)").Scan(&count); err != nil || count != 0 {
		t.Fatal("unverified result changed original ledger", count, err)
	}
}

// All SQL constraints stay enabled. A structurally balanced fixture is still
// not an authenticated application award; restored data must retain this
// distinction before a refund or callback can rely on the award.
func TestReferralSQLShapedForgedAwardCannotAuthorizeRefund(t *testing.T) {
	s, promoter, mid, _ := referralRewardFixture(t)
	ctx := context.Background()
	o := referralCreateTestOrder(t, s, mid, 100, "forged-award-proof")
	if _, _, err := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	terms, err := s.readReferralTerms(ctx, tx, o)
	if err != nil {
		t.Fatal(err)
	}
	if err = recordPaidReceipt(ctx, tx, o.ID, PaidReceipt{ChannelID: 7, ProviderTradeNo: "LOCAL_FORGED_" + o.TradeNo}); err != nil {
		t.Fatal(err)
	}
	if err = post(ctx, tx, "payment:"+o.TradeNo, "SQL-shaped original net", 0, "clearing", mid, "available", o.MerchantAmount); err != nil {
		t.Fatal(err)
	}
	if err = post(ctx, tx, "payment-fee:"+o.TradeNo, "SQL-shaped original fee", 0, "clearing", 0, "fees", o.PlatformFee); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("referral-award:%d", o.ID)
	if err = post(ctx, tx, key, "SQL-shaped fake reward", 0, "fees", promoter, "available", terms.RewardAmount); err != nil {
		t.Fatal(err)
	}
	var rewardID int64
	if err = tx.QueryRow(ctx, `INSERT INTO referral_rewards(order_id,merchant_id,promoter_id,amount,terms_hash,journal_id,channel_id,provider_trade_no,proof_hash) SELECT $1,$2,$3,$4,$5,id,7,$6,$7 FROM journals WHERE event_key=$8 RETURNING referral_rewards.id`, o.ID, mid, promoter, terms.RewardAmount, terms.ProofHash, "LOCAL_FORGED_"+o.TradeNo, make([]byte, 32), key).Scan(&rewardID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO referral_reward_audits(reward_id,event_key) VALUES($1,$2)", rewardID, key); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE orders SET status='paid',paid_at=clock_timestamp() WHERE id=$1", o.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal("SQL shape fixture must remain constraint-valid", err)
	}
	if err = s.MarkPaid(ctx, o.TradeNo, "untrusted-replay"); !errors.Is(err, ErrConflict) {
		t.Fatal("callback adopted forged reward proof", err)
	}
	if _, err = s.ReserveRefund(ctx, mid, o.OutTradeNo, "forged-refund", 100); !errors.Is(err, ErrConflict) {
		t.Fatal("refund adopted forged award", err)
	}
	report, err := s.CheckLedger(ctx)
	if err != nil || report.InvalidReferralRewards != 1 || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
		t.Fatal("keyed checker missed balanced forgery", report, err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM refunds)+(SELECT count(*) FROM referral_refund_clawbacks)").Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected proof created new refund", count, err)
	}
}
