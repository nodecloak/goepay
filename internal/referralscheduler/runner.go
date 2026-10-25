package referralscheduler

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/payment"
	"time"
)

type Store interface {
	TickReferralDaily(context.Context, string, int) (payment.ReferralDailyTickResult, error)
}
type DatabaseClock interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Runner struct {
	Enabled  bool
	Store    Store
	Clock    DatabaseClock
	Limit    int
	Interval time.Duration
}

func (r Runner) RunOnce(ctx context.Context) (payment.ReferralDailyTickResult, error) {
	empty := payment.ReferralDailyTickResult{OrderIDs: []string{}}
	if !r.Enabled {
		return empty, nil
	}
	if err := r.validate(); err != nil {
		return empty, err
	}
	operation, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var day string
	if err := r.Clock.QueryRow(operation, `SELECT ((pg_catalog.clock_timestamp() AT TIME ZONE 'UTC') + interval '8 hours')::date::text`).Scan(&day); err != nil {
		return empty, err
	}
	parsed, err := time.Parse("2006-01-02", day)
	if err != nil || parsed.Format("2006-01-02") != day {
		return empty, errors.New("invalid database business date")
	}
	return r.Store.TickReferralDaily(operation, day, r.Limit)
}
func (r Runner) validate() error {
	if r.Store == nil || r.Clock == nil || r.Limit < 1 || r.Limit > 100 || r.Interval < time.Second || r.Interval > 24*time.Hour {
		return errors.New("invalid daily scheduler configuration")
	}
	return nil
}

// Run requires explicit deployment opt-in and has no provider client, funds
// grant or payment credentials. Core ledger proofs and locks remain authoritative.
func (r Runner) Run(ctx context.Context) error {
	if !r.Enabled {
		return nil
	}
	if err := r.validate(); err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		if _, err := r.RunOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		timer := time.NewTimer(r.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
