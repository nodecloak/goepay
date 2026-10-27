package payment

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestReferralClawbackCompleteAccountSetPreventsLegacyTransferLockCycle(t *testing.T) {
	for _, kind := range []string{"legacy-transfer", "reward-refund"} {
		t.Run(kind, func(t *testing.T) {
			s, _, _, o := referralClawbackFixture(t, "platform")
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			var businessID int64
			if kind == "legacy-transfer" {
				tx, err := s.pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(context.Background())
				transfer, err := s.reserveTransferTx(ctx, tx, 902, "fee-transfer", 10, 12, 2, 2000, "")
				if err != nil {
					t.Fatal(err)
				}
				businessID = transfer.ID
				if err = tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				r, err := s.ReserveRefund(ctx, 902, o.OutTradeNo, "with-reward", 98)
				if err != nil {
					t.Fatal(err)
				}
				businessID = r.ID
			}
			var fees, reserved int64
			if err := s.pool.QueryRow(ctx, "SELECT (SELECT id FROM accounts WHERE owner_id=0 AND kind='fees'),(SELECT id FROM accounts WHERE owner_id=902 AND kind='reserved')").Scan(&fees, &reserved); err != nil {
				t.Fatal(err)
			}
			if fees >= reserved {
				t.Fatal("fixture did not create the original inverse account order")
			}
			holder, err := s.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback(context.Background())
			var holderPID int32
			if err = holder.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&holderPID); err != nil {
				t.Fatal(err)
			}
			if _, err = holder.Exec(ctx, "SELECT id FROM accounts WHERE id=$1 FOR UPDATE", fees); err != nil {
				t.Fatal(err)
			}
			resolver, err := s.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer resolver.Rollback(context.Background())
			var resolverPID int32
			if err = resolver.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&resolverPID); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				var e error
				if kind == "legacy-transfer" {
					e = s.resolveTransferTx(ctx, resolver, businessID, "succeeded")
				} else {
					// Retain this transaction's known PID while applying the same
					// trusted internal result parent as Store.ResolveRefund.
					prior, readErr := scanRefund(resolver.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1", businessID))
					e = readErr
					if e == nil {
						e = s.resolveRefundTx(ctx, resolver, businessID, "succeeded")
					}
					if e == nil {
						e = s.insertRefundResultEvidence(ctx, resolver, prior, providerRefundResultEvidence{RefundID: prior.ID, OrderID: prior.OrderID, Amount: prior.Amount, RefundNo: prior.RefundNo, PriorRefundState: prior.Status, State: "succeeded", Source: "internal"})
					}
				}
				if e == nil {
					e = resolver.Commit(ctx)
				}
				done <- e
			}()
			for {
				var blocked bool
				if err = s.pool.QueryRow(ctx, "SELECT $1=ANY(pg_blocking_pids($2))", holderPID, resolverPID).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				select {
				case err = <-done:
					t.Fatalf("resolver finished without waiting on the original fee lock: %v", err)
				case <-ctx.Done():
					t.Fatal("resolver never reached complete account lock", ctx.Err())
				case <-time.After(5 * time.Millisecond):
				}
			}
			// The resolver must wait at the lower fee account before taking the
			// higher merchant reservation. Pair-by-pair locking fails this NOWAIT.
			var got int64
			lockErr := holder.QueryRow(ctx, "SELECT id FROM accounts WHERE id=$1 FOR UPDATE NOWAIT", reserved).Scan(&got)
			if err = holder.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
				t.Fatal(err)
			}
			resolveErr := <-done
			if lockErr != nil || resolveErr != nil {
				t.Fatalf("retained a higher reservation before lower fee lock: account=%d lock=%v resolve=%v", got, lockErr, resolveErr)
			}
		})
	}
}

