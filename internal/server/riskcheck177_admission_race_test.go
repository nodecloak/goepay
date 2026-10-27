package server

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestRisk177PolicyAndCheckCommitSerializeWithOriginalAdmission(t *testing.T) {
	for _, change := range []string{"first-policy", "check-action"} {
		t.Run(change, func(t *testing.T) {
			f, _, creates := risk177Flow(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			v := risk177Data(t, f.request("GET", "", f.csrf))
			if change == "check-action" {
				v = risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) {
					s["auto_check_payip"] = "1"
					s["check_payip_second"] = "3600"
					s["check_payip_count"] = "1"
				}), f.csrf))
			}
			if first := f.pay(t, "risk177-before-race", "192.0.2.177"); first["code"] != float64(1) {
				t.Fatal(first)
			}
			const gate = int64(1776496701)
			held, e := f.s.Pool.Acquire(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer held.Release()
			if _, e = held.Exec(ctx, `SELECT pg_advisory_lock($1)`, gate); e != nil {
				t.Fatal(e)
			}
			locked := true
			defer func() {
				if locked {
					held.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, gate)
				}
			}()
			if _, e = f.s.Pool.Exec(ctx, fmt.Sprintf(`CREATE FUNCTION risk177_origin_race_gate()RETURNS TRIGGER LANGUAGE plpgsql AS $$BEGIN PERFORM pg_advisory_xact_lock(%d);RETURN NEW;END$$;CREATE TRIGGER risk177_origin_race_gate BEFORE INSERT ON core_risk_origins FOR EACH ROW EXECUTE FUNCTION risk177_origin_race_gate()`, gate)); e != nil {
				t.Fatal(e)
			}
			paymentDone := make(chan map[string]any, 1)
			go func() { paymentDone <- f.pay(t, "risk177-racing-original", "192.0.2.177") }()
			observed := false
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if e = f.s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory' AND query LIKE 'INSERT INTO core_risk_origins%')`).Scan(&observed); e != nil {
					t.Fatal(e)
				}
				if observed {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !observed {
				t.Fatal("controlled last origin write was not reached")
			}
			changed := make(chan bool, 1)
			go func() {
				if change == "first-policy" {
					w := f.request("PATCH", risk177Body(t, v, func(s map[string]any) { s["pay_iplimit"] = "1" }), f.csrf)
					changed <- w.Code == 200
				} else {
					out := f.check(t, v, "risk177-concurrent-check")
					changed <- out["actions"] == float64(1)
				}
			}()
			early := false
			var ok bool
			select {
			case ok = <-changed:
				early = true
			case <-time.After(150 * time.Millisecond):
			}
			if _, e = held.Exec(ctx, `SELECT pg_advisory_unlock($1)`, gate); e != nil {
				t.Fatal(e)
			}
			locked = false
			select {
			case p := <-paymentDone:
				if p["code"] != float64(1) {
					t.Fatal("original intent lost while holding prior admission", p)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if !early {
				select {
				case ok = <-changed:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if early || !ok {
				t.Fatal("new policy/action committed ahead of an unfinished original admission", change, early, ok)
			}
			if p := f.pay(t, "risk177-after-race", "192.0.2.177"); p["code"] == float64(1) || creates.Load() != 2 {
				t.Fatal("new admission after committed restriction escaped", p["code"], creates.Load())
			}
		})
	}
}
