package payment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

// Independent review exercises both financial claimants against the same paid
// declaration, with different synthetic TLS providers and the same order lock.
func TestChannelSharingFinishReviewConcurrentPSAndPFClaimAtMostOneFinancialIntent(t *testing.T) {
	for run := 0; run < 3; run++ {
		t.Run(fmt.Sprint(run), func(t *testing.T) {
			base := newSharingExecutionFixture(t)
			ctx := context.Background()
			o := base.paid(t, "review-ps-pf-race", false)
			psFlow := base.Flow
			psFlow.Providers = map[int64]adapter.Provider{7: base.Flow.Providers[7]}
			finish := sharingFinishFixtureForOrder(t, base, o)
			start := make(chan struct{})
			errorsOut := make(chan error, 2)
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				_, e := psFlow.SubmitChannelSharing(ctx, 751, o.TradeNo, "review-concurrent-ps", channelSharingActor)
				errorsOut <- e
			}()
			go func() {
				defer wg.Done()
				<-start
				_, e := finish.Flow.SubmitChannelSharingFinish(ctx, 751, o.TradeNo, "review-concurrent-pf", channelSharingActor)
				errorsOut <- e
			}()
			close(start)
			wg.Wait()
			close(errorsOut)
			successes := 0
			for e := range errorsOut {
				if e == nil {
					successes++
				}
			}
			var ps, pf int
			e := base.Store.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM channel_sharing_orders),(SELECT count(*) FROM channel_sharing_finishes)").Scan(&ps, &pf)
			if e != nil || successes != 1 || ps+pf != 1 || base.Sends.Load()+finish.Sends.Load() != 1 {
				t.Fatal("concurrent original order admitted both financial requests", successes, ps, pf, base.Sends.Load(), finish.Sends.Load(), e)
			}
			b, e := base.Store.Balance(ctx, 751)
			if e != nil || b.Available != 0 || b.Reserved != 100 {
				t.Fatal("acceptance race released original funds", b, e)
			}
		})
	}
}

func TestChannelSharingFinishReviewForgedTerminalCannotBeRelabeledOrEnableIncomeAndRefund(t *testing.T) {
	f := newSharingFinishFixture(t)
	ctx := context.Background()
	first, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "review-finish-original", channelSharingActor)
	if e != nil || first.State != "pending" {
		t.Fatal(first, e)
	}
	tx, e := f.Store.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var account, available, journal, observation int64
	if e = tx.QueryRow(ctx, "SELECT account_id FROM channel_sharing_fundings WHERE order_id=$1", first.OrderID).Scan(&account); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=751 AND kind='available' AND scope_id=0").Scan(&available); e != nil {
		t.Fatal(e)
	}
	key := fmt.Sprintf("channel-sharing-finish:%d:release", first.ID)
	if e = postByAccountIDs(ctx, tx, key, "synthetic forensic forged PF", account, available, first.ReleaseAmount); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(ctx, "SELECT id FROM journals WHERE event_key=$1", key).Scan(&journal); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(ctx, "INSERT INTO channel_sharing_finish_observations(finish_id,fingerprint,source,provider_order_no,state,unsplit_amount,actor_role,actor_id,actor_name) VALUES($1,$2,'query',$3,'succeeded',$4,'merchant',751,'sharing-owner') RETURNING id", first.ID, make([]byte, 32), first.ProviderOrderNo, first.ExpectedUnsplit).Scan(&observation); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_finishes SET state='succeeded',last_observation_id=$2,release_journal_id=$3 WHERE id=$1", first.ID, observation, journal); e != nil {
		t.Fatal(e)
	}
	if e = sharingConfigurationAudit(ctx, tx, channelSharingActor, "channel-sharing-finishes", first.ID, "query-result", "succeeded"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Flow.QueryChannelSharingFinish(ctx, 751, "review-finish-original", channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal("public query adopted unkeyed retained completion", e)
	}
	if _, e = f.Flow.ReconcileChannelSharingFinish(ctx, first.ID); !errors.Is(e, ErrConflict) {
		t.Fatal("worker adopted unkeyed retained completion", e)
	}
	report, e := f.Store.CheckLedger(ctx)
	if e != nil || report.InvalidChannelSharingFinishes != 1 || report.InvalidChannelSharingFundings != 1 {
		t.Fatal("keyed finish/funding corruption escaped reconciliation", report, e)
	}
	if _, e = f.Store.OrderIncomeFreezable(ctx, 751, f.Order.TradeNo); !errors.Is(e, ErrConflict) {
		t.Fatal("unkeyed completion enabled original source income", e)
	}
	if _, e = f.Store.ReserveRefund(ctx, 751, f.Order.OutTradeNo, "review-forged-pf-refund", 1); !errors.Is(e, ErrConflict) {
		t.Fatal("unkeyed completion enabled ordinary refund", e)
	}
	if f.Sends.Load() != 1 || f.Queries.Load() != 0 {
		t.Fatal("failed retained proof reached provider", f.Sends.Load(), f.Queries.Load())
	}
}

