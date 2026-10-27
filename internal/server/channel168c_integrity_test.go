package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestChannel168CPreciseTargetAndConcurrentIndependentPolicyCAS(t *testing.T) {
	f := channel168Local(t)
	const id int64 = 9007199254740993
	f.s.ProviderFlow.Providers[id] = &channel168Provider{app: "precise-global-c"}
	state := channel168Data(t, f.admin("GET", "/9007199254740993/limits", "", f.csrf))
	if state["channel_id"] != "9007199254740993" {
		t.Fatal("parent ID rounded", state)
	}
	body := map[string]any{"daytop": "100", "daymaxorder": "0", "paymin": "0", "paymax": "0", "timestart": "", "timestop": "", "rate_limit_seconds": "0", "rate_limit_count": "0", "expected_fingerprint": state["fingerprint"]}
	raw, _ := json.Marshal(body)
	if w := f.admin("PATCH", "/7/limits", string(raw), f.csrf); w.Code != 409 {
		t.Fatal("foreign parent fingerprint accepted", w.Code)
	}
	done := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() { done <- f.admin("PATCH", "/9007199254740993/limits", string(raw), f.csrf) }()
	}
	wins, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		w := <-done
		switch w.Code {
		case 200:
			wins++
		case 409:
			conflicts++
		default:
			t.Fatal("CAS returned unexpected status", w.Code)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal("independent limits CAS multiple winners", wins, conflicts)
	}
	unchanged := channel168Data(t, f.admin("GET", "/7/limits", "", f.csrf))
	if unchanged["configured"] != false {
		t.Fatal("large target changed another parent", unchanged)
	}
	var policies, audits int
	if e := f.p.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM runtime_channel_limits),(SELECT count(*) FROM backoffice_audit WHERE operation='runtime-channel-limits-update')`).Scan(&policies, &audits); e != nil || policies != 1 || audits != 1 {
		t.Fatal("CAS loser left write/audit", policies, audits, e)
	}
}

func TestChannel168CConcurrentParentLastStartCannotOverbook(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	channel168CSetLimits(t, f, map[string]string{"rate_limit_seconds": "60", "rate_limit_count": "1"})
	done := make(chan map[string]any, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); done <- f.pay(t, fmt.Sprintf("global-concurrent-%d", i), "1.00", 7) }(i)
	}
	wg.Wait()
	close(done)
	wins := 0
	for out := range done {
		if out["code"] == float64(1) {
			wins++
		}
	}
	var starts int
	if e := f.p.QueryRow(ctx, `SELECT count(*) FROM provider_route_assignments WHERE channel_id=7`).Scan(&starts); e != nil || starts != 1 || wins != 1 {
		t.Fatal("global last start was overbooked", wins, starts, e)
	}
}

func TestChannel168CExplicitLimitsAuthenticateOriginalStopBeforeAssigning(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	channel168CSetLimits(t, f, map[string]string{})
	if _, e := f.p.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('system',0,'synthetic-stop-evidence','channel-stops',7,'pause','{"channel_id":"7"}')`); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "global-stop-c", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("unverifiable stop reached payment")
	}
	var starts int
	if e := f.p.QueryRow(ctx, `SELECT count(*) FROM provider_route_assignments WHERE channel_id=7`).Scan(&starts); e != nil || starts != 0 {
		t.Fatal("typed global policy skipped original stop evidence before committing assignment", starts, e)
	}
}

func TestChannel168CPolicyTargetAndStrictJSON(t *testing.T) {
	f := channel168Local(t)
	state := channel168Data(t, f.admin("GET", "/7/limits", "", f.csrf))
	fp := state["fingerprint"].(string)
	body := fmt.Sprintf(`{"daytop":"100.00","daymaxorder":"2","paymin":"0","paymax":"0","timestart":"","timestop":"","rate_limit_seconds":"30","rate_limit_count":"1","expected_fingerprint":%q}`, fp)
	for _, tc := range []struct {
		path, body, csrf string
		want             int
	}{
		{"/07/limits", body, f.csrf, 400}, {"/7/limits?channel_id=8", body, f.csrf, 400}, {"/7/limits", body, "invalid", 403}, {"/7/limits", strings.TrimSuffix(body, "}") + `,"daytop":"1"}`, f.csrf, 400}, {"/7/limits", strings.TrimSuffix(body, "}") + `,"merchant_id":"1"}`, f.csrf, 400}, {"/7/limits", strings.Replace(body, `"100.00"`, `100`, 1), f.csrf, 400}, {"/7/limits", strings.Replace(body, `"100.00"`, `null`, 1), f.csrf, 400},
		{"/7/limits", strings.Replace(body, `"100.00"`, `"-1"`, 1), f.csrf, 422}, {"/7/limits", strings.Replace(body, `"timestart":""`, `"timestart":"1"`, 1), f.csrf, 422},
	} {
		w := f.admin("PATCH", tc.path, tc.body, tc.csrf)
		if w.Code != tc.want || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("strict limits status/cache", tc.path, w.Code, tc.want)
		}
		if tc.want == 422 && strings.Contains(w.Body.String(), "retained rate") {
			t.Fatal("limits validation describes an unrelated fee field")
		}
	}
}
