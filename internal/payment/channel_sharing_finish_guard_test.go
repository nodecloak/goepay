package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"sync"
	"testing"
)

func TestChannelSharingFinishLostAcceptanceKeepsOriginalNumberAndNeverResends(t *testing.T) {
	f := newSharingFinishFixture(t)
	ctx := context.Background()
	f.Phase.Store(5)
	first, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "finish-lost", channelSharingActor)
	if e == nil || first.ID <= 0 || first.State != "unknown" {
		t.Fatal(first, e)
	}
	if _, e = f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "finish-lost-alias", channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal("unknown PF was rearmed with another caller number", e)
	}
	if _, e = f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "finish-lost", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if f.Sends.Load() != 1 {
		t.Fatal("PF resubmitted", f.Sends.Load())
	}
	f.Phase.Store(1)
	reader := f.Flow
	reader.AllowLive = false
	final, e := reader.ReconcileChannelSharingFinish(ctx, first.ID)
	if e != nil || !final.FundsReleased || final.OutNo != first.OutNo || final.ID != first.ID {
		t.Fatal(final, e)
	}
	if f.Sends.Load() != 1 || f.Queries.Load() != 1 {
		t.Fatal(f.Sends.Load(), f.Queries.Load())
	}
}
func TestChannelSharingFinishConcurrentClaimsConsumeOneGrantAndSendOnce(t *testing.T) {
	f := newSharingFinishFixture(t)
	ctx := context.Background()
	if _, e := f.Store.pool.Exec(ctx, "CREATE TABLE local_finish_grants(count integer NOT NULL); INSERT INTO local_finish_grants VALUES(0)"); e != nil {
		t.Fatal(e)
	}
	guarded := WithFundsActionGuard(ctx, func(c context.Context, tx pgx.Tx, kind, key string) error {
		if kind != "channel-sharing-finish" || key != "751:finish-concurrent" {
			return ErrInvalid
		}
		_, e := tx.Exec(c, "UPDATE local_finish_grants SET count=count+1")
		return e
	})
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan ChannelSharingFinishView, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r, e := f.Flow.SubmitChannelSharingFinish(guarded, 751, f.Order.TradeNo, "finish-concurrent", channelSharingActor)
			if e != nil {
				t.Error(e)
			}
			results <- r
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var id int64
	for r := range results {
		if id == 0 {
			id = r.ID
		}
		if r.ID != id {
			t.Fatal("concurrent duplicate PF", r, id)
		}
	}
	var grants, jobs int
	if e := f.Store.pool.QueryRow(ctx, "SELECT (SELECT count FROM local_finish_grants),(SELECT count(*) FROM jobs WHERE kind='channel-sharing-finish.reconcile')").Scan(&grants, &jobs); e != nil || grants != 1 || jobs != 1 || f.Sends.Load() != 1 {
		t.Fatal(grants, jobs, f.Sends.Load(), e)
	}
}
func TestChannelSharingFinishRequiresOriginalQueryTerminalReceiverAndGrossProof(t *testing.T) {
	for _, phase := range []int32{0, 2, 3, 4, 6, 7} {
		t.Run(fmt.Sprint(phase), func(t *testing.T) {
			f := newSharingFinishFixture(t)
			ctx := context.Background()
			first, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "finish-invalid-proof", channelSharingActor)
			if e != nil {
				t.Fatal(e)
			}
			f.Phase.Store(phase)
			v, e := f.Flow.QueryChannelSharingFinish(ctx, 751, "finish-invalid-proof", channelSharingActor)
			if phase == 0 || phase == 6 {
				if e != nil {
					t.Fatal(e)
				}
			} else if e == nil {
				t.Fatal("invalid original finish proof was accepted", phase, v)
			}
			b, err := f.Store.Balance(ctx, 751)
			if err != nil || b.Available != 0 || b.Reserved != 100 || v.FundsReleased {
				t.Fatal("unproved finish released original funds", v, b, err)
			}
			var n int
			if e = f.Store.pool.QueryRow(ctx, "SELECT count(*) FROM channel_sharing_fund_releases WHERE order_id=$1", f.Order.ID).Scan(&n); e != nil || n != 0 {
				t.Fatal(n, e)
			}
			if _, e = f.Store.ReserveRefund(ctx, 751, f.Order.OutTradeNo, "finish-still-blocked", 1); !errors.Is(e, ErrState) {
				t.Fatal(e)
			}
			if _, e = f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "finish-rearm", channelSharingActor); phase != 6 && !errors.Is(e, ErrConflict) || phase == 6 && e != nil {
				t.Fatal("nonreleased PF admitted alias", e)
			}
			_ = first
		})
	}
}
func TestChannelSharingFinishBlocksUnobservedOrPendingPSAndReturnsPriorAutoFinish(t *testing.T) {
	for _, mode := range []string{"claimed", "pending", "partial", "failed-unaccepted", "forged-failed"} {
		t.Run(mode, func(t *testing.T) {
			base := newSharingExecutionFixture(t)
			ctx := context.Background()
			o := base.paid(t, "finish-ps-barrier", mode == "partial")
			switch mode {
			case "pending":
				if _, e := base.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "prior-ps", channelSharingActor); e != nil {
					t.Fatal(e)
				}
			case "partial":
				base.Phase.Store(2)
				v, e := base.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "prior-ps", channelSharingActor)
				if e != nil || !v.FundsReleased {
					t.Fatal(v, e)
				}
			default:
				r, owner, e := base.Flow.claimChannelSharingOrder(ctx, 751, o.TradeNo, "prior-ps", channelSharingActor)
				if e != nil || !owner {
					t.Fatal(r, e)
				}
				if mode == "failed-unaccepted" {
					if e = base.Store.sharingOrderFailure(ctx, r, "failed", channelSharingActor); e != nil {
						t.Fatal(e)
					}
				}
				if mode == "forged-failed" {
					tx, e := base.Store.pool.Begin(ctx)
					if e != nil {
						t.Fatal(e)
					}
					if _, e = tx.Exec(ctx, "UPDATE channel_sharing_orders SET state='failed',failure_proof=decode(repeat('00',32),'hex') WHERE id=$1", r.ID); e != nil {
						t.Fatal(e)
					}
					if e = sharingConfigurationAudit(ctx, tx, channelSharingActor, "channel-sharing-orders", r.ID, "submit-result", "failed"); e != nil {
						t.Fatal(e)
					}
					if e = tx.Commit(ctx); e != nil {
						t.Fatal(e)
					}
				}
			}
			f := sharingFinishFixtureForOrder(t, base, o)
			v, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, o.TradeNo, "finish-after-ps", channelSharingActor)
			switch mode {
			case "partial":
				if e != nil || v.Source != "sharing" || v.Sharing == nil || !v.FundsReleased || v.ReleaseAmount != 80 || f.Sends.Load() != 0 {
					t.Fatal(v, e)
				}
			case "failed-unaccepted":
				if e != nil || v.Source != "finish" || f.Sends.Load() != 1 {
					t.Fatal(v, e)
				}
				f.Phase.Store(1)
				v, e = f.Flow.QueryChannelSharingFinish(ctx, 751, "finish-after-ps", channelSharingActor)
				if e != nil || !v.FundsReleased || v.ReleaseAmount != 100 {
					t.Fatal(v, e)
				}
			case "forged-failed":
				if !errors.Is(e, ErrConflict) || f.Sends.Load() != 0 {
					t.Fatal("unkeyed parent failure opened PF", v, e)
				}
			default:
				if !errors.Is(e, ErrState) || f.Sends.Load() != 0 {
					t.Fatal("unresolved PS opened PF", v, e)
				}
			}
		})
	}
}
func TestChannelSharingFinishAuditFailuresAndSQLCannotUnfreezeOrRearm(t *testing.T) {
	f := newSharingFinishFixture(t)
	ctx := context.Background()
	if _, e := f.Store.pool.Exec(ctx, `CREATE FUNCTION reject_local_finish_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='channel-sharing-finishes' THEN RAISE EXCEPTION 'local finish audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_local_finish_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_local_finish_audit()`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "finish-audit", channelSharingActor); e == nil || f.Sends.Load() != 0 {
		t.Fatal("claim without audit sent PF", e)
	}
	if _, e := f.Store.pool.Exec(ctx, "DROP TRIGGER reject_local_finish_audit ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	r, owner, e := f.Flow.claimSharingFinish(ctx, 751, f.Order.TradeNo, "finish-audit", channelSharingActor)
	if e != nil || !owner {
		t.Fatal(r, e)
	}
	// No final query observation means a balanced release is still unproved.
	tx, e := f.Store.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var available int64
	if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=751 AND kind='available'").Scan(&available); e != nil {
		t.Fatal(e)
	}
	key := fmt.Sprintf("channel-sharing-finish:%d:release", r.ID)
	if e = postByAccountIDs(ctx, tx, key, "local unproved finish release", r.FundingAccount, available, 100); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_finishes SET state='succeeded',release_journal_id=(SELECT id FROM journals WHERE event_key=$2) WHERE id=$1", r.ID, key); e != nil {
		t.Fatal(e)
	}
	if e = sharingConfigurationAudit(ctx, tx, channelSharingActor, "channel-sharing-finishes", r.ID, "query-result", "succeeded"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e == nil {
		t.Fatal("balanced release and audit replaced the original PF query proof")
	}

	// A fabricated failed row cannot authorize reads or any new number.
	tx, e = f.Store.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_finishes SET state='failed',failure_proof=decode(repeat('00',32),'hex') WHERE id=$1", r.ID); e != nil {
		t.Fatal(e)
	}
	if e = sharingConfigurationAudit(ctx, tx, channelSharingActor, "channel-sharing-finishes", r.ID, "submit-result", "failed"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Flow.QueryChannelSharingFinish(ctx, 751, "finish-audit", channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal("unkeyed finish failure accepted", e)
	}
	if _, e = f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "finish-new-after-forged", channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 0 || b.Reserved != 100 {
		t.Fatal(b, e)
	}
}

