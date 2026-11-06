package payment

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

func TestRefundAttestationEligibilityDoesNotReenterPool(t *testing.T) {
	s := testStore(t)
	cfg := s.pool.Config()
	cfg.MaxConns = 1
	single, e := pgxpool.NewWithConfig(t.Context(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer single.Close()
	s.pool = single
	tx, e := single.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	target := refundAttestationTarget{refund: Refund{OrderID: 1, MerchantID: 1000, Status: "unknown"}, tradeNo: "missing-account", operationID: 1, channelID: 1, opState: "unknown"}
	_, _, e = (ProviderFlow{Store: s}).refundAttestationEligibility(ctx, tx, target)
	if ctx.Err() != nil || single.Stat().CanceledAcquireCount() != 0 {
		t.Fatalf("transaction reacquired its own connection: canceled=%d ctx=%v err=%v", single.Stat().CanceledAcquireCount(), ctx.Err(), e)
	}
	if e == nil {
		t.Fatal("unverifiable payment account was treated as eligible")
	}
}
