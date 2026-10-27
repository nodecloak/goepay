package server

import (
	"context"
	"encoding/json"
	"goepay/internal/riskgeo"
	"net/netip"
	"testing"
	"time"
)

func risk177GroupBody(t *testing.T, v map[string]any, value string) string {
	t.Helper()
	b, e := json.Marshal(map[string]any{"expected_fingerprint": v["fingerprint"], "config": map[string]string{"pay_block_oversea": value, "check_pay_regoin": ""}})
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestRisk177EffectiveMembershipAndLastOriginWriteExpiry(t *testing.T) {
	for _, phase := range []string{"active", "expired-before", "last-write"} {
		t.Run(phase, func(t *testing.T) {
			f, _, creates := risk177Flow(t)
			ctx := context.Background()
			f.s.Store.CoreRiskGeo = risk177GeoFunc(func(netip.Addr) (riskgeo.Region, bool, error) { return riskgeo.Region{Country: "美国"}, true, nil })
			global := risk177Data(t, f.request("GET", "", f.csrf))
			risk177Data(t, f.request("PATCH", risk177Body(t, global, func(s map[string]any) { s["pay_block_oversea"] = "1" }), f.csrf))
			const gid = int64(9007199254741773)
			if _, e := f.s.Pool.Exec(ctx, `INSERT INTO backoffice_records(id,resource,body)VALUES($1,'groups','{"name":"Synthetic VIP","enabled":true,"info":{}}')`, gid); e != nil {
				t.Fatal(e)
			}
			path := "/groups/9007199254741773"
			g := risk177Data(t, f.riskPath("GET", path, ""))
			risk177Data(t, f.riskPath("PATCH", path, risk177GroupBody(t, g, "0")))
			duration := "1 minute"
			if phase == "expired-before" {
				duration = "-1 second"
			}
			if phase == "last-write" {
				duration = "2 seconds"
				if _, e := f.s.Pool.Exec(ctx, `CREATE FUNCTION risk177_member_expiry_wait()RETURNS TRIGGER LANGUAGE plpgsql AS $$BEGIN PERFORM pg_sleep(2.5);RETURN NEW;END$$;CREATE TRIGGER risk177_member_expiry_wait BEFORE INSERT ON core_risk_origins FOR EACH ROW EXECUTE FUNCTION risk177_member_expiry_wait()`); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := f.s.Pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at)VALUES(1,$1,clock_timestamp()+$2::interval)`, gid, duration); e != nil {
				t.Fatal(e)
			}
			done := make(chan map[string]any, 1)
			go func() { done <- f.pay(t, "risk177-membership-window", "192.0.2.177") }()
			if phase == "last-write" {
				observed := false
				limit := time.Now().Add(3 * time.Second)
				for time.Now().Before(limit) {
					if e := f.s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event='PgSleep' AND query LIKE 'INSERT INTO core_risk_origins%')`).Scan(&observed); e != nil {
						t.Fatal(e)
					}
					if observed {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !observed {
					t.Fatal("actual controlled last origin write was not reached before membership expired")
				}
			}
			var p map[string]any
			select {
			case p = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("controlled membership payment timed out")
			}
			if phase == "active" {
				if p["code"] != float64(1) || creates.Load() != 1 {
					t.Fatal("current VIP override not consumed", p["code"], creates.Load())
				}
				var captured string
				if e := f.s.Pool.QueryRow(ctx, `SELECT facts#>>'{Group,GroupID}'FROM core_risk_origins`).Scan(&captured); e != nil || captured != "9007199254741773" {
					t.Fatal("membership group ID lost in source", captured, e)
				}
			} else {
				if p["code"] == float64(1) || creates.Load() != 0 {
					t.Fatal("expired membership retained oversea exception", phase, p["code"], creates.Load())
				}
				var attempts, origins, reconcile int
				if e := f.s.Pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM provider_attempts),(SELECT count(*)FROM core_risk_origins),(SELECT count(*)FROM jobs WHERE kind='payment.reconcile')`).Scan(&attempts, &origins, &reconcile); e != nil || attempts != 0 || origins != 0 || reconcile != 0 {
					t.Fatal("expired intent committed partial risk or send facts", attempts, origins, reconcile, e)
				}
			}
			var info string
			if e := f.s.Pool.QueryRow(ctx, `SELECT(body->'info')::text FROM backoffice_records WHERE id=$1`, gid).Scan(&info); e != nil || info != "{}" {
				t.Fatal("risk inheritance changed original strict payment info", info, e)
			}
		})
	}
}
func TestRisk177OriginalGroupOverseaInheritsAndOverridesCurrentGlobalPolicy(t *testing.T) {
	f, _, creates := risk177Flow(t)
	f.s.Store.CoreRiskGeo = risk177GeoFunc(func(netip.Addr) (riskgeo.Region, bool, error) {
		return riskgeo.Region{Country: "美国", Province: "合成省", City: "合成市"}, true, nil
	})
	v := risk177Data(t, f.riskPath("GET", "/groups/0", ""))
	if v["group_id"] != "0" || v["configured"] != false || v["effective_oversea"] != "0" {
		t.Fatal("original default group sentinel lost", v)
	}
	v = risk177Data(t, f.riskPath("PATCH", "/groups/0", risk177GroupBody(t, v, "1")))
	if v["revision"] != "1" || v["effective_oversea"] != "1" {
		t.Fatal(v)
	}
	if p := f.pay(t, "risk177-group-forbid", "192.0.2.177"); p["code"] == float64(1) || creates.Load() != 0 {
		t.Fatal("saved current group override did not consume local country", p["code"], creates.Load())
	}
	old := risk177GroupBody(t, v, "0")
	v = risk177Data(t, f.riskPath("PATCH", "/groups/0", old))
	if p := f.pay(t, "risk177-group-allow", "192.0.2.177"); p["code"] != float64(1) || creates.Load() != 1 {
		t.Fatal("explicit group0 did not override global", p)
	}
	if w := f.riskPath("PATCH", "/groups/0", old); w.Code != 409 {
		t.Fatal("stale group risk CAS accepted", w.Code)
	}
	v = risk177Data(t, f.riskPath("PATCH", "/groups/0", risk177GroupBody(t, v, "_")))
	if v["config"].(map[string]any)["pay_block_oversea"] != "" {
		t.Fatal("original UI underscore did not normalize to inherited empty")
	}
	global := risk177Data(t, f.request("GET", "", f.csrf))
	risk177Data(t, f.request("PATCH", risk177Body(t, global, func(s map[string]any) { s["pay_block_oversea"] = "1" }), f.csrf))
	if w := f.riskPath("PATCH", "/groups/0", risk177GroupBody(t, v, "0")); w.Code != 409 {
		t.Fatal("group confirmation ignored changed global inheritance", w.Code)
	}
	v = risk177Data(t, f.riskPath("GET", "/groups/0", ""))
	if v["effective_oversea"] != "1" {
		t.Fatal("empty group did not inherit current global", v)
	}
	if p := f.pay(t, "risk177-group-inherited-forbid", "192.0.2.177"); p["code"] == float64(1) {
		t.Fatal("global inheritance was display metadata")
	}
	v = risk177Data(t, f.riskPath("PATCH", "/groups/0", risk177GroupBody(t, v, "0")))
	if p := f.pay(t, "risk177-group-explicit-allow", "192.0.2.177"); p["code"] != float64(1) || creates.Load() != 2 {
		t.Fatal("explicit0 failed to override global1", p["code"], creates.Load())
	}
}
