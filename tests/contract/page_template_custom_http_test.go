package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/pagetemplate"
)

// The custom home template (自定义 HTML) over HTTP: only the super
// administrator's own session saves versions; the public page serves the
// sanitized HTML under the unchanged CSP; images become public only while the
// selected custom page shows them; pay pages never take the HTML; every
// version enqueues exactly one administrator notice and nothing financial.
func TestCustomHomeHTMLConsoleVersionsPublicPageAndNoPayPageEffect(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	admin, csrf, e := s.Sessions.Create(identity.User{Role: "admin", AdminClass: "super", ID: 1, Username: "admin", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	operator, operatorCSRF, e := s.Sessions.Create(identity.User{Role: "admin", AdminClass: "operator", ID: 2, Username: "operator", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	tls := httptest.NewTLSServer(s.Handler())
	defer tls.Close()
	call := func(method, path, token, cs, contentType string, body []byte) (int, http.Header, []byte) {
		t.Helper()
		r, e := http.NewRequest(method, tls.URL+path, bytes.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		}
		if cs != "" {
			r.Header.Set("X-CSRF-Token", cs)
		}
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		res, e := tls.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b, e := io.ReadAll(res.Body)
		if e != nil {
			t.Fatal(e)
		}
		return res.StatusCode, res.Header, b
	}
	jsonCall := func(method, path string, body string, want int) []byte {
		t.Helper()
		code, _, b := call(method, path, admin, csrf, "application/json", []byte(body))
		if code != want {
			t.Fatalf("%s %s status %d want %d: %s", method, path, code, want, b)
		}
		return b
	}
	base := "/console/v1/admin/templates/home/custom"
	// A session that is not the super administrator's own is refused (401
	// before the console route when the account does not hold the session).
	if code, _, _ := call("GET", base+"/html", operator, "", "", nil); code != 401 && code != 403 {
		t.Fatal("operator read custom HTML", code)
	}
	if code, _, _ := call("PUT", base+"/html", operator, operatorCSRF, "application/json", []byte(`{"expected_revision":"0","html":"<p>x</p>","css":"","frame":true}`)); code != 401 && code != 403 {
		t.Fatal("operator saved custom HTML", code)
	}
	if code, _, _ := call("PUT", base+"/html", admin, "", "application/json", []byte(`{"expected_revision":"0","html":"<p>x</p>","css":"","frame":true}`)); code != 403 {
		t.Fatal("custom HTML saved without CSRF", code)
	}
	var empty struct {
		Data pagetemplate.CustomView `json:"data"`
	}
	if json.Unmarshal(jsonCall("GET", base+"/html", "", 200), &empty) != nil || empty.Data.Version != 0 || !empty.Data.Frame {
		t.Fatal("empty custom page", empty)
	}
	var sample struct {
		Data struct {
			HTML  string `json:"html"`
			Frame bool   `json:"frame"`
		} `json:"data"`
	}
	if json.Unmarshal(jsonCall("GET", base+"/sample", "", 200), &sample) != nil || !strings.Contains(sample.Data.HTML, "id=\"faq\"") || !sample.Data.Frame {
		t.Fatal("sample", sample)
	}
	jsonCall("PUT", "/console/v1/admin/templates/home/selection", `{"expected_revision":"0","name":"custom"}`, 422)
	jsonCall("PUT", base+"/html", `{"expected_revision":"0","html":"<p>x</p>","css":"","frame":true,"frame":false}`, 400)
	jsonCall("PUT", base+"/html", `{"expected_revision":"0","html":"<p>x</p>","css":""}`, 400)
	jsonCall("PUT", base+"/html", `{"expected_revision":"0","html":"<p>x</p>","css":"","frame":"yes"}`, 422)
	refused := jsonCall("PUT", base+"/html", `{"expected_revision":"0","html":"<p>x</p>","css":"p{background:url(https://evil.example/x)}","frame":true}`, 422)
	if !strings.Contains(string(refused), "url()") {
		t.Fatal("the refusal must name its reason", string(refused))
	}
	big, _ := json.Marshal(map[string]any{"expected_revision": "0", "html": strings.Repeat("a", pagetemplate.MaxCustomHTML+1), "css": "", "frame": true})
	jsonCall("PUT", base+"/html", string(big), 422)

	var pngBytes bytes.Buffer
	png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	var form bytes.Buffer
	mp := multipart.NewWriter(&form)
	part, _ := mp.CreateFormFile("file", "custom.png")
	part.Write(pngBytes.Bytes())
	mp.Close()
	code, _, uploaded := call("POST", "/console/v1/admin/templates/images", admin, csrf, mp.FormDataContentType(), form.Bytes())
	var asset struct {
		Data pagetemplate.Asset `json:"data"`
	}
	if code != 201 || json.Unmarshal(uploaded, &asset) != nil || len(asset.Data.ID) != 64 {
		t.Fatal("upload", code, string(uploaded))
	}
	id := asset.Data.ID
	html := `<h1 onclick="steal()">自定义首页</h1><img src="/template-assets/` + id + `" alt="图"><script>steal()</script><a href="javascript:steal()">x</a><a href="/doc/">文档</a>`
	save, _ := json.Marshal(map[string]any{"expected_revision": "0", "html": html, "css": ".mine{color:#c00}", "frame": true})
	var saved struct {
		Data pagetemplate.CustomSaved `json:"data"`
	}
	if json.Unmarshal(jsonCall("PUT", base+"/html", string(save), 200), &saved) != nil || saved.Data.Version != 1 || saved.Data.Revision != 1 || saved.Data.Removed["<script>"] != 1 || saved.Data.Removed["onclick"] != 1 {
		t.Fatal("save", saved)
	}
	jsonCall("PUT", base+"/html", string(save), 409)
	code, header, preview := call("GET", base+"/versions/1/preview", admin, "", "", nil)
	if code != 200 || !strings.Contains(header.Get("Content-Security-Policy"), "script-src 'none'") || !bytes.Contains(preview, []byte(`<img src="/console/v1/admin/templates/images/`+id+`" alt="图">`)) || bytes.Contains(preview, []byte("<script")) {
		t.Fatal("preview", code, string(preview))
	}
	if code, _, _ = call("GET", "/template-assets/"+id, "", "", "", nil); code != 404 {
		t.Fatal("image public before the custom page is selected", code)
	}
	jsonCall("PUT", "/console/v1/admin/templates/home/selection", `{"expected_revision":"1","name":"custom"}`, 200)
	code, header, home := call("GET", "/", "", "", "", nil)
	if code != 200 || !strings.Contains(header.Get("Content-Security-Policy"), "default-src 'none'") || strings.Contains(header.Get("Content-Security-Policy"), "script-src") ||
		!bytes.Contains(home, []byte(`<h1>自定义首页</h1>`)) || !bytes.Contains(home, []byte(`<img src="/template-assets/`+id+`" alt="图">`)) || !bytes.Contains(home, []byte(`<style>.mine{color:#c00}</style>`)) ||
		bytes.Contains(home, []byte("<script")) || bytes.Contains(home, []byte("javascript:")) || bytes.Contains(home, []byte("steal()")) {
		t.Fatal("public custom page", code, header.Get("Content-Security-Policy"), string(home))
	}
	if code, _, _ = call("GET", "/template-assets/"+id, "", "", "", nil); code != 200 {
		t.Fatal("image of the selected custom page not public", code)
	}
	code, _, agreement := call("GET", "/agreement.html", "", "", "", nil)
	if code != 200 || !bytes.Contains(agreement, []byte(`class="home home-custom"`)) || bytes.Contains(agreement, []byte("自定义首页")) {
		t.Fatal("the agreement keeps the system page in the custom style", code)
	}
	second, _ := json.Marshal(map[string]any{"expected_revision": "2", "html": "<p>第二版</p>", "css": "", "frame": false})
	jsonCall("PUT", base+"/html", string(second), 200)
	if code, _, _ = call("GET", "/template-assets/"+id, "", "", "", nil); code != 404 {
		t.Fatal("image stays public after the page stopped showing it", code)
	}
	var restored struct {
		Data pagetemplate.CustomSaved `json:"data"`
	}
	jsonCall("POST", base+"/versions/1/restore", `{"expected_revision":"2"}`, 409)
	if json.Unmarshal(jsonCall("POST", base+"/versions/1/restore", `{"expected_revision":"3"}`, 200), &restored) != nil || restored.Data.Version != 3 {
		t.Fatal("restore", restored)
	}
	jsonCall("POST", base+"/versions/9/restore", `{"expected_revision":"4"}`, 404)
	var versions struct {
		Data struct {
			Items []pagetemplate.CustomVersionItem `json:"items"`
		} `json:"data"`
	}
	if json.Unmarshal(jsonCall("GET", base+"/versions", "", 200), &versions) != nil || len(versions.Data.Items) != 3 || versions.Data.Items[0].RestoredFrom != 1 || versions.Data.Items[0].Notice != "pending" {
		t.Fatal("versions", versions)
	}
	var v2 struct {
		Data pagetemplate.CustomView `json:"data"`
	}
	if json.Unmarshal(jsonCall("GET", base+"/versions/2", "", 200), &v2) != nil || v2.Data.HTML != "<p>第二版</p>" || v2.Data.Frame {
		t.Fatal("version 2", v2)
	}
	for _, path := range []string{base + "/versions/0", base + "/versions/01", base + "/versions/x/preview", base + "/nope"} {
		if code, _, _ = call("GET", path, admin, "", "", nil); code != 404 {
			t.Fatal("unknown custom route", path, code)
		}
	}
	// The pay pages never take the custom HTML.
	code, _, payPreview := call("GET", "/console/v1/admin/templates/pay/default/preview", admin, "", "", nil)
	if code != 200 || bytes.Contains(payPreview, []byte("自定义首页")) || bytes.Contains(payPreview, []byte("custom-main")) {
		t.Fatal("pay page took the custom HTML", code)
	}
	// Exactly one administrator notice job per version, nothing financial.
	var notices, others int
	if e = p.QueryRow(ctx, `SELECT count(*) FILTER (WHERE kind=$1), count(*) FILTER (WHERE kind<>$1) FROM jobs`, pagetemplate.CustomNoticeKind).Scan(&notices, &others); e != nil || notices != 3 || others != 0 {
		t.Fatal("notice jobs", notices, others, e)
	}
	for _, table := range []string{"orders", "ledger_entries"} {
		var n int
		if e = p.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); e != nil || n != 0 {
			t.Fatal("custom HTML changed financial table", table, n, e)
		}
	}
	var audit string
	if e = p.QueryRow(ctx, "SELECT coalesce(jsonb_agg(changes)::text,'') FROM backoffice_audit WHERE resource='page-templates' AND operation IN('save-html','restore-html')").Scan(&audit); e != nil || strings.Contains(audit, "steal()") || strings.Count(audit, `"digest"`) != 3 {
		t.Fatal("audit must record each version without its HTML", audit, e)
	}
}
