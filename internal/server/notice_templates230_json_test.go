package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotice230NullTemplateIsNotAnExplicitClear(t *testing.T) {
	f := channel168Local(t)
	var id string
	if e := f.p.QueryRow(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"wxnotice_tpl_order","value":"template230"}') RETURNING id::text`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	expected, _ := json.Marshal([]templateRow230{{ID: id, Key: "wxnotice_tpl_order", Value: "template230"}})
	for _, body := range []string{
		`{"expected":` + string(expected) + `,"values":{"wxnotice_tpl_order":null}}`,
		`{"expected":[{"id":"` + id + `","key":"wxnotice_tpl_order"}],"values":{"wxnotice_tpl_order":""}}`,
		`{"expected":` + string(expected) + `,"values":{"wxnotice_tpl_order":false}}`,
	} {
		r := httptest.NewRequest("PUT", "/console/v1/admin/settings/notice-templates", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("malformed template edit returned %d", w.Code)
		}
	}
	var value string
	if e := f.p.QueryRow(t.Context(), "SELECT body->>'value' FROM backoffice_records WHERE id=$1", id).Scan(&value); e != nil || value != "template230" {
		t.Fatal("malformed template edit changed data", e)
	}
}
