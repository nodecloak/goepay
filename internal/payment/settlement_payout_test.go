package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"sync"
	"testing"
)

var settlementAdmin = TransferBatchActor{Role: "admin", Class: "operator", ID: 1, Name: "admin"}

func payoutFixture(t *testing.T) (*Store, ProviderFlow, *batchTestProvider, LedgerRequest) {
	t.Helper()
	s, g, p := batchFixture(t)
	ctx := context.Background()
	transferSetting(t, s, "settlement_fee_rate_bps", "300")
	cfg, e := s.SetSettlementRecipient(ctx, 501, SettlementRecipientInput{Type: "wxpay", AppID: "batch-test-app", Account: "original-payout-openid", Name: "Original Payout Name"}, nil, SettlementRecipientActor{Role: "merchant", ID: 501, Name: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.RequestFundsWithSettlementRecipient(ctx, "settlements", 501, 0, "payout-request", 1000, "merchant:501", nil, &cfg.ConfigID)
	if e != nil {
		t.Fatal(e)
	}
	v, e = s.ReviewFundsRequest(ctx, v.ID, true, "admin:1")
	if e != nil {
		t.Fatal(e)
	}
	return s, g, p, v
}
func TestSettlementPayoutPrepareUsesOriginalGrossWithoutSecondDebit(t *testing.T) {
	s, g, p, v := payoutFixture(t)
	ctx := context.Background()
	out, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil)
	if e != nil || out.SendState != "unsent" || out.Transfer.Amount != 970 || out.Transfer.CostAmount != 970 || out.Transfer.FeeAmount != 0 {
		t.Fatal(out, e)
	}
	if out.Transfer.FundingSource != "settlement" || out.Transfer.SettlementRequestID != v.ID || out.Transfer.SendState != "unsent" {
		t.Fatal("transfer funding hidden", out.Transfer)
	}
	replay, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil)
	if e != nil || replay.Transfer.ID != out.Transfer.ID {
		t.Fatal(replay, e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			same, err := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil)
			if err != nil || same.TransferID != out.TransferID {
				t.Error(same, err)
			}
		}()
	}
	wg.Wait()
	balance, _ := s.Balance(ctx, 501)
	if balance.Available != 0 || balance.Reserved != 1000 {
		t.Fatal("double debit", balance)
	}
	q, e := g.QueryTransfer(ctx, 501, out.Transfer.TransferNo)
	if e != nil || q.Status != "unsent" || p.query.Load() != 0 || p.transfers.Load() != 0 {
		t.Fatal(q, e)
	}
	var jobs int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE kind LIKE 'transfer.%'").Scan(&jobs); e != nil || jobs != 0 {
		t.Fatal("unsent reconciliation job", jobs, e)
	}
	if _, e = g.PrepareSettlementPayout(ctx, v.ID, 8, settlementAdmin, nil); e == nil {
		t.Fatal("changed channel accepted")
	}
}
func TestSettlementPayoutConcurrentExecuteUnknownOnlyQueryAndExactNetFeeJournals(t *testing.T) {
	s, g, p, v := payoutFixture(t)
	ctx := context.Background()
	p.unknown = true
	out, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil) }()
	}
	wg.Wait()
	if p.transfers.Load() != 1 {
		t.Fatal("duplicate settlement payment", p.transfers.Load())
	}
	if _, e = g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil); e != nil || p.transfers.Load() != 1 {
		t.Fatal("unknown resent", e)
	}
	balance, _ := s.Balance(ctx, 501)
	if balance.Available != 0 || balance.Reserved != 1000 {
		t.Fatal("unknown released", balance)
	}
	g.AllowLive = false
	recovered, e := g.QuerySettlementPayout(ctx, v.ID, settlementAdmin)
	if e != nil || recovered.State != "succeeded" || recovered.Transfer.ID != out.Transfer.ID || p.query.Load() != 1 {
		t.Fatal(recovered, e)
	}
	balance, _ = s.Balance(ctx, 501)
	var fee int64
	s.pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees'").Scan(&fee)
	if balance.Available != 0 || balance.Reserved != 0 || fee != 30 {
		t.Fatal("wrong settlement gross/net/fee", balance, fee)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidSettlementFundings != 0 || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
		t.Fatal(report, e)
	}
}
func TestSettlementPayoutCancelCompetesWithExecuteAndNoBankOrMissingSnapshot(t *testing.T) {
	s, g, p, v := payoutFixture(t)
	ctx := context.Background()
	p.unknown = true
	out, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil)
	if e != nil {
		t.Fatal(e)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-start; g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil) }()
	go func() { defer wg.Done(); <-start; g.CancelUnsentSettlementPayout(ctx, v.ID, settlementAdmin) }()
	close(start)
	wg.Wait()
	current, e := s.SettlementPayout(ctx, v.ID)
	if e != nil || current.SendState != "claimed" && current.SendState != "cancelled" || p.transfers.Load() > 1 {
		t.Fatal(current, e)
	}
	if current.SendState == "cancelled" {
		q, e := g.QueryTransfer(ctx, 501, out.Transfer.TransferNo)
		if e != nil || q.Status != "cancelled" || p.query.Load() != 0 {
			t.Fatal(q, e)
		}
		balance, _ := s.Balance(ctx, 501)
		if balance.Available != 1000 || balance.Reserved != 0 {
			t.Fatal(balance)
		}
	} else {
		if _, e = g.CancelUnsentSettlementPayout(ctx, v.ID, settlementAdmin); !errors.Is(e, ErrState) {
			t.Fatal("cancelled claimed", e)
		}
	}
}

