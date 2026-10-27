package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func site191Save(t *testing.T, f channel168Fixture, key, value string) {
	t.Helper()
	if _, err := f.p.Exec(t.Context(), `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'=$1`, key); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"key": key, "value": value})
	if _, err := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body)VALUES('settings',$1::jsonb)`, string(raw)); err != nil {
		t.Fatal(err)
	}
}
func site191Request(f channel168Fixture, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}
func TestSite191PublicOriginalFieldsChangeWithoutExposingOtherSettings(t *testing.T) {
	f := channel168Local(t)
	site191Save(t, f, "site_name", "Legacy name")
	site191Save(t, f, "support_email", "legacy@example.test")
	site191Save(t, f, "sitename", "原网站191")
	site191Save(t, f, "title", "原网站标题191")
	site191Save(t, f, "email", "contact@example.test")
	site191Save(t, f, "login_subtitle", "欢迎登录191")
	site191Save(t, f, "captcha_key", "synthetic-private191")
	w := site191Request(f, "GET", "/console/v1/public/site")
	if w.Code != 200 {
		t.Fatal("public site settings unavailable", w.Code)
	}
	var body struct {
		Code int
		Data map[string]any
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Code != 0 {
		t.Fatal("site response", err)
	}
	for key, value := range map[string]string{"sitename": "原网站191", "title": "原网站标题191", "email": "contact@example.test", "login_subtitle": "欢迎登录191"} {
		if body.Data[key] != value {
			t.Fatal("original field not consumed", key, body.Data[key])
		}
	}
	if strings.Contains(w.Body.String(), "synthetic-private191") || strings.Contains(w.Body.String(), "captcha_key") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("public site leaked unrelated private config or cached stale branding")
	}
	site191Save(t, f, "sitename", "原网站已修改191")
	if w = site191Request(f, "GET", "/console/v1/public/site"); w.Code != 200 || !strings.Contains(w.Body.String(), "原网站已修改191") {
		t.Fatal("updated site not immediately consumed", w.Code)
	}
	for _, path := range []string{"/console/v1/public/site?", "/console/v1/public/site?key=captcha_key"} {
		if w = site191Request(f, "GET", path); w.Code != 400 {
			t.Fatal("query expanded public allowlist", path, w.Code)
		}
	}
	if w = site191Request(f, "POST", "/console/v1/public/site"); w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatal("public method policy", w.Code)
	}
}

func TestSite191OriginalFourHomepageModesAndEscaping(t *testing.T) {
	f := channel168Local(t)
	site191Save(t, f, "sitename", "支付<191>")
	site191Save(t, f, "title", "标题<191>")
	site191Save(t, f, "homepage_url", "https://homepage.example.invalid/a?x=1&y=2")
	for _, mode := range []string{"0", "1", "2", "3"} {
		site191Save(t, f, "homepage", mode)
		w := site191Request(f, "GET", "/")
		switch mode {
		case "0":
			if w.Code != 200 || !strings.Contains(w.Body.String(), "标题&lt;191&gt;") || strings.Contains(w.Body.String(), "<191>") {
				t.Fatal("built-in home not branded/escaped", w.Code)
			}
		case "1":
			if w.Code != 302 || w.Header().Get("Location") != "/merchant/dashboard" {
				t.Fatal("merchant homepage mode", w.Code, w.Header().Get("Location"))
			}
		case "2":
			if w.Code != 200 || !strings.Contains(w.Body.String(), "<iframe") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-src https://homepage.example.invalid") {
				t.Fatal("custom frame homepage", w.Code)
			}
		case "3":
			if w.Code != 200 || w.Body.Len() != 0 {
				t.Fatal("blank home mode", w.Code)
			}
		}
	}
	site191Save(t, f, "homepage", "2")
	site191Save(t, f, "homepage_url", "javascript:alert(191)")
	if w := site191Request(f, "GET", "/"); w.Code != 503 || strings.Contains(w.Body.String(), "javascript:") {
		t.Fatal("unsafe historical homepage config executed", w.Code)
	}
}

func TestSite191DefaultsLegacyFallbackClearingAndMalformedOptionalValue(t *testing.T) {
	f := channel168Local(t)
	first := channel168Data(t, site191Request(f, "GET", "/console/v1/public/site"))
	if first["sitename"] != "GOEPAY" || first["homepage"] != "0" {
		t.Fatal("original unset site defaults", first)
	}
	exportSite191DefaultFixture(t, f, first)
	site191Save(t, f, "site_name", "Legacy191")
	site191Save(t, f, "support_email", "old191@example.test")
	legacy := channel168Data(t, site191Request(f, "GET", "/console/v1/public/site"))
	if legacy["sitename"] != "Legacy191" || legacy["email"] != "old191@example.test" {
		t.Fatal("existing aliases no longer effective", legacy)
	}
	site191Save(t, f, "email", "")
	cleared := channel168Data(t, site191Request(f, "GET", "/console/v1/public/site"))
	if cleared["email"] != "" {
		t.Fatal("cleared original email fell back to old contact", cleared)
	}
	if _, err := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"title","value":null}')`); err != nil {
		t.Fatal(err)
	}
	if w := site191Request(f, "GET", "/console/v1/public/site"); w.Code != 503 {
		t.Fatal("non-string setting silently became empty", w.Code)
	}
}
