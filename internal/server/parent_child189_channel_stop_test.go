package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParentChild189ManagedAliasParticipatesInOriginalChannelStopConsole(t *testing.T) {
	setup := parent180RuntimeReady(t)
	get := func(suffix string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/console/v1/admin/channel-stops"+suffix, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: setup.f.token})
		w := httptest.NewRecorder()
		setup.f.s.Handler().ServeHTTP(w, r)
		return w
	}
	list := get("")
	var result struct {
		Data struct {
			Items []struct {
				ChannelID string `json:"channel_id"`
				Provider  string `json:"provider"`
			}
		}
	}
	if list.Code != 200 || json.Unmarshal(list.Body.Bytes(), &result) != nil {
		t.Fatal("stop catalog unavailable", list.Code)
	}
	found := false
	for _, row := range result.Data.Items {
		if row.ChannelID == setup.id && row.Provider == "wxpay" {
			found = true
		}
	}
	if !found {
		t.Fatal("managed receiving parent missing from original stop catalog")
	}
	if w := get("/" + setup.id); w.Code != 200 {
		t.Fatal("managed channel stop status missing", w.Code)
	}
	if w := get("/9223372036854775807"); w.Code != 404 {
		t.Fatal("unregistered status accepted", w.Code)
	}
	if setup.creates.Load() != 0 || setup.queries.Load() != 0 || setup.bad.Load() != 0 {
		t.Fatal("stop metadata read contacted provider")
	}
}
