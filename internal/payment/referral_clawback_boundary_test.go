package payment

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestReferralClawbackDatabaseRejectsMissingParentEscrowMutationAndAudit(t *testing.T) {
	s, _, _, o := referralClawbackFixture(t, "platform")
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `INSERT INTO refunds(merchant_id,order_id,refund_no,amount,merchant_amount,fee_amount) VALUES(902,$1,'raw-no-clawback',1,1,0)`, o.ID); err == nil {
		t.Fatal("raw refund skipped original reward")
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO accounts(owner_id,kind,scope_id) VALUES(0,'referral_clawback_reserve',9999)`); err == nil {
		t.Fatal("orphan private escrow committed")
	}
	r, err := s.ReserveRefund(ctx, 902, o.OutTradeNo, "boundary", 98)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, "UPDATE referral_refund_clawbacks SET original_fee=11 WHERE refund_id=$1", r.ID); err == nil {
		t.Fatal("original fee snapshot mutable")
	}
	if _, err = s.pool.Exec(ctx, `UPDATE accounts SET balance=balance-1 WHERE id=(SELECT escrow_account_id FROM referral_refund_clawbacks WHERE refund_id=$1)`, r.ID); err == nil {
		t.Fatal("unknown reward escrow became spendable")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "UPDATE refunds SET status='unknown' WHERE id=$1", r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE referral_refund_clawbacks SET state='unknown',result_hash=$2 WHERE refund_id=$1", r.ID, make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err == nil {
		t.Fatal("missing result audit committed")
	}
	referralClawbackBalances(t, s, 0, 8, 2, 0)
}

func TestReferralClawbackForgedFailedProjectionCannotReopenOrClaimProvider(t *testing.T) {
	s, flow, p, o := referralClawbackFixture(t, "platform")
	ctx := context.Background()
	r, err := s.ReserveRefund(ctx, 902, o.OutTradeNo, "crash-before-claim", 98)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	funding, err := s.scanRefundFunding(ctx, tx, r)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.scanReferralClawback(ctx, tx, tx.QueryRow(ctx, "SELECT "+referralClawbackColumns+" FROM referral_refund_clawbacks WHERE refund_id=$1", r.ID))
	if err != nil {
		t.Fatal(err)
	}
	// Produce all SQL-visible balances, journals and audit rows, while
	// deliberately lacking the service's keyed result proof.
	if err = lockRefundLedgerAccounts(ctx, tx, r, &c, "failed"); err != nil {
		t.Fatal(err)
	}
	// Keep schema63's independent principal-source proof genuine while the
	// schema58 reward and schema59 terminal proofs below remain deliberately
	// unkeyed. This preserves the original forgery target without bypassing any
	// new source-funding guard.
	if err = s.resolveRefundFunding(ctx, tx, r, funding, "failed"); err != nil {
		t.Fatal(err)
	}
	if err = post(ctx, tx, fmt.Sprintf("refund-fee:%d:resolve", r.ID), "forged fee restore", 0, "fee_reserved", 0, "fees", 8); err != nil {
		t.Fatal(err)
	}
	if c.RestoreContributionJournalID, err = referralClawbackJournal(ctx, tx, c, "restore-contribution", c.FeesAccountID, c.EscrowAccountID, 3); err != nil {
		t.Fatal(err)
	}
	if c.RestoreRewardJournalID, err = referralClawbackJournal(ctx, tx, c, "restore-reward", c.EscrowAccountID, c.PromoterAccountID, 5); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE refunds SET status='failed' WHERE id=$1", r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE referral_refund_clawbacks SET state='failed',restore_contribution_journal_id=$2,restore_reward_journal_id=$3,result_hash=$4,resolved_at=transaction_timestamp() WHERE refund_id=$1`, r.ID, c.RestoreContributionJournalID, c.RestoreRewardJournalID, make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	c.State = "failed"
	if err = insertReferralClawbackAudit(ctx, tx, c, make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	// Schema59 also requires a terminal result parent for fresh refunds. Keep
	// its SQL shape complete while deliberately supplying no valid HMAC.
	if _, err = tx.Exec(ctx, `INSERT INTO provider_refund_result_evidence(refund_id,order_id,amount,refund_no,prior_refund_state,state,source,proof_hash) VALUES($1,$2,$3,$4,'pending','failed','internal',$5)`, r.ID, r.OrderID, r.Amount, r.RefundNo, make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal("fixture failed to produce SQL-valid forged projection", err)
	}
	if _, err = s.Refund(ctx, 902, r.RefundNo); !errors.Is(err, ErrConflict) {
		t.Fatalf("public read adopted fake proof %v", err)
	}
	if _, err = s.ReserveRefund(ctx, 902, o.OutTradeNo, "new-number", 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("new number reopened fake failed reward %v", err)
	}
	a, err := s.ProviderAttempt(ctx, o.TradeNo)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = s.beginProviderOperation(ctx, "refund", fmt.Sprint(r.ID), o, a, r.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("reserve/claim gap adopted fake failed %v", err)
	}
	if _, err = flow.Refund(ctx, 902, o.TradeNo, r.RefundNo, 98); !errors.Is(err, ErrConflict) || p.refunds.Load() != 0 {
		t.Fatalf("replay sent after forged failure %v calls%d", err, p.refunds.Load())
	}
	report, err := s.CheckLedger(ctx)
	if err != nil || report.InvalidReferralClawbacks != 1 || report.InvalidProviderRefundResults != 1 {
		t.Fatalf("checker trusted forged terminal proof %+v %v", report, err)
	}
}
