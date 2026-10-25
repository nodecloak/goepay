package payment

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
)

type scanRecoveryExecutor78 interface {
	ExecuteScanRecovery(context.Context, int64, string) (time.Duration, error)
}

type observationCounter78 struct {
	*adapter.WechatV2
	reads atomic.Int32
	lost  bool
}

func (p *observationCounter78) ObserveScan(ctx context.Context, r adapter.QueryRequest) (adapter.QueryResult, error) {
	p.reads.Add(1)
	if p.lost {
		return adapter.QueryResult{}, errors.New("synthetic-local-read-error78")
	}
	return p.WechatV2.ObserveScan(ctx, r)
}

func TestScanRecovery78ExecuteOneLeasedObservationAndSettleOnlyAuthenticatedSuccess(t *testing.T) {
	for _, mode := range []string{"SUCCESS", "USERPAYING", "NOTPAY", "read-error", "disabled", "wrong-lease"} {
		t.Run(mode, func(t *testing.T) {
			s, _, o := recoveryOriginFixture78(t)
			ctx := context.Background()
			a, r, pending, up := pendingReceiptProviderStateFixture78(t, s, o, func() string {
				if mode == "SUCCESS" || mode == "NOTPAY" {
					return mode
				}
				return "USERPAYING"
			})
			if e := s.RecordProviderAttemptResult(ctx, a.ID, "unknown", "", "", ""); e != nil {
				t.Fatal(e)
			}
			if e := s.RecordScanPending(ctx, a.ID, "app78", r, pending); e != nil {
				t.Fatal(e)
			}
			p := &observationCounter78{WechatV2: up, lost: mode == "read-error"}
			flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true}
			if mode != "disabled" {
				enableScanRecovery78(t, &flow)
			}
			executor, ok := any(flow).(scanRecoveryExecutor78)
			if !ok {
				t.Fatal("durable scan recovery has no bounded execution flow")
			}
			job, _ := claimScanFixtureJob78(t, s)
			token := job.Token
			if mode == "wrong-lease" {
				token = "00000000000000000000000000000000"
			}
			delay, e := executor.ExecuteScanRecovery(ctx, job.ID, token)
			if mode == "disabled" || mode == "wrong-lease" {
				if e == nil || p.reads.Load() != 0 {
					t.Fatal("unadmitted recovery performed network I/O", delay, e, p.reads.Load())
				}
				return
			}
			if e != nil || p.reads.Load() != 1 {
				t.Fatal("original leased read missing", delay, e, p.reads.Load())
			}
			if mode == "USERPAYING" {
				if delay <= 0 || delay > 3*time.Second {
					t.Fatal("pending query omitted original interval", delay)
				}
			} else if delay != 0 {
				t.Fatal("terminal observation scheduled another query", delay)
			}
			if _, e = executor.ExecuteScanRecovery(ctx, job.ID, token); e != nil || p.reads.Load() != 1 {
				t.Fatal("immediate replay repeated read or financial I/O", e, p.reads.Load())
			}
			current, e := s.OrderByTradeNo(ctx, o.TradeNo)
			balance, be := s.Balance(ctx, 78)
			wantStatus, wantBalance := "unknown", int64(0)
			if mode == "SUCCESS" {
				wantStatus, wantBalance = "paid", 123
			}
			if e != nil || be != nil || current.Status != wantStatus || balance.Available != wantBalance || balance.Reserved != 0 {
				t.Fatal("query settled wrong original outcome", current.Status, balance, e, be)
			}
			var cancellations, reads int
			if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM scan_cancellation_claims),(SELECT count(*) FROM scan_recovery_query_events)`).Scan(&cancellations, &reads); e != nil || cancellations != 0 || reads != 2 {
				t.Fatal("single query generated cancellation or duplicate evidence", cancellations, reads, e)
			}
			if invalid, unknown := scanLedgerCounters78(t, s); invalid != 0 || unknown != 0 {
				t.Fatal("legitimate read flow damaged original source", invalid, unknown)
			}
		})
	}
}
