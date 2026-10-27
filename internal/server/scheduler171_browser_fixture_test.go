package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScheduler171ActualHTTPBrowserContractAndFullRunID(t *testing.T) {
	f := scheduler171Local(t)
	ctx := context.Background()
	snapshots := map[string]any{}
	snapshots["unconfigured_catalog"] = scheduler171Data(t, f.request("GET", "", "", f.csrf))
	state := scheduler171Data(t, f.request("GET", "/referral_daily", "", f.csrf))
	schedule := map[string]any{"frequency": "once", "interval": 1, "execute_at": time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second).Format(time.RFC3339)}
	configure := func(enabled bool) map[string]any {
		t.Helper()
		raw, e := json.Marshal(map[string]any{"enabled": enabled, "schedule": schedule, "params": map[string]int{"limit": 20}, "expected_fingerprint": state["fingerprint"]})
		if e != nil {
			t.Fatal(e)
		}
		state = scheduler171Data(t, f.request("PATCH", "/referral_daily", string(raw), f.csrf))
		return state
	}
	configure(true)
	if _, e := f.s.Pool.Exec(ctx, `SELECT setval('scheduled_task_runs_id_seq',9007199254740992)`); e != nil {
		t.Fatal(e)
	}
	first := scheduler171Data(t, f.request("POST", "/referral_daily/run", `{"expected_fingerprint":"`+state["fingerprint"].(string)+`"}`, f.csrf))
	if first["id"] != "9007199254740993" {
		t.Fatal("actual run ID lost precision", first["id"])
	}
	tick, e := f.s.Store.RunScheduledTasksOnce(ctx, 20)
	if e != nil || tick.Completed != 1 || tick.Failed != 0 {
		t.Fatal("actual local handler", tick, e)
	}
	state = scheduler171Data(t, f.request("GET", "/referral_daily", "", f.csrf))
	snapshots["completed_task"] = state
	snapshots["completed_catalog"] = scheduler171Data(t, f.request("GET", "", "", f.csrf))
	snapshots["completed_history"] = scheduler171Data(t, f.request("GET", "/referral_daily/runs?limit=20&before_id=0", "", f.csrf))
	snapshots["saved_disabled_task"] = configure(false)
	snapshots["saved_disabled_catalog"] = scheduler171Data(t, f.request("GET", "", "", f.csrf))
	snapshots["manual_before_task"] = configure(true)
	snapshots["manual_before_catalog"] = scheduler171Data(t, f.request("GET", "", "", f.csrf))
	snapshots["manual_pending_run"] = scheduler171Data(t, f.request("POST", "/referral_daily/run", `{"expected_fingerprint":"`+state["fingerprint"].(string)+`"}`, f.csrf))
	snapshots["manual_pending_task"] = scheduler171Data(t, f.request("GET", "/referral_daily", "", f.csrf))
	snapshots["manual_pending_history"] = scheduler171Data(t, f.request("GET", "/referral_daily/runs?limit=20&before_id=0", "", f.csrf))
	raw, e := json.MarshalIndent(snapshots, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if output := os.Getenv("SCHEDULER171_BROWSER_FIXTURE_OUT"); output != "" {
		if filepath.Base(output) != "scheduler171-http-fixtures.json" {
			t.Fatal("dedicated synthetic fixture output required")
		}
		if e = os.WriteFile(output, append(raw, '\n'), 0600); e != nil {
			t.Fatal(e)
		}
	}
}
