package settlementscheduler

import (
	"context"
	"goepay/internal/platform/jobs"
	"log/slog"
	"time"
)

type JobQueue interface {
	Claim(context.Context, []string, time.Duration) (jobs.Job, error)
	Renew(context.Context, jobs.Job, time.Duration) error
	Complete(context.Context, jobs.Job) error
	Retry(context.Context, jobs.Job, string, time.Duration) error
}
type Runner struct {
	Scheduler                                                  Scheduler
	Queue                                                      JobQueue
	Handler                                                    Handler
	Lease, RenewInterval, TaskTimeout, TickInterval, IdleDelay time.Duration
}

func (r Runner) ProcessOne(ctx context.Context) error {
	if !r.Scheduler.Enabled {
		return ErrDisabled
	}
	if r.Queue == nil || r.Handler.Store == nil {
		return ErrUnavailable
	}
	lease, renew, timeout := r.Lease, r.RenewInterval, r.TaskTimeout
	if lease == 0 {
		lease = time.Minute
	}
	if renew == 0 {
		renew = lease / 3
	}
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	if lease < time.Second || lease > time.Hour || renew <= 0 || renew >= lease || timeout <= 0 {
		return ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	j, err := r.Queue.Claim(ctx, []string{GenerateKind}, lease)
	if err != nil {
		return err
	}
	work, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Handler.Handle(work, j) }()
	timer := time.NewTicker(renew)
	defer timer.Stop()
	for {
		select {
		case err = <-done:
			finish, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if err == nil {
				return r.Queue.Complete(finish, j)
			}
			shift := j.Attempts
			if shift < 0 {
				shift = 0
			}
			if shift > 10 {
				shift = 10
			}
			return r.Queue.Retry(finish, j, "settlement generation unresolved; query original run", time.Duration(1<<shift)*time.Second)
		case <-timer.C:
			renewTimeout := lease / 2
			if renewTimeout > 5*time.Second {
				renewTimeout = 5 * time.Second
			}
			renewCtx, stopRenew := context.WithTimeout(work, renewTimeout)
			err = r.Queue.Renew(renewCtx, j, lease)
			stopRenew()
			if err != nil {
				cancel()
				return err
			}
		case <-work.Done():
			cancel()
			finish, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			return r.Queue.Retry(finish, j, "settlement generation unresolved; query original run", time.Second)
		}
	}
}
func (r Runner) Run(ctx context.Context) error {
	if !r.Scheduler.Enabled {
		return ErrDisabled
	}
	if r.Scheduler.Store == nil || r.Queue == nil || r.Handler.Store == nil {
		return ErrUnavailable
	}
	interval, idle := r.TickInterval, r.IdleDelay
	if interval == 0 {
		interval = 30 * time.Second
	}
	if idle == 0 {
		idle = time.Second
	}
	if interval <= 0 || idle <= 0 {
		return ErrUnavailable
	}
	next := time.Time{}
	for ctx.Err() == nil {
		if !time.Now().Before(next) {
			scan, stop := context.WithTimeout(ctx, 30*time.Second)
			err := r.Scheduler.Tick(scan)
			stop()
			next = time.Now().Add(interval)
			if err != nil && ctx.Err() == nil {
				slog.Error("settlement plan scan failed")
			}
		}
		err := r.ProcessOne(ctx)
		if err == nil {
			continue
		}
		if !jobs.IsEmpty(err) && ctx.Err() == nil {
			slog.Error("settlement generation job unresolved")
		}
		timer := time.NewTimer(idle)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
	return nil
}
