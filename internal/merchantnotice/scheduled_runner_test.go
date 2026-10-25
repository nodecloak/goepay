package merchantnotice

import (
	"context"
	"testing"
	"time"
)

type scheduledGenerator80 struct{ cursors []int64 }

func (g *scheduledGenerator80) GenerateScheduledPage(_ context.Context, cursor int64, limit, hour int, _ TransferReportReader) (ScheduledPage, error) {
	g.cursors = append(g.cursors, cursor)
	if cursor == 0 {
		return ScheduledPage{LastMerchantID: 250, Scanned: 200, Created: 4}, nil
	}
	return ScheduledPage{LastMerchantID: 251, Scanned: 1, Created: 1}, nil
}
func TestMerchantNotice80ScheduledRunnerDisabledAndAscendingBoundedPages(t *testing.T) {
	if _, err := (ScheduledRunner{}).RunOnce(context.Background()); err != nil {
		t.Fatal("disabled runner touched configuration", err)
	}
	generator := &scheduledGenerator80{}
	total, err := (ScheduledRunner{Enabled: true, Store: generator, SendHour: 9, Interval: time.Minute}).RunOnce(context.Background())
	if err != nil || total != 5 || len(generator.cursors) != 2 || generator.cursors[0] != 0 || generator.cursors[1] != 250 {
		t.Fatal("wrong bounded scheduler cursor", total, generator.cursors, err)
	}
}
