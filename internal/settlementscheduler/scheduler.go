package settlementscheduler

import (
	"context"
	"errors"
)

var ErrDisabled = errors.New("settlement scheduler disabled")
var ErrUnavailable = errors.New("settlement scheduler unavailable")

type PlanScanner interface{ ScheduleSettlementPlans(context.Context) error }
type Scheduler struct {
	Store   PlanScanner
	Enabled bool
}

func (s Scheduler) Tick(ctx context.Context) error {
	if !s.Enabled {
		return ErrDisabled
	}
	if s.Store == nil {
		return ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.Store.ScheduleSettlementPlans(ctx)
}
