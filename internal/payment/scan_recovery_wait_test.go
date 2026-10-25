package payment

import (
	"context"
	"errors"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
)

type scanRecoveryWaiter78 interface {
	WaitScanRecovery(context.Context, int64, string) (Order, error)
}

func TestScanRecovery78WaitUsesOnlyOriginalTaskAndHonorsCallerDeadline(t *testing.T) {
	for _, mode := range []string{"success", "notpay", "deadline", "worker-owned", "foreign-merchant"} {
		t.Run(mode, func(t *testing.T) {
			s, _, o := recoveryOriginFixture78(t)
			ctx := context.Background()
			a, r, pending, up := pendingReceiptProviderStateFixture78(t, s, o, func() string {
				if mode == "notpay" {
					return "NOTPAY"
				}
				return "SUCCESS"
			})
			if e := s.RecordProviderAttemptResult(ctx, a.ID, "unknown", "", "", ""); e != nil {
				t.Fatal(e)
			}
			if e := s.RecordScanPending(ctx, a.ID, "app78", r, pending); e != nil {
				t.Fatal(e)
			}
			p := &observationCounter78{WechatV2: up}
			flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true}
			enableScanRecovery78(t, &flow)
			waiter, ok := any(flow).(scanRecoveryWaiter78)
			if !ok {
				t.Fatal("scan endpoint cannot await the original durable recovery task")
			}
			if mode == "worker-owned" {
				claimScanFixtureJob78(t, s)
			}
			merchant := int64(78)
			if mode == "foreign-merchant" {
				merchant = 79
			}
			if mode == "deadline" || mode == "worker-owned" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			start := time.Now()
			current, e := waiter.WaitScanRecovery(ctx, merchant, o.TradeNo)
			if mode == "success" {
				if e != nil || current.Status != "paid" || p.reads.Load() != 1 {
					t.Fatal("original query result was not returned", current.Status, e, p.reads.Load())
				}
				if _, e = waiter.WaitScanRecovery(context.Background(), 78, o.TradeNo); e != nil || p.reads.Load() != 1 {
					t.Fatal("paid wait replay repeated original query", e, p.reads.Load())
				}
			} else {
				if e == nil {
					t.Fatal("unresolved or foreign original scan reported success")
				}
				if mode == "notpay" {
					if p.reads.Load() != 1 {
						t.Fatal("authenticated notpay was not observed")
					}
				} else if p.reads.Load() != 0 {
					t.Fatal("deadline or foreign lease/merchant triggered external read", mode, p.reads.Load())
				}
				if (mode == "deadline" || mode == "worker-owned") && time.Since(start) > time.Second {
					t.Fatal("caller cancellation did not bound scan wait", time.Since(start))
				}
				if mode == "foreign-merchant" && !errors.Is(e, ErrNotFound) {
					t.Fatal("cross merchant scan exposed task", e)
				}
			}
			var state string
			var attempts int
			if e = s.pool.QueryRow(context.Background(), "SELECT state,attempts FROM jobs WHERE event_key=$1", scanRecoveryJobKey(a.ID)).Scan(&state, &attempts); e != nil {
				t.Fatal(e)
			}
			if mode == "success" || mode == "notpay" {
				if state != "succeeded" || attempts != 1 {
					t.Fatal("wait did not finish its original lease", state, attempts)
				}
			}
			if mode == "deadline" || mode == "foreign-merchant" {
				if state != "pending" || attempts != 0 {
					t.Fatal("wait changed an unclaimed task", state, attempts)
				}
			}
			if mode == "worker-owned" {
				if state != "running" || attempts != 1 {
					t.Fatal("HTTP wait stole the active worker lease", state, attempts)
				}
			}
		})
	}
}
