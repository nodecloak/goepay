package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Public HTTP DTOs from a migrated synthetic fixture; never sessions or keys.
func TestRisk177ExportPublicUIFixture(t *testing.T) {
	f, _, _ := risk177Flow(t)
	f.s.Store.CoreRiskGeo = risk177GeoFunc(risk177ScanResolver)
	ctx := context.Background()
	if _, e := f.s.Pool.Exec(ctx, `SELECT setval(pg_get_serial_sequence('core_risk_actions','id'),9007199254741774,false);SELECT setval(pg_get_serial_sequence('orders','id'),9007199254741775,false)`); e != nil {
		t.Fatal(e)
	}
	v := risk177Data(t, f.request("GET", "", f.csrf))
	v = risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) {
		s["auto_check_payip"] = "1"
		s["check_payip_second"] = "3600"
		s["check_payip_count"] = "1"
		s["check_pay_regoin"] = "2"
	}), f.csrf))
	if p := f.pay(t, "risk177-ui-public-birth", "192.0.2.177"); p["code"] != float64(1) {
		t.Fatal(p)
	}
	f.check(t, v, "risk177-ui-public-check")
	g := risk177Data(t, f.riskPath("GET", "/groups/0", ""))
	fixtures := map[string]any{"policy": v, "actions": risk177Data(t, f.riskPath("GET", "/actions", "")), "group": g}
	raw, e := json.MarshalIndent(fixtures, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	for _, private := range []string{"proof_hash", "password", "session_token", "csrf_token", "payload_cipher", "subject_key"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private data in public fixture", private)
		}
	}
	target := os.Getenv("GOEPAY_RISK177_UI_FIXTURE")
	if target == "" {
		target = filepath.Join(t.TempDir(), "public-fixture.json")
	} else {
		absolute, e := filepath.Abs(target)
		if e != nil {
			t.Fatal(e)
		}
		allowed := "e:/goepay/.tools/work-riskcheck177-39cdd806-b87f-4ff1-b6c1-f3d886c89eaa/web/.tools/"
		if !strings.HasPrefix(strings.ToLower(filepath.ToSlash(absolute)), allowed) {
			t.Fatal("owned fixture directory required")
		}
	}
	if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(target, raw, 0600); e != nil {
		t.Fatal(e)
	}
}