func TestChannelSharingFinishLostResultAuditLeavesOriginalClaimQueryable(t *testing.T) {
	f := newSharingFinishFixture(t)
	ctx := context.Background()
	if _, e := f.Store.pool.Exec(ctx, `CREATE FUNCTION reject_finish_result_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='channel-sharing-finishes' AND NEW.operation='submit-result' THEN RAISE EXCEPTION 'local finish result audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_finish_result_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_finish_result_audit()`); e != nil {
		t.Fatal(e)
	}
	v, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "finish-lost-result-audit", channelSharingActor)
	if e == nil || f.Sends.Load() != 1 || v.ID <= 0 {
		t.Fatal(v, e)
	}
	persisted, e := f.Store.ChannelSharingFinish(ctx, 751, "finish-lost-result-audit", channelSharingActor)
	if e != nil || persisted.State != "claimed" {
		t.Fatal(persisted, e)
	}
	if _, e = f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "finish-lost-result-audit", channelSharingActor); e != nil || f.Sends.Load() != 1 {
		t.Fatal(e, f.Sends.Load())
	}
	if _, e = f.Store.pool.Exec(ctx, "DROP TRIGGER reject_finish_result_audit ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	f.Phase.Store(1)
	final, e := f.Flow.QueryChannelSharingFinish(ctx, 751, "finish-lost-result-audit", channelSharingActor)
	if e != nil || !final.FundsReleased || final.OutNo != v.OutNo {
		t.Fatal(final, e)
	}
}
func TestChannelSharingFinishUsesGrossProofNetReleaseAndT1OriginalIncome(t *testing.T) {
	base := newSharingExecutionFixture(t)
	ctx := context.Background()
	old := base.paid(t, "finish-matured-other-order", false)
	completeIncomeChannelSharing(t, base, old, "finish-matured-other-order-ps")
	if _, e := base.Store.pool.Exec(ctx, "UPDATE orders SET paid_at=statement_timestamp()-interval '2 days' WHERE id=$1", old.ID); e != nil {
		t.Fatal(e)
	}
	var group int64
	if e := base.Store.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"local finish fee","enabled":true,"rate_bps":"1000"}') RETURNING id`).Scan(&group); e != nil {
		t.Fatal(e)
	}
	if _, e := base.Store.pool.Exec(ctx, "UPDATE merchants SET profile=jsonb_build_object('group_id',$1::bigint::text) WHERE id=751", group); e != nil {
		t.Fatal(e)
	}
	for _, pair := range [][2]string{{"payment_fee_enabled", "true"}, {"refund_fee_payer", "platform"}, {"settle_type", "1"}} {
		if _, e := base.Store.pool.Exec(ctx, "INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))", pair[0], pair[1]); e != nil {
			t.Fatal(e)
		}
	}
	o := base.paid(t, "finish-fee-original", false)
	if o.MerchantAmount != 90 || o.PlatformFee != 10 {
		t.Fatal(o)
	}
	f := sharingFinishFixtureForOrder(t, base, o)
	before, e := f.Store.FundsAvailability(ctx, 751)
	if e != nil || before.LedgerAvailable != 80 || before.TodayGross != 0 || before.Available != 80 {
		t.Fatal("dedicated original reserve double-blocked other income", before, e)
	}
	if _, e = f.Store.ReserveRefund(ctx, 751, o.OutTradeNo, "finish-before-proof", 10); !errors.Is(e, ErrState) {
		t.Fatal(e)
	}
	first, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, o.TradeNo, "finish-fee", channelSharingActor)
	if e != nil || first.ExpectedUnsplit != 100 || first.ReleaseAmount != 90 {
		t.Fatal(first, e)
	}
	f.Phase.Store(1)
	v, e := f.Flow.QueryChannelSharingFinish(ctx, 751, "finish-fee", channelSharingActor)
	if e != nil || !v.FundsReleased || v.ReleaseAmount != 90 {
		t.Fatal(v, e)
	}
	after, e := f.Store.FundsAvailability(ctx, 751)
	if e != nil || after.LedgerAvailable != 170 || after.TodayGross != 100 || after.Available != 70 {
		t.Fatal("PF bypassed original gross T1 policy or subsidized platform fee", after, e)
	}
	var fees int64
	if e = f.Store.pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees'").Scan(&fees); e != nil || fees != 10 {
		t.Fatal(fees, e)
	}
	refund, e := f.Store.ReserveRefund(ctx, 751, o.OutTradeNo, "finish-after-proof", 10)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Store.ResolveRefund(ctx, refund.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Store.ExecuteProfitRule(ctx, incomeSharingRule(t, f.Store, 2000), o.TradeNo, "merchant:751"); e != nil {
		t.Fatal(e)
	}
	h, e := f.Store.FreezeOrderIncome(ctx, 751, o.TradeNo, "finish-original-income", incomeTestActor)
	if e != nil || h.Amount != 60 || h.RefundMerchantAmount != 10 || h.InternalAllocatedAmount != 20 || h.ChannelSharingAmount != 0 {
		t.Fatal("PF original income mixed distinct source uses", h, e)
	}
	if _, e = f.Store.UnfreezeOrderIncome(ctx, 751, o.TradeNo, h.ID, incomeTestActor); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "finish-cannot-create-ps", channelSharingActor); e == nil {
		t.Fatal("PS was allowed after independent PF claimed the original payment")
	}
}