func TestChannelSharingFinishReviewSQLCannotClaimPastPendingOrUnknownPS(t *testing.T) {
	for _, phase := range []int32{0, 4} {
		t.Run(fmt.Sprint(phase), func(t *testing.T) {
			base := newSharingExecutionFixture(t)
			ctx := context.Background()
			o := base.paid(t, "review-sql-pf-ps", false)
			base.Phase.Store(phase)
			ps, e := base.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "review-existing-ps", channelSharingActor)
			if phase == 0 && (e != nil || ps.State != "pending") || phase == 4 && (e == nil || ps.State != "unknown") {
				t.Fatal(ps, e)
			}
			tx, e := base.Store.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			_, e = tx.Exec(ctx, `INSERT INTO channel_sharing_finishes(order_id,merchant_id,external_no,out_no,provider_trade_no,gross_amount,merchant_net,expected_unsplit,release_amount,original_sharing_id,request_hash,actor_role,actor_id,actor_name) VALUES($1,751,'review-raw-pf','PF012345678901234567890123456789',$2,100,100,100,100,$3,$4,'merchant',751,'sharing-owner')`, o.ID, "WX_"+o.TradeNo, ps.ID, make([]byte, 32))
			if e == nil {
				t.Fatal("SQL birth guard accepted independent PF over unresolved original PS")
			}
			if e = tx.Rollback(ctx); e != nil {
				t.Fatal(e)
			}
			b, e := base.Store.Balance(ctx, 751)
			if e != nil || b.Available != 0 || b.Reserved != 100 || base.Sends.Load() != 1 {
				t.Fatal(b, e, base.Sends.Load())
			}
		})
	}
}

func TestChannelSharingFinishReviewLatestUnknownAfterTrustedFailureStillBlocksNewIntent(t *testing.T) {
	f := newSharingFinishFixture(t)
	ctx := context.Background()
	first, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "review-old-failed-pf", channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	f.Phase.Store(6)
	if _, e = f.Flow.QueryChannelSharingFinish(ctx, 751, first.ExternalNo, channelSharingActor); e != nil {
		t.Fatal(e)
	}
	f.Phase.Store(5)
	second, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "review-latest-unknown-pf", channelSharingActor)
	var unknown *adapter.UnknownOutcome
	if !errors.As(e, &unknown) || second.State != "unknown" || second.PreviousFailedID != first.ID || second.ID == first.ID {
		t.Fatal(second, e)
	}
	preview, e := f.Flow.PreviewChannelSharingFinish(ctx, 751, f.Order.TradeNo, channelSharingActor)
	if e != nil || preview.ID != second.ID || preview.State != "unknown" {
		t.Fatal("preview chose old failure over latest unknown operation", preview, e)
	}
	if _, e = f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "review-third-pf-bypass", channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal("old failed history reopened a newer unknown PF", e)
	}
	old, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, first.ExternalNo, channelSharingActor)
	if e != nil || old.ID != first.ID || old.State != "failed" || f.Sends.Load() != 2 {
		t.Fatal("old failed replay changed current operation", old, e, f.Sends.Load())
	}
	f.Phase.Store(1)
	reader := f.Flow
	reader.AllowLive = false
	done, e := reader.ReconcileChannelSharingFinish(ctx, second.ID)
	if e != nil || !done.FundsReleased || done.ID != second.ID {
		t.Fatal(done, e)
	}
	if _, e = reader.QueryChannelSharingFinish(ctx, 751, first.ExternalNo, channelSharingActor); e != nil {
		t.Fatal("old failed history could not still be read", e)
	}
	amount, e := f.Store.OrderIncomeFreezable(ctx, 751, f.Order.TradeNo)
	if e != nil || amount != 100 {
		t.Fatal("historical failed PF obscured latest credible release", amount, e)
	}
	report, e := f.Store.CheckLedger(ctx)
	if e != nil || report.InvalidChannelSharingFinishes != 0 || report.InvalidChannelSharingFundings != 0 {
		t.Fatal(report, e)
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 100 || b.Reserved != 0 || f.Sends.Load() != 2 || f.Queries.Load() != 2 {
		t.Fatal(b, e, f.Sends.Load(), f.Queries.Load())
	}
}
