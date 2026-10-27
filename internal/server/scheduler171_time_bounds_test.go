package server

import (
	"context"
	"encoding/json"
	"testing"
)

func TestScheduler171UTCNormalizationCannotPersistUnreadableYear(t *testing.T) {
	for _, at := range []string{"0001-01-01T00:00:00+08:00", "9999-12-31T23:59:59-08:00"} {
		t.Run(at, func(t *testing.T) {
			f := scheduler171Local(t)
			state := scheduler171Data(t, f.request("GET", "/referral_daily", "", f.csrf))
			raw, e := json.Marshal(map[string]any{"enabled": false, "schedule": map[string]any{"frequency": "once", "interval": 1, "execute_at": at}, "params": map[string]int{"limit": 20}, "expected_fingerprint": state["fingerprint"]})
			if e != nil {
				t.Fatal(e)
			}
			if w := f.request("PATCH", "/referral_daily", string(raw), f.csrf); w.Code != 422 {
				t.Fatal("UTC conversion persisted an unreadable policy year", w.Code)
			}
			var count int
			if e = f.s.Pool.QueryRow(context.Background(), `SELECT count(*)FROM scheduled_tasks`).Scan(&count); e != nil || count != 0 {
				t.Fatal("invalid UTC policy was committed", count, e)
			}
		})
	}
}