func TestChannelSharingFinishDatabaseRequiresOriginalFailureResultAudit(t *testing.T) {
	for _, state := range []string{"unknown", "failed"} {
		t.Run(state, func(t *testing.T) {
			f := newSharingFinishFixture(t)
			ctx := context.Background()
			r, owner, e := f.Flow.claimSharingFinish(ctx, 751, f.Order.TradeNo, "finish-result-audit", channelSharingActor)
			if e != nil || !owner {
				t.Fatal(r, e)
			}
			tx, e := f.Store.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			var proof any
			if state == "failed" {
				h, e := f.Store.sharingFinishFailureFingerprint(r)
				if e != nil {
					t.Fatal(e)
				}
				proof = h
			}
			if _, e = tx.Exec(ctx, "UPDATE channel_sharing_finishes SET state=$2,failure_proof=$3 WHERE id=$1", r.ID, state, proof); e != nil {
				t.Fatal(e)
			}
			if e = tx.Commit(ctx); e == nil {
				t.Fatal("finish result detached its original actor audit", state)
			}
		})
	}
}
func TestChannelSharingFinishPriorAutoFinishRemainsReadableAfterLaterMerchantMoneyUses(t *testing.T) {
	for _, use := range []string{"refund", "income-hold", "internal-allocation"} {
		t.Run(use, func(t *testing.T) {
			base := newSharingExecutionFixture(t)
			ctx := context.Background()
			o := base.paid(t, "finish-read-existing", false)
			completeIncomeChannelSharing(t, base, o, "finish-read-existing-ps")
			switch use {
			case "refund":
				r, e := base.Store.ReserveRefund(ctx, 751, o.OutTradeNo, "after-completed-ps", 20)
				if e != nil {
					t.Fatal(e)
				}
				if e = base.Store.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
					t.Fatal(e)
				}
			case "income-hold":
				if _, e := base.Store.FreezeOrderIncome(ctx, 751, o.TradeNo, "after-completed-ps", incomeTestActor); e != nil {
					t.Fatal(e)
				}
			default:
				if _, e := base.Store.ExecuteProfitRule(ctx, incomeSharingRule(t, base.Store, 3000), o.TradeNo, "merchant:751"); e != nil {
					t.Fatal(e)
				}
			}
			f := sharingFinishFixtureForOrder(t, base, o)
			guarded := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error {
				t.Error("readonly existing release consumed a new funds grant")
				return ErrInvalid
			})
			preview, e := f.Flow.PreviewChannelSharingFinish(guarded, 751, o.TradeNo, channelSharingActor)
			if e != nil || preview.Source != "sharing" || !preview.FundsReleased || preview.ReleaseAmount != 80 {
				t.Fatal(preview, e)
			}
			v, e := f.Flow.SubmitChannelSharingFinish(guarded, 751, o.TradeNo, "readonly-existing-finish", channelSharingActor)
			if e != nil || v.Source != "sharing" || v.Sharing == nil || v.Sharing.ExternalNo != "finish-read-existing-ps" || f.Sends.Load() != 0 {
				t.Fatal(v, e)
			}
			var n int
			if e = f.Store.pool.QueryRow(ctx, "SELECT count(*) FROM channel_sharing_finishes WHERE order_id=$1", o.ID).Scan(&n); e != nil || n != 0 {
				t.Fatal(n, e)
			}
		})
	}
}