func TestReferralClawbackMerchantForeignKeyIsLockedBeforePromoterWallet(t *testing.T) {
	for _, kind := range []string{"refund-clawback", "payment-award"} {
		t.Run(kind, func(t *testing.T) {
			s, flow, _, o := referralClawbackFixture(t, "platform")
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			if kind == "payment-award" {
				var err error
				o, err = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 902, OutTradeNo: "second-fk-payment", Name: "local lock-order payment", Type: "wxpay", Amount: 100})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = flow.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.1", ""); err != nil {
					t.Fatal(err)
				}
			}
			holder, err := s.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback(context.Background())
			var holderPID int32
			if err = holder.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&holderPID); err != nil {
				t.Fatal(err)
			}
			if _, err = holder.Exec(ctx, "SELECT id FROM merchants WHERE id=901 FOR UPDATE"); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				if kind == "refund-clawback" {
					_, e := s.ReserveRefund(ctx, 902, o.OutTradeNo, "fk-refund", 98)
					done <- e
				} else {
					done <- s.markPaid(ctx, o.TradeNo, "fk-paid-second", true, PaidReceipt{RealMoney: o.Amount, ChannelID: 7, ProviderTradeNo: "WX_SECOND_" + o.TradeNo})
				}
			}()
			for {
				var blocked int
				if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity a WHERE a.datname=current_database() AND $1=ANY(pg_blocking_pids(a.pid))", holderPID).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked > 0 {
					break
				}
				select {
				case err = <-done:
					t.Fatalf("FK unexpectedly bypassed original promoter merchant lock: %v", err)
				case <-ctx.Done():
					t.Fatal("FK domain lock never reached", ctx.Err())
				case <-time.After(5 * time.Millisecond):
				}
			}
			// Membership/deposit lock their merchant before debiting its wallet.
			// The new reward paths must obtain their unavoidable FK KEY SHARE
			// before account locks, not wait for this row while holding the wallet.
			var accountID int64
			lockErr := holder.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=901 AND kind='available' AND scope_id=0 FOR UPDATE NOWAIT").Scan(&accountID)
			if err = holder.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			resolveErr := <-done
			if lockErr != nil || resolveErr != nil {
				t.Fatalf("account->merchant FK reverses membership/deposit lock order: wallet=%d lock=%v resolve=%v", accountID, lockErr, resolveErr)
			}
		})
	}
}

func TestReferralClawbackResolutionAuditMerchantKeyPrecedesPayerWallet(t *testing.T) {
	for _, kind := range []string{"refund-resolution", "transfer-resolution"} {
		t.Run(kind, func(t *testing.T) {
			s, _, _, o := referralClawbackFixture(t, "platform")
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			var businessID int64
			if kind == "refund-resolution" {
				r, err := s.ReserveRefund(ctx, 902, o.OutTradeNo, "audit-fk-refund", 98)
				if err != nil {
					t.Fatal(err)
				}
				businessID = r.ID
			} else {
				tx, err := s.pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(context.Background())
				r, err := s.reserveTransferTx(ctx, tx, 902, "audit-fk-transfer", 10, 12, 2, 2000, "")
				if err != nil {
					t.Fatal(err)
				}
				businessID = r.ID
				if err = tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			}
			holder, err := s.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback(context.Background())
			var holderPID int32
			if err = holder.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&holderPID); err != nil {
				t.Fatal(err)
			}
			if _, err = holder.Exec(ctx, "SELECT id FROM merchants WHERE id=902 FOR UPDATE"); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				if kind == "refund-resolution" {
					done <- s.ResolveRefund(ctx, businessID, "failed")
				} else {
					done <- s.ResolveTransfer(ctx, businessID, "failed")
				}
			}()
			for {
				var blocked int
				if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity a WHERE a.datname=current_database() AND $1=ANY(pg_blocking_pids(a.pid))", holderPID).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked > 0 {
					break
				}
				select {
				case err = <-done:
					t.Fatalf("resolution skipped merchant FK: %v", err)
				case <-ctx.Done():
					t.Fatal("resolution merchant FK never reached", ctx.Err())
				case <-time.After(5 * time.Millisecond):
				}
			}
			var accountID int64
			lockErr := holder.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=902 AND kind='available' AND scope_id=0 FOR UPDATE NOWAIT").Scan(&accountID)
			if err = holder.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			resolveErr := <-done
			if lockErr != nil || resolveErr != nil {
				t.Fatalf("resolution audit FK held payer wallet before merchant: wallet=%d lock=%v resolve=%v", accountID, lockErr, resolveErr)
			}
		})
	}
}
