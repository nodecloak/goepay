package merchantnotice

import (
	"context"
	"time"
)

type ScheduledGenerator interface {
	GenerateScheduledPage(context.Context, int64, int, int, TransferReportReader) (ScheduledPage, error)
}
type ScheduledRunner struct {
	Enabled  bool
	Store    ScheduledGenerator
	Reader   TransferReportReader
	SendHour int
	Interval time.Duration
}

func (r ScheduledRunner) validate() error {
	if r.Store == nil || r.SendHour < 0 || r.SendHour > 23 || r.Interval < time.Second || r.Interval > 24*time.Hour {
		return ErrInvalid
	}
	return nil
}
func (r ScheduledRunner) RunOnce(ctx context.Context) (int, error) {
	if !r.Enabled {
		return 0, nil
	}
	if ctx == nil {
		return 0, ErrInvalid
	}
	if err := r.validate(); err != nil {
		return 0, err
	}
	total := 0
	for cursor := int64(0); ; {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		pageContext, cancel := context.WithTimeout(ctx, 30*time.Second)
		page, err := r.Store.GenerateScheduledPage(pageContext, cursor, 200, r.SendHour, r.Reader)
		cancel()
		if err != nil {
			return total, err
		}
		if page.Scanned < 0 || page.Scanned > 200 || page.Created < 0 || page.Created > 3*page.Scanned || page.LastMerchantID < cursor || page.Scanned > 0 && page.LastMerchantID <= cursor {
			return total, ErrState
		}
		total += page.Created
		if page.Scanned < 200 {
			return total, nil
		}
		cursor = page.LastMerchantID
	}
}
func (r ScheduledRunner) Run(ctx context.Context) error {
	if !r.Enabled {
		return nil
	}
	if ctx == nil {
		return ErrInvalid
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
