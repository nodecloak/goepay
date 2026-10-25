package payment

import (
	"context"
	"errors"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

func TestScanRecovery78OriginalPendingCodesDelayFirstQueryOnlyAsPro(t *testing.T) {
	for _, tt := range []struct {
		provider, code string
		milliseconds   int64
	}{{"wxpay", "USERPAYING", 5000}, {"wxpay", "SYSTEMERROR", 3000}, {"alipay", "10003", 5000}, {"alipay", "20000", 3000}} {
		t.Run(tt.provider+"/"+tt.code, func(t *testing.T) {
			s, _, o := recoveryOriginFixtureProvider78(t, tt.provider)
			ctx := context.Background()
			var a ProviderAttempt
			var request adapter.ScanRequest
			var pending error
			if tt.provider == "wxpay" {
				a, request, pending, _ = pendingReceiptProviderCodeStateFixture78(t, s, o, tt.code, func() string { return "USERPAYING" })
			} else {
				f := alipayScanPendingCodeFixture78(t, s, o, "close", tt.code)
				a, request, pending = f.attempt, f.request, f.pending
			}
			if e := s.RecordScanPending(ctx, a.ID, "app78", request, pending); e != nil {
				t.Fatal(e)
			}
			var actual int64
			if e := s.pool.QueryRow(ctx, `SELECT (extract(epoch FROM(j.run_at-e.created_at))*1000)::bigint FROM scan_recovery_eligibility e JOIN jobs j ON j.event_key='scan-recover:'||e.attempt_id::text WHERE e.attempt_id=$1`, a.ID).Scan(&actual); e != nil || actual != tt.milliseconds {
				t.Fatal("original initial two-plus-three-second rule lost", actual, tt.milliseconds, e)
			}
		})
	}
}

func TestScanRecovery78WechatNOTPAYEndsAutomaticLoopWithoutReverse(t *testing.T) {
	s, _, o := recoveryOriginFixture78(t)
	ctx := context.Background()
	a, request, pending, p := pendingReceiptProviderStateFixture78(t, s, o, func() string { return "NOTPAY" })
	if e := s.RecordScanPending(ctx, a.ID, "app78", request, pending); e != nil {
		t.Fatal(e)
	}
	job, _ := claimScanFixtureJob78(t, s)
	scope, _ := adapter.TransferAccountScope(p)
	claim, owner, e := s.ClaimScanQuery(ctx, job.ID, job.Token, scope)
	if e != nil || !owner {
		t.Fatal(e)
	}
	result, e := p.ObserveScan(ctx, adapter.QueryRequest{TradeNo: claim.TradeNo, ProviderTradeNo: claim.ProviderTradeNo})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RecordScanQueryResult(ctx, claim.ID, job.Token, result, nil); e != nil {
		t.Fatal(e)
	}
	waitScanQueryBoundary78(t, s, a.ID)
	if _, owner, e = s.ClaimScanQuery(ctx, job.ID, job.Token, scope); !errors.Is(e, ErrState) || owner {
		t.Fatal("Pro stops on NOTPAY, but another automatic query was granted", owner, e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM scan_cancellation_claims").Scan(&count); e != nil || count != 0 {
		t.Fatal("NOTPAY manufactured a reverse", count, e)
	}
}