func TestSettlementPayoutClaimedAdmissionRevocationDoesNotSendOrRearm(t *testing.T) {
	s, g, p, v := payoutFixture(t)
	ctx := context.Background()
	if _, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil); e != nil {
		t.Fatal(e)
	}
	checks := 0
	check := func(c context.Context, ch int64) error {
		checks++
		if checks == 2 {
			_, e := s.pool.Exec(c, "UPDATE merchants SET enabled=false WHERE id=501")
			return e
		}
		return nil
	}
	out, e := g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, check)
	if !errors.Is(e, ErrState) || out.SendState != "claimed" || p.transfers.Load() != 0 {
		t.Fatal("claim lost policy before send", out, e, p.transfers.Load())
	}
	if _, e = s.pool.Exec(ctx, "UPDATE merchants SET enabled=true WHERE id=501"); e != nil {
		t.Fatal(e)
	}
	out, e = g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil)
	if e != nil || out.SendState != "claimed" || p.transfers.Load() != 0 {
		t.Fatal("claimed gap rearmed", out, e)
	}
}
func TestSettlementPayoutGuardAuditFailureAndCancelAuthorizationRollback(t *testing.T) {
	s, g, p, v := payoutFixture(t)
	ctx := context.Background()
	if _, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil); e != nil {
		t.Fatal(e)
	}
	denied := errors.New("secondary authorization denied")
	guarded := WithFundsActionGuard(ctx, func(_ context.Context, _ pgx.Tx, kind, key string) error {
		if kind != "settlement-payout" && kind != "settlement-payout-cancel" || key != fmt.Sprint(v.ID) {
			t.Error(kind, key)
		}
		return denied
	})
	if _, e := g.ExecuteSettlementPayout(guarded, v.ID, settlementAdmin, nil); !errors.Is(e, denied) || p.transfers.Load() != 0 {
		t.Fatal(e)
	}
	if _, e := g.CancelUnsentSettlementPayout(guarded, v.ID, settlementAdmin); !errors.Is(e, denied) {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `CREATE TABLE payout_test_guard(id integer);CREATE FUNCTION payout_test_audit_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation='payout-claim' THEN RAISE EXCEPTION 'test payout audit blocked'; END IF;RETURN NEW;END $$;CREATE TRIGGER payout_test_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION payout_test_audit_failure()`); e != nil {
		t.Fatal(e)
	}
	consumed := WithFundsActionGuard(ctx, func(c context.Context, tx pgx.Tx, _, _ string) error {
		_, e := tx.Exec(c, "INSERT INTO payout_test_guard VALUES(1)")
		return e
	})
	if _, e := g.ExecuteSettlementPayout(consumed, v.ID, settlementAdmin, nil); e == nil || p.transfers.Load() != 0 {
		t.Fatal("audit failed open", e)
	}
	var grants, jobs int
	if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM payout_test_guard),(SELECT count(*) FROM jobs WHERE kind='transfer.reconcile')").Scan(&grants, &jobs); e != nil || grants+jobs != 0 {
		t.Fatal(grants, jobs, e)
	}
	out, e := s.SettlementPayout(ctx, v.ID)
	if e != nil || out.SendState != "unsent" {
		t.Fatal(out, e)
	}
}
func TestSettlementPayoutRejectsMissingSnapshotBankAndUnapprovedRequest(t *testing.T) {
	for _, kind := range []string{"missing", "bank", "pending"} {
		t.Run(kind, func(t *testing.T) {
			s, g, p := batchFixture(t)
			ctx := context.Background()
			if kind != "missing" {
				typ, account, app := "wxpay", "test-openid", "batch-test-app"
				if kind == "bank" {
					typ, account, app = "bank", "6222020000000000", ""
				}
				if _, e := s.SetSettlementRecipient(ctx, 501, SettlementRecipientInput{Type: typ, Account: account, Name: "Synthetic Name", AppID: app}, nil, SettlementRecipientActor{Role: "merchant", ID: 501, Name: "merchant"}); e != nil {
					t.Fatal(e)
				}
			}
			v, e := s.RequestFunds(ctx, "settlements", 501, 0, "not-ready", 1000, "merchant:501")
			if e != nil {
				t.Fatal(e)
			}
			if kind != "pending" {
				if _, e = s.ReviewFundsRequest(ctx, v.ID, true, "admin:1"); e != nil {
					t.Fatal(e)
				}
			}
			_, e = g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil)
			if e == nil || p.transfers.Load() != 0 {
				t.Fatal(kind, e)
			}
			if kind == "bank" && !errors.Is(e, adapter.ErrUnsupported) {
				t.Fatal("bank pretended wallet", e)
			}
			var count int
			if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM transfers").Scan(&count); e != nil || count != 0 {
				t.Fatal(count, e)
			}
		})
	}
}

