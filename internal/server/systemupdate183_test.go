package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSystemUpdate183ActualConsoleHasVersionContractWithoutConfiguredSource(t *testing.T) {
	f := channel168Local(t)
	r := httptest.NewRequest(http.MethodGet, "/console/v1/admin/system-update", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatal("original system-update page has no actual version contract", w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			CurrentVersion string `json:"current_version"`
			LatestVersion  string `json:"latest_version"`
			NeedUpdate     bool   `json:"need_update"`
			CheckStatus    string `json:"check_status"`
			CanPrepare     bool   `json:"can_prepare"`
		}
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.CurrentVersion == "" || out.Data.CheckStatus != "unconfigured" || out.Data.NeedUpdate || out.Data.CanPrepare {
		t.Fatal("unconfigured source must report real build and truthful unavailable preparation", w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("version/check context cached")
	}
}

func TestSystemUpdate183PrepareUnconfiguredRejectsFactsRatherThanMissingAction(t *testing.T) {
	f := channel168Local(t)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/admin/system-update"+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.csrf)
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	w := request("GET", "", "")
	var out struct {
		Data struct {
			Fingerprint string `json:"fingerprint"`
		}
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Data.Fingerprint) != 64 {
		t.Fatal("real check fingerprint unavailable", w.Code)
	}
	raw, _ := json.Marshal(map[string]string{"expected_fingerprint": out.Data.Fingerprint, "intent": out.Data.Fingerprint})
	w = request("POST", "/prepare", string(raw))
	if w.Code != 409 {
		t.Fatal("preparation action missing or unconfigured update falsely accepted", w.Code, w.Body.String())
	}
	var events int
	if e := f.p.QueryRow(t.Context(), `SELECT count(*) FROM backoffice_audit WHERE resource='system-update183'`).Scan(&events); e != nil || events != 0 {
		t.Fatal("unconfigured update left persistent preparation", events, e)
	}
}
