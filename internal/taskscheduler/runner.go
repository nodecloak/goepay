package taskscheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"goepay/internal/payment"
)

type Store interface {
	RunScheduledTasksOnce(context.Context, int) (payment.ScheduledTaskTick, error)
}
type Runner struct {
	Enabled   bool
	Store     Store
	Limit     int
	Interval  time.Duration
	AutoQueue *payment.ProviderFlow
}

func (r Runner) validate() error {
	if r.Store == nil || r.Limit < 1 || r.Limit > 100 || r.Interval < time.Second || r.Interval > time.Hour {
		return errors.New("invalid scheduled task runtime")
	}
	return nil
}
func (r Runner) RunOnce(ctx context.Context) (payment.ScheduledTaskTick, error) {
	if !r.Enabled {
		return payment.ScheduledTaskTick{}, nil
	}
	if e := r.validate(); e != nil {
		return payment.ScheduledTaskTick{}, e
	}
	if r.AutoQueue != nil {
		store, ok := r.Store.(interface {
			RunScheduledTasksOnceWithAutoQueue(context.Context, int, *payment.ProviderFlow) (payment.ScheduledTaskTick, error)
		})
		if !ok {
			return payment.ScheduledTaskTick{}, errors.New("automatic queue runtime unavailable")
		}
		return store.RunScheduledTasksOnceWithAutoQueue(ctx, r.Limit, r.AutoQueue)
	}
	return r.Store.RunScheduledTasksOnce(ctx, r.Limit)
}
func (r Runner) Run(ctx context.Context) error {
	if !r.Enabled {
		return nil
	}
	if e := r.validate(); e != nil {
		return e
	}
	for ctx.Err() == nil {
		if _, e := r.RunOnce(ctx); e != nil && ctx.Err() == nil {
			slog.Error("scheduled task tick unresolved; original lease retained")
		}
		timer := time.NewTimer(r.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
	return nil
}