type payoutChangedApplication struct{ batchTestProvider }

func (*payoutChangedApplication) ApplicationID() string { return "replacement-app" }
func TestSettlementPayoutApplicationBindingAndFailedOutcomeNeverRecreatesTransfer(t *testing.T) {
	s, g, p, v := payoutFixture(t)
	ctx := context.Background()
	p.fail = true
	first, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil)
	if e != nil {
		t.Fatal(e)
	}
	g.Providers[7] = &payoutChangedApplication{}
	if _, e = g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil); !errors.Is(e, ErrConflict) || p.transfers.Load() != 0 {
		t.Fatal("application changed", e)
	}
	g.Providers[7] = p
	failed, e := g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil)
	if e != nil || failed.State != "failed" || failed.Funding.State != "released" {
		t.Fatal(failed, e)
	}
	balance, _ := s.Balance(ctx, 501)
	if balance.Available != 1000 || balance.Reserved != 0 {
		t.Fatal("did not release original gross", balance)
	}
	replay, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil)
	if e != nil || replay.TransferID != first.TransferID {
		t.Fatal(replay, e)
	}
	if _, e = g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil); e != nil || p.transfers.Load() != 1 {
		t.Fatal("failed recreated", e)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidSettlementFundings != 0 {
		t.Fatal(report, e)
	}
}

type payoutInstantSuccess struct{ batchTestProvider }

func (p *payoutInstantSuccess) Transfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.transfers.Add(1)
	return adapter.TransferResult{TransferNo: r.TransferNo, Amount: r.Amount, State: "SUCCESS", ProviderTransferNo: "instant-original"}, nil
}
func TestSettlementPayoutResultAuditFailureKeepsVerifiedPaymentAndNoOrdinarySendBypass(t *testing.T) {
	s, g, _, v := payoutFixture(t)
	ctx := context.Background()
	p := &payoutInstantSuccess{}
	g.Providers[7] = p
	out, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.Transfer(ctx, 501, 7, out.Transfer.TransferNo, transferRequest(out.Transfer)); e != nil || p.transfers.Load() != 0 {
		t.Fatal("ordinary create acquired settlement send", e)
	}
	if e = s.ResolveTransfer(ctx, out.TransferID, "succeeded"); e == nil {
		t.Fatal("unsent transfer manually marked paid")
	}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION payout_result_test_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation='payout-result' THEN RAISE EXCEPTION 'test result audit rejected'; END IF;RETURN NEW;END $$;CREATE TRIGGER payout_result_test BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION payout_result_test_failure()`); e != nil {
		t.Fatal(e)
	}
	out, e = g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil)
	if e == nil || out.State != "succeeded" || out.Funding.State != "paid" || p.transfers.Load() != 1 {
		t.Fatal("lost real payment due audit error", out, e)
	}
	if _, e = g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil); e != nil || p.transfers.Load() != 1 {
		t.Fatal("audit error rearmed send", e)
	}
	balance, _ := s.Balance(ctx, 501)
	if balance.Available != 0 || balance.Reserved != 0 {
		t.Fatal(balance)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidSettlementFundings != 0 || report.InvalidAccounts != 0 {
		t.Fatal(report, e)
	}
}

func TestSettlementPayoutEvidenceGuardRejectsAttemptSuccessWithoutScopedSettlementJournals(t *testing.T) {
	s, g, _, v := payoutFixture(t)
	ctx := context.Background()
	p, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil)
	if e != nil {
		t.Fatal(e)
	}
	checks := 0
	_, e = g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, func(context.Context, int64) error {
		checks++
		if checks == 2 {
			return ErrLiveDisabled
		}
		return nil
	})
	if !errors.Is(e, ErrLiveDisabled) {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE provider_transfer_attempts SET state='succeeded',provider_transfer_no='forged' WHERE transfer_id=$1", p.TransferID); e == nil {
		t.Fatal("attempt-only success poisoned original pending payout")
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidSettlementFundings != 0 {
		t.Fatal(report, e)
	}
}