func TestChannelSharingFinishTrustedFailureAllowsNewIntentWithFreshGrantButNeverRearmsOldNumber(t *testing.T) {
	f := newSharingFinishFixture(t)
	ctx := context.Background()
	grants := 0
	guarded := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { grants++; return nil })
	first, e := f.Flow.SubmitChannelSharingFinish(guarded, 751, f.Order.TradeNo, "finish-failed-original", channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	f.Phase.Store(6)
	failed, e := f.Flow.QueryChannelSharingFinish(ctx, 751, "finish-failed-original", channelSharingActor)
	if e != nil || failed.State != "failed" || failed.FundsReleased {
		t.Fatal(failed, e)
	}
	preview, e := f.Flow.PreviewChannelSharingFinish(ctx, 751, f.Order.TradeNo, channelSharingActor)
	if e != nil || preview.State != "eligible" || preview.PreviousFailedID != first.ID {
		t.Fatal("credible failure left original income permanently frozen", preview, e)
	}
	f.Phase.Store(0)
	next, e := f.Flow.SubmitChannelSharingFinish(guarded, 751, f.Order.TradeNo, "finish-fresh-intent", channelSharingActor)
	if e != nil || next.ID == first.ID || next.OutNo == first.OutNo || next.State != "pending" {
		t.Fatal("explicit fresh PF after proved failure was blocked or reused old identity", next, e)
	}
	old, e := f.Flow.SubmitChannelSharingFinish(guarded, 751, f.Order.TradeNo, "finish-failed-original", channelSharingActor)
	if e != nil || old.ID != first.ID || old.State != "failed" || grants != 2 || f.Sends.Load() != 2 {
		t.Fatal("old failed number rearmed or consumed another grant", old, e, grants, f.Sends.Load())
	}
	f.Phase.Store(1)
	done, e := f.Flow.QueryChannelSharingFinish(ctx, 751, "finish-fresh-intent", channelSharingActor)
	if e != nil || !done.FundsReleased {
		t.Fatal(done, e)
	}
	records, e := f.Store.ChannelSharingFinishes(ctx, 751, 10, channelSharingActor)
	if e != nil || len(records) != 2 || records[0].State != "succeeded" || records[1].State != "failed" {
		t.Fatal(records, e)
	}
	if _, e = f.Flow.SubmitChannelSharingFinish(guarded, 751, f.Order.TradeNo, "finish-after-success", channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal("successful PF admitted another release", e)
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 100 || b.Reserved != 0 {
		t.Fatal(b, e)
	}
}
