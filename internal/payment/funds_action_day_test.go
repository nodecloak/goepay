package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

func TestSettlementOperationDayIsChosenAfterFundsAuthorizationWait(t *testing.T) {
	original := testStore(t)
	settlementMerchant(t, original)
	ctx := context.Background()
	transferSetting(t, original, "settlement_daily_limit", "1")
	s := settlementClockStore(t, original, "2042-06-01T15:59:59Z")
	const nextDay = "2042-06-01T16:00:01Z"
	guarded := WithFundsActionGuard(ctx, func(ctx context.Context, tx pgx.Tx, kind, key string) error {
		// A credential/grant row lock can be held across midnight after the
		// account locks. Only this isolated schema's SQL clock is controlled.
		_, err := tx.Exec(ctx, "UPDATE settlement_test_clock SET moment=$1::timestamptz", nextDay)
		return err
	})
	v, err := s.RequestFunds(guarded, "settlements", 681, 0, "authorization-midnight", 100, "admin:1")
	if err != nil {
		t.Fatal(err)
	}
	wanted, _ := time.Parse(time.RFC3339, nextDay)
	if !v.CreatedAt.Equal(wanted) {
		t.Fatal("authorization wait assigned reservation to previous day", v.CreatedAt, wanted)
	}
	if _, err = s.RequestFunds(ctx, "settlements", 681, 0, "next", 100, "merchant:681"); !errors.Is(err, ErrState) {
		t.Fatal("authorization wait escaped daily quota", err)
	}
}
