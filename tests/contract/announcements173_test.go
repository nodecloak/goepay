package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
)

func TestAnnouncements173OriginalFieldsAndNoExtraRequiredTitle(t *testing.T) {
	s, p, _ := fixture(t)
	s.Sessions = identity.NewPersistentSessions(p, false)
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/admin/resources/announcements"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := request("GET", "/schema", "")
	if w.Code != 200 {
		t.Fatal("announcement schema unavailable", w.Code)
	}
	data := credentialData162(t, w)
	fields := map[string]map[string]any{}
	for _, raw := range data["fields"].([]any) {
		f := raw.(map[string]any)
		fields[f["name"].(string)] = f
	}
	if fields["title"]["required"] == true || fields["sort"]["min"] != float64(1) || fields["color"]["max_length"] != float64(7) || fields["content"]["label"] != "公告内容" {
		t.Fatal("original notice fields missing or extra title still required")
	}
	for _, body := range []string{`{"content":"<p>原公告</p>","sort":50,"color":"#336699","content_format":"html","audience":"merchant","enabled":true}`, `{"content":"纯文本兼容","audience":"all","enabled":true}`} {
		if w = request("POST", "", body); w.Code != 200 && w.Code != 201 {
			t.Fatal("original notice body rejected", w.Code)
		}
	}
	for _, body := range []string{`{"content":"bad","sort":0,"color":"#336699","audience":"merchant","enabled":true}`, `{"content":"bad","sort":1,"color":"red;xxx","audience":"merchant","enabled":true}`, `{"content":"<p><br></p>","sort":1,"color":"","content_format":"html","audience":"merchant","enabled":true}`} {
		if w = request("POST", "", body); w.Code != 422 {
			t.Fatal("invalid original notice accepted", w.Code)
		}
	}
}

