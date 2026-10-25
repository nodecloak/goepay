package payment

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func dailyFutureClock70() (string, func(context.Context, pgx.Tx) (time.Time, error)) {
	at := time.Now().UTC().Add(48 * time.Hour)
	return at.In(referralBeijing70).Format("2006-01-02"), func(context.Context, pgx.Tx) (time.Time, error) { return at, nil }
}
func TestReferralDaily70TickOnceAndQueueOriginalBinding(t *testing.T) {
	s, o, promoter := dailyFixture70(t)
	ctx := context.Background()
	referralPayTestOrder(t, s, o)
	actor := TransferBatchActor{Role: "merchant", ID: promoter, Name: "local-daily-owner"}
	page, e := s.ListReferralDailyQueue(ctx, promoter, 0, 10, actor)
	if e != nil || len(page.Items) != 1 || page.Items[0].OrderID != o.ID || page.Items[0].State != "pending" || page.Items[0].PendingAmount != 10 || !page.Items[0].SourceVerified {
		t.Fatal(page, e)
	}
	raw, _ := json.Marshal(page)
	if !strings.Contains(string(raw), `"reward_amount":"10"`) || strings.Contains(string(raw), "hash") || strings.Contains(string(raw), "proof") {
		t.Fatal("daily queue money/proof serialization", string(raw))
	}
	if _, e = s.ListReferralDailyQueue(ctx, promoter+1, 0, 10, actor); !errors.Is(e, ErrInvalid) {
		t.Fatal("daily queue foreign owner", e)
	}
	future, clock := dailyFutureClock70()
	if _, e = s.TickReferralDaily(ctx, future, 10); !errors.Is(e, ErrInvalid) {
		t.Fatal("public tick accepted future date", e)
	}
	var awards atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.tickReferralDailyAt(ctx, future, 10, clock)
			if e != nil {
				t.Error(e)
				return
			}
			awards.Add(int64(r.Awarded))
		}()
	}
	wg.Wait()
	if awards.Load() != 1 {
		t.Fatal("daily tick did not pay exactly once", awards.Load())
	}
	b, e := s.Balance(ctx, promoter)
	if e != nil || b.Available != 10 {
		t.Fatal("daily original reward wrong", b, e)
	}
	page, e = s.ListReferralDailyQueue(ctx, promoter, 0, 10, actor)
	if e != nil || len(page.Items) != 1 || page.Items[0].State != "awarded" || page.Items[0].PendingAmount != 0 {
		var stored time.Time
		if err := s.pool.QueryRow(ctx, `SELECT executed_at FROM referral_daily_awards WHERE order_id=$1`, o.ID).Scan(&stored); err == nil {
			input, _ := clock(ctx, nil)
			t.Logf("timestamp precision diagnostic: stored_ns=%d clock_ns=%d", stored.Nanosecond(), input.Nanosecond())
		}
		t.Fatal(page, e)
	}
	if r, e := s.tickReferralDailyAt(ctx, future, 10, clock); e != nil || r.Awarded != 0 {
		t.Fatal("daily retry repeated award", r, e)
	}
	if e = s.markPaid(ctx, o.TradeNo, "daily70-after-award", true, PaidReceipt{ChannelID: 7, ProviderTradeNo: "LOCAL_" + o.TradeNo}); e != nil {
		t.Fatal("duplicate callback rejected valid deferred award", e)
	}
}
func TestReferralDaily70TickAuditFaultRollbackAndDisabledPreserve(t *testing.T) {
	s, o, promoter := dailyFixture70(t)
	ctx := context.Background()
	referralPayTestOrder(t, s, o)
	day, clock := dailyFutureClock70()
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET enabled=false WHERE id=$1`, promoter); e != nil {
		t.Fatal(e)
	}
	if r, e := s.tickReferralDailyAt(ctx, day, 10, clock); e != nil || r.Awarded != 0 {
		t.Fatal("disabled promoter paid", r, e)
	}
	var pending int64
	if e := s.pool.QueryRow(ctx, `SELECT balance FROM accounts WHERE owner_id=0 AND kind='referral_daily_pending' AND scope_id=$1`, o.ID).Scan(&pending); e != nil || pending != 10 {
		t.Fatal("disabled promoter liability lost", pending, e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET enabled=true WHERE id=$1`, promoter); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION fail_daily_award70() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation='awarded' THEN RAISE EXCEPTION 'synthetic daily audit fault';END IF;RETURN NEW;END $$;CREATE TRIGGER fail_daily_award70 BEFORE INSERT ON referral_daily_events FOR EACH ROW EXECUTE FUNCTION fail_daily_award70()`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.tickReferralDailyAt(ctx, day, 10, clock); e == nil {
		t.Fatal("award audit failure accepted")
	}
	var rewards int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM referral_rewards WHERE order_id=$1`, o.ID).Scan(&rewards); e != nil || rewards != 0 {
		t.Fatal("failed daily audit credited reward", rewards, e)
	}
	if e := s.pool.QueryRow(ctx, `SELECT balance FROM accounts WHERE owner_id=0 AND kind='referral_daily_pending' AND scope_id=$1`, o.ID).Scan(&pending); e != nil || pending != 10 {
		t.Fatal("failed daily audit spent pending", pending, e)
	}
	if _, e := s.pool.Exec(ctx, `DROP TRIGGER fail_daily_award70 ON referral_daily_events`); e != nil {
		t.Fatal(e)
	}
	if r, e := s.tickReferralDailyAt(ctx, day, 10, clock); e != nil || r.Awarded != 1 {
		t.Fatal("safe daily retry failed", r, e)
	}
}
