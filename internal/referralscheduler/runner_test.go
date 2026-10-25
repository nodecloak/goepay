package referralscheduler

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/payment"
	"testing"
	"time"
)

type clockStub struct {
	date  string
	err   error
	calls int
	sql   string
}
type dateRow struct {
	date string
	err  error
}

func (d dateRow) Scan(dest ...any) error {
	if d.err != nil {
		return d.err
	}
	*(dest[0].(*string)) = d.date
	return nil
}
func (c *clockStub) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	c.calls++
	c.sql = query
	return dateRow{c.date, c.err}
}

type storeStub struct {
	calls  int
	date   string
	limit  int
	err    error
	cancel context.CancelFunc
}

func (s *storeStub) TickReferralDaily(_ context.Context, date string, limit int) (payment.ReferralDailyTickResult, error) {
	s.calls++
	s.date = date
	s.limit = limit
	if s.cancel != nil {
		s.cancel()
	}
	return payment.ReferralDailyTickResult{Examined: 1, Awarded: 1, OrderIDs: []string{"9007199254740993123"}}, s.err
}

func TestDisabledRunnerDoesNoDatabaseOrMoneyWork(t *testing.T) {
	r := Runner{}
	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestRunnerUsesDatabaseBusinessDateAndBoundedTick(t *testing.T) {
	clock := &clockStub{date: "2000-01-02"}
	store := &storeStub{}
	r := Runner{Enabled: true, Store: store, Clock: clock, Limit: 20, Interval: time.Second}
	result, err := r.RunOnce(context.Background())
	if err != nil || store.calls != 1 || store.date != "2000-01-02" || store.limit != 20 || result.Awarded != 1 || clock.calls != 1 {
		t.Fatalf("database date not respected: result=%+v store=%+v err=%v", result, store, err)
	}
}
func TestRunnerFailsClosedBeforeTickOnClockErrorAndInvalidBounds(t *testing.T) {
	for _, tc := range []struct {
		date     string
		limit    int
		clockErr error
	}{{"bad", 20, nil}, {"2026-02-29", 20, nil}, {"2026-10-24", 0, nil}, {"2026-10-24", 101, nil}, {"2026-10-24", 20, errors.New("database unavailable")}} {
		clock := &clockStub{date: tc.date, err: tc.clockErr}
		store := &storeStub{}
		if _, err := (Runner{Enabled: true, Clock: clock, Store: store, Limit: tc.limit, Interval: time.Second}).RunOnce(context.Background()); err == nil || store.calls != 0 {
			t.Fatalf("unsafe tick invoked: %+v", tc)
		}
	}
}
func TestRunnerStopsAfterCancellationAndDoesNotRetryFailedTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &storeStub{cancel: cancel}
	clock := &clockStub{date: "2026-10-24"}
	r := Runner{Enabled: true, Clock: clock, Store: store, Limit: 20, Interval: time.Second}
	if err := r.Run(ctx); err != nil || store.calls != 1 {
		t.Fatal("cancelled scheduler did extra work", store.calls, err)
	}
	expected := errors.New("proof verification failed")
	store = &storeStub{err: expected}
	r.Store = store
	if err := r.Run(context.Background()); !errors.Is(err, expected) || store.calls != 1 {
		t.Fatal("failed funded operation retried", store.calls, err)
	}
}