func TestAnnouncements173LayoutSettingsAndRealPublicAndMerchantConsumers(t *testing.T) {
	s, p, _ := fixture(t)
	s.Sessions = identity.NewPersistentSessions(p, false)
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, path, body, role, token, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		if role != "" {
			r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	for key, value := range map[string]string{"modal": "<p>欢迎回来173</p>", "zhuce": "<p>注册公告173</p>", "footer": "<p>页脚173</p>"} {
		b, _ := json.Marshal(map[string]string{"key": key, "value": value})
		w := call("POST", "/console/v1/admin/resources/settings", string(b), "admin", token, csrf)
		if w.Code != 200 && w.Code != 201 {
			t.Fatal("original layout key not writable", key, w.Code)
		}
	}
	w := call("GET", "/console/v1/public/layout", "", "", "", "")
	if w.Code != 200 {
		t.Fatal("public layout unavailable", w.Code)
	}
	d := credentialData162(t, w)
	if d["zhuce"] != "<p>注册公告173</p>" || d["footer"] != "<p>页脚173</p>" || d["format"] != "html" {
		t.Fatal("public original layout not consumed")
	}
	if _, ok := d["modal"]; ok {
		t.Fatal("private welcome exposed publicly")
	}
	mt, mc, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	w = call("GET", "/console/v1/merchant/dashboard", "", "merchant", mt, mc)
	if w.Code != 200 || credentialData162(t, w)["welcome_notice"] != "<p>欢迎回来173</p>" {
		t.Fatal("merchant welcome not consumed", w.Code)
	}
	var id int64
	if e = p.QueryRow(context.Background(), `SELECT id FROM backoffice_records WHERE resource='settings' AND body->>'key'='modal'`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	w = call("PATCH", fmt.Sprintf("/console/v1/admin/resources/settings/%d", id), `{"value":""}`, "admin", token, csrf)
	if w.Code != 200 {
		t.Fatal("blank welcome cannot disable", w.Code)
	}
	w = call("GET", "/console/v1/merchant/dashboard", "", "merchant", mt, mc)
	if w.Code != 200 || credentialData162(t, w)["welcome_notice"] != "" {
		t.Fatal("blank welcome remained enabled", w.Code)
	}
	for _, body := range []map[string]string{{"key": "site_name", "value": ""}, {"key": "site_name", "value": strings.Repeat("a", 1001)}} {
		b, _ := json.Marshal(body)
		w = call("POST", "/console/v1/admin/resources/settings", string(b), "admin", token, csrf)
		if w.Code != 422 {
			t.Fatal("layout exception broadened other settings", w.Code)
		}
	}
	b, _ := json.Marshal(map[string]string{"key": "footer", "value": strings.Repeat("a", 1001)})
	var fid int64
	if e = p.QueryRow(context.Background(), `SELECT id FROM backoffice_records WHERE resource='settings' AND body->>'key'='footer'`).Scan(&fid); e != nil {
		t.Fatal(e)
	}
	w = call("PATCH", fmt.Sprintf("/console/v1/admin/resources/settings/%d", fid), string(b), "admin", token, csrf)
	if w.Code != 200 {
		t.Fatal("rich layout retained short generic setting bound", w.Code)
	}
	for _, path := range []string{"/console/v1/public/layout?x=1"} {
		if w = call("GET", path, "", "", "", ""); w.Code != 400 {
			t.Fatal("layout accepted query", w.Code)
		}
	}
	if w = call("POST", "/console/v1/public/layout", `{}`, "", "", ""); w.Code != 405 {
		t.Fatal("layout accepted mutation", w.Code)
	}
}

func TestAnnouncements173HomepageSortColorFormatAudienceAndLegacyPreservation(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(p, false)
	token, csrf, e := s.Sessions.Create(identity.User{ID: 1, Username: "admin", Role: "admin", AdminClass: "super", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	request := func(method, path, body, token, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		if strings.Contains(path, "/merchant/") {
			r.Header.Del("Cookie")
			r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	for _, body := range []string{`{"content":"<p>late173</p>","sort":80,"color":"#993366","content_format":"html","audience":"merchant","enabled":true}`, `{"content":"<p>early173</p>","sort":10,"color":"#336699","content_format":"html","audience":"merchant","enabled":true}`, `{"content":"admin-only173","sort":1,"color":"","content_format":"text","audience":"admin","enabled":true}`} {
		w := request("POST", "/console/v1/admin/resources/announcements", body, token, csrf)
		if w.Code != 200 && w.Code != 201 {
			t.Fatal("notice create failed", w.Code)
		}
	}
	var legacy int64
	if e = p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('announcements','{"title":"original173","content":"<b>literal173</b>","audience":"merchant","enabled":true,"unrelated":{"old":"keep"}}') RETURNING id`).Scan(&legacy); e != nil {
		t.Fatal(e)
	}
	adminList := request("GET", "/console/v1/admin/resources/announcements?limit=20", "", token, csrf)
	if adminList.Code != 200 {
		t.Fatal("admin announcement list", adminList.Code)
	}
	adminRows := credentialData162(t, adminList)["items"].([]any)
	if len(adminRows) != 4 || adminRows[0].(map[string]any)["content"] != "admin-only173" || adminRows[1].(map[string]any)["content"] != "<p>early173</p>" {
		t.Fatal("admin original announcement sort not applied")
	}
	merchantToken, merchantCSRF, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	w := request("GET", "/console/v1/merchant/dashboard", "", merchantToken, merchantCSRF)
	if w.Code != 200 {
		t.Fatal("merchant dashboard", w.Code)
	}
	rows := credentialData162(t, w)["announcements"].([]any)
	if len(rows) != 3 {
		t.Fatal("announcement audience scope", len(rows))
	}
	first, middle, last := rows[0].(map[string]any), rows[1].(map[string]any), rows[2].(map[string]any)
	if first["content"] != "<p>early173</p>" || first["color"] != "#336699" || first["content_format"] != "html" || middle["id"] != fmt.Sprint(legacy) || middle["content_format"] != "text" || last["content"] != "<p>late173</p>" {
		t.Fatal("original sort/color/legacy format not projected")
	}
	w = request("PATCH", "/console/v1/admin/resources/announcements/"+first["id"].(string), `{"enabled":false}`, token, csrf)
	if w.Code != 200 {
		t.Fatal("hide original notice", w.Code)
	}
	w = request("GET", "/console/v1/merchant/dashboard", "", merchantToken, merchantCSRF)
	rows = credentialData162(t, w)["announcements"].([]any)
	if len(rows) != 2 {
		t.Fatal("hidden notice remained visible", len(rows))
	}
	w = request("PATCH", fmt.Sprintf("/console/v1/admin/resources/announcements/%d", legacy), `{"content":"<p>edited173</p>","sort":30,"color":"#336699","content_format":"html"}`, token, csrf)
	if w.Code != 200 {
		t.Fatal("legacy safe edit", w.Code)
	}
	var title, proof string
	if e = p.QueryRow(ctx, `SELECT body->>'title',body->'unrelated'->>'old' FROM backoffice_records WHERE id=$1`, legacy).Scan(&title, &proof); e != nil || title != "original173" || proof != "keep" {
		t.Fatal("notice edit overwrote legacy fields", e)
	}
	if _, ok := adminRows[0].(map[string]any)["created_at"].(string); !ok {
		t.Fatal("original announcement timestamp missing")
	}
	if _, e = p.Exec(ctx, `INSERT INTO backoffice_records(id,resource,body) VALUES(999999999,'announcements','{"content":"tie999","sort":5,"enabled":true,"audience":"merchant"}'),(1000000000,'announcements','{"content":"tie1000","sort":5,"enabled":true,"audience":"merchant"}')`); e != nil {
		t.Fatal(e)
	}
	var lexical string
	if e = p.QueryRow(ctx, `SELECT id::text FROM (VALUES(999999999::bigint),(1000000000::bigint))t(id) ORDER BY id DESC LIMIT 1`).Scan(&lexical); e != nil || lexical != "999999999" {
		t.Fatal("numeric alias regression fixture invalid", e)
	}
	w = request("GET", "/console/v1/merchant/dashboard", "", merchantToken, merchantCSRF)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	rows = credentialData162(t, w)["announcements"].([]any)
	if rows[0].(map[string]any)["id"] != "1000000000" || rows[1].(map[string]any)["id"] != "999999999" {
		t.Fatal("same-sort announcement IDs sorted as text")
	}
}
