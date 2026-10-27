package payment

import (
	"context"
	"testing"

	notice "goepay/internal/merchantnotice"
)

func TestScheduler171AdoptedNoticeCannotBePublishedByLegacyTimer(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	for _, key := range []string{"daily_report", "balance_notice"} {
		scheduler171ConfigureManaged(t, s, a, key, false)
	}
	if e := s.CreateMerchant(ctx, Merchant{ID: 901, Name: "Synthetic notice", APIKey: "synthetic171"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"email":"scheduler171@example.test"}'::jsonb WHERE id=901`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.notice.SaveSettings(ctx, 901, 0, notice.SettingsWrite{Preferences: notice.Preferences{DailyReport: 2, Balance: 2, BalanceMoney: 100}}, notice.Actor{Role: "merchant", ID: 901, Name: "Synthetic"}); e != nil {
		t.Fatal(e)
	}
	page, e := s.notice.GenerateScheduledPage(ctx, 0, 200, 0, s)
	if e != nil {
		t.Fatal(e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, `SELECT count(*)FROM merchant_notice_timer_sources WHERE scene IN('daily_report','balance')`).Scan(&count); e != nil || count != 0 || page.Created != 0 {
		t.Fatal("disabled scheduler policy bypassed by original publisher", page, count, e)
	}
}
