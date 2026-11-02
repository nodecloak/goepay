package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotice230TemplatesSaveFinalMappingAtomicallyAndRejectStaleSnapshots(t *testing.T) {
	f := channel168Local(t)
	call := func(in templateInput230) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(in)
		r := httptest.NewRequest("PUT", "/console/v1/admin/settings/notice-templates", strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	parse := func(w *httptest.ResponseRecorder) []templateRow230 {
		t.Helper()
		var out struct {
			Data []templateRow230 `json:"data"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("template HTTP %d: %s", w.Code, w.Body.String())
		}
		return out.Data
	}
	first := parse(call(templateInput230{Expected: []templateRow230{}, Values: map[string]string{"wxnotice_tpl_order": "template230", "wxnotice_tpl_order_name": "thing1", "wxnotice_tpl_order_money": "amount2"}}))
	if len(first) != 3 {
		t.Fatal("template fields not saved together")
	}
	swapped := parse(call(templateInput230{Expected: first, Values: map[string]string{"wxnotice_tpl_order_name": "amount2", "wxnotice_tpl_order_money": "thing1"}}))
	if len(swapped) != 2 {
		t.Fatal("valid mapping swap rejected")
	}
	if w := call(templateInput230{Expected: first, Values: map[string]string{"wxnotice_tpl_order": "other230"}}); w.Code != 409 {
		t.Fatal("stale template snapshot overwritten", w.Code)
	}
	var current []templateRow230
	rows, e := f.p.Query(t.Context(), "SELECT id::text,body->>'key',body->>'value' FROM backoffice_records WHERE resource='settings' AND starts_with(body->>'key','wxnotice_tpl_') ORDER BY id")
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var row templateRow230
		if e = rows.Scan(&row.ID, &row.Key, &row.Value); e != nil {
			t.Fatal(e)
		}
		current = append(current, row)
	}
	rows.Close()
	if w := call(templateInput230{Expected: current, Values: map[string]string{"wxnotice_tpl_order": "must-not-commit230", "wxnotice_tpl_order_name": "thing1"}}); w.Code != 422 {
		t.Fatal("duplicate template mapping accepted", w.Code)
	}
	var stored string
	if e = f.p.QueryRow(t.Context(), "SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='wxnotice_tpl_order'").Scan(&stored); e != nil || stored != "template230" {
		t.Fatal("invalid batch partially committed", stored, e)
	}
	cleared := parse(call(templateInput230{Expected: current, Values: map[string]string{"wxnotice_tpl_order": ""}}))
	if len(cleared) != 1 || cleared[0].Value != "" {
		t.Fatal("template ID could not be cleared")
	}
}
