package payment

import (
	"context"
	"testing"
)

func TestSettlementQuoteUsesSameBoundedBusinessDayAsReservation(t *testing.T) {
	original := testStore(t)
	settlementMerchant(t, original)
	ctx := context.Background()
	transferSetting(t, original, "settlement_daily_limit", "1")
	s := settlementClockStore(t, original, "2042-06-01T16:00:01Z")
	if _, err := original.pool.Exec(ctx, `INSERT INTO ledger_requests(kind,merchant_id,external_no,amount,created_at) VALUES('settlements',681,'different-day',100,'2042-06-02T16:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	quote, err := s.SettlementQuote(ctx, 681, 100)
	if err != nil || quote.DailyUsed != 0 || !quote.CanApply {
		t.Fatal("quote counted a future business day", quote, err)
	}
}
