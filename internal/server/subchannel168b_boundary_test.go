package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func child168BCreate(t *testing.T, f channel168Fixture, app string, enabled bool) map[string]any {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"merchant_id": "1", "parent_id": "7", "name": "Synthetic child", "enabled": enabled, "weight": 1, "min_amount": "0", "max_amount": "0", "values": map[string]string{"app": app, "secret": "synthetic-only-secret"}})
	w := f.childHTTP("POST", "", string(b))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct{ Data map[string]any }
	if json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal("bad child envelope")
	}
	return out.Data
}
func TestSubchannel168BAuthenticatedStaleChildDoesNotBlockOtherCurrentChild(t *testing.T) {
	f, factory := subchannel168BLocal(t)
	child168BCreate(t, f, "old-schema", true)
	factory.schema = strings.Repeat("c", 64)
	child168BCreate(t, f, "current-schema", true)
	out := f.pay(t, "current-after-stale", "1.00", 7)
	if out["code"] != float64(1) {
		t.Fatal("an authentic stale template blocked eligible current child", out)
	}
	if factory.providers["current-schema"].sends.Load() != 1 || factory.providers["old-schema"].sends.Load() != 0 {
		t.Fatal("wrong execution")
	}
}
func TestSubchannel168BEnablingStaleAccountRequiresRealCurrentSchema(t *testing.T) {
	f, factory := subchannel168BLocal(t)
	row := child168BCreate(t, f, "stale-disabled", false)
	factory.schema = strings.Repeat("c", 64)
	b, _ := json.Marshal(map[string]any{"enabled": true, "expected_fingerprint": row["fingerprint"]})
	w := f.childHTTP("PATCH", "/"+row["id"].(string), string(b))
	if w.Code == 200 {
		t.Fatal("status claimed enabled without usable current account", w.Body.String())
	}
	var enabled bool
	if e := f.p.QueryRow(context.Background(), `SELECT enabled FROM payment_subchannels`).Scan(&enabled); e != nil || enabled {
		t.Fatal("unusable account was enabled", e)
	}
}
