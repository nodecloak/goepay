package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBusinessSMS231HTTPFullSnapshotCASAndNoSending(t *testing.T) {
	f := channel168Local(t)
	call := func(method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/admin/settings/business-sms", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	first := call("GET", "")
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	var data struct {
		Data struct {
			Revision  string            `json:"revision"`
			Templates map[string]string `json:"templates"`
		}
	}
	if json.Unmarshal(first.Body.Bytes(), &data) != nil || data.Data.Revision != "0" || len(data.Data.Templates) != 9 {
		t.Fatal("independent scene templates missing")
	}
	templates := data.Data.Templates
	templates["aliyun_balance"] = "SMS_BALANCE"
	templates["qcloud_group"] = "123456"
	templates["smsbao_complain"] = "投诉订单：{code}"
	raw, _ := json.Marshal(map[string]any{"expected_revision": "0", "templates": templates})
	saved := call("PUT", string(raw))
	if saved.Code != 200 {
		t.Fatal(saved.Code, saved.Body.String())
	}
	if w := call("PUT", string(raw)); w.Code != 409 {
		t.Fatal("stale CAS accepted", w.Code, w.Body.String())
	}
	nullRaw := strings.Replace(string(raw), `"SMS_BALANCE"`, `null`, 1)
	if w := call("PUT", nullRaw); w.Code != 400 {
		t.Fatal("null template implicitly cleared", w.Code)
	}
	delete(templates, "aliyun_balance")
	raw, _ = json.Marshal(map[string]any{"expected_revision": "1", "templates": templates})
	if w := call("PUT", string(raw)); w.Code != 400 {
		t.Fatal("partial snapshot implicitly cleared", w.Code)
	}
	var sends int
	if e := f.p.QueryRow(t.Context(), `SELECT count(*)FROM sms_test_requests134`).Scan(&sends); e != nil || sends != 0 {
		t.Fatal("saving templates sent a test SMS", e, sends)
	}
	if _, e := f.p.Exec(t.Context(), `UPDATE console_users SET enabled=false WHERE username='admin168' AND role='admin'`); e != nil {
		t.Fatal(e)
	}
	if w := call("GET", ""); w.Code == 200 {
		t.Fatal("revoked administrator retained access")
	}
}
