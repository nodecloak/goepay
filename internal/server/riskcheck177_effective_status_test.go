package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRisk177ReadDTOSeparatesConfigurationFromEffectiveRisk(t *testing.T) {
	for _, kind := range []string{"merchant", "channel"} {
		t.Run(kind, func(t *testing.T) {
			f, _, _ := risk177Flow(t)
			v := risk177Data(t, f.request("GET", "", f.csrf))
			v = risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) {
				if kind == "merchant" {
					s["auto_check_sucrate"] = "1"
					s["check_sucrate_second"] = "3600"
					s["check_sucrate_count"] = "1"
					s["check_sucrate_value"] = "50"
				} else {
					s["auto_check_channel"] = "1"
					s["check_channel_second"] = "3600"
					s["check_channel_failcount"] = "1"
				}
			}), f.csrf))
			if p := f.pay(t, "risk177-effective-status", "192.0.2.177"); p["code"] != float64(1) {
				t.Fatal(p)
			}
			f.check(t, v, "risk177-effective-check")
			path := "/resources/merchants/1"
			if kind == "channel" {
				path = "/channel-policies/7"
			}
			r := httptest.NewRequest("GET", "/console/v1/admin"+path, nil)
			r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
			w := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatal("real read DTO failed", kind, w.Code)
			}
			var out struct{ Data map[string]any }
			if json.Unmarshal(w.Body.Bytes(), &out) != nil {
				t.Fatal("invalid JSON")
			}
			status, ok := out.Data["core_risk"].(map[string]any)
			if !ok || status["blocked"] != true || status["reason"] == "" {
				t.Fatal("read DTO incorrectly implied normal configured payment while core risk denied it", kind, out.Data["core_risk"])
			}
			if out.Data["enabled"] != true {
				t.Fatal("risk action changed original configuration flag", kind)
			}
		})
	}
}
