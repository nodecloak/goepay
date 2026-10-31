package pagetemplate

import (
	"archive/zip"
	"bytes"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipOf builds a package zip from path -> content.
func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, p := range sortedPaths(stringFiles(files)) {
		w, e := zw.Create(p)
		if e != nil {
			t.Fatal(e)
		}
		w.Write([]byte(files[p]))
	}
	if e := zw.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}

func stringFiles(files map[string]string) map[string][]byte {
	out := map[string][]byte{}
	for p, s := range files {
		out[p] = []byte(s)
	}
	return out
}

func pngFile(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	if e := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); e != nil {
		t.Fatal(e)
	}
	return b.String()
}

func manifest(kind, name string) string {
	return `{"type":"` + kind + `","name":"` + name + `","title":"测试模板","version":"1.0.0","author":"tester","description":"for tests"}`
}

func refusedWith(t *testing.T, e error, want string) {
	t.Helper()
	var u *UploadError
	if !errors.As(e, &u) || !errors.Is(e, ErrInvalid) || !strings.Contains(u.Reason, want) {
		t.Fatalf("want refusal containing %q, got %v", want, e)
	}
}

func TestSamplePackagesAreAcceptedAsIs(t *testing.T) {
	for _, kind := range []string{"home", "pay", "qrpay"} {
		raw, e := SamplePackage(kind)
		if e != nil {
			t.Fatal(kind, e)
		}
		p, e := preparePackage(kind, raw)
		if e != nil {
			t.Fatal(kind, e)
		}
		if p.pkg.Manifest.Name != "sample-"+kind || len(p.report.Warnings) != 0 || len(p.report.Removed) != 0 {
			t.Fatalf("%s sample: name %q warnings %v removed %v", kind, p.pkg.Manifest.Name, p.report.Warnings, p.report.Removed)
		}
		if _, ok := p.pkg.Files["README.md"]; ok || len(p.report.Ignored) != 1 {
			t.Fatalf("%s sample: README must be ignored on upload, ignored %v", kind, p.report.Ignored)
		}
		if _, ok := p.pkg.Files["preview.png"]; !ok {
			t.Fatalf("%s sample has no preview picture", kind)
		}
		if kind != "home" && (strings.Contains(p.compiled.before+p.compiled.after, "<script") || len(p.compiled.links) != 1) {
			t.Fatalf("%s sample compiled to %q with links %v", kind, p.compiled.before, p.compiled.links)
		}
	}
	if _, e := SamplePackage("other"); e == nil {
		t.Fatal("unknown kind")
	}
}

func TestParsePackageRefusesWhatItDoesNotUnderstand(t *testing.T) {
	index := `<!doctype html><html><head><link rel="stylesheet" href="css/a.css"></head><body><div>{{content}}</div></body></html>`
	good := map[string]string{"template.json": manifest("pay", "shop-pay"), "index.html": index, "css/a.css": "body{color:red}"}
	with := func(change func(m map[string]string)) []byte {
		m := map[string]string{}
		for k, v := range good {
			m[k] = v
		}
		change(m)
		return zipOf(t, m)
	}
	if _, e := ParsePackage("pay", zipOf(t, good)); e != nil {
		t.Fatal("good package", e)
	}
	cases := []struct {
		raw  []byte
		kind string
		want string
	}{
		{[]byte("not a zip"), "pay", "zip"},
		{bytes.Repeat([]byte("a"), MaxPackageZip+1), "pay", "10 MiB"},
		{with(func(m map[string]string) { delete(m, "template.json") }), "pay", "template.json"},
		{with(func(m map[string]string) { delete(m, "index.html") }), "pay", "index.html"},
		{zipOf(t, good), "home", "支付页模板"},
		{with(func(m map[string]string) { m["template.json"] = manifest("pay", "default") }), "pay", "保留"},
		{with(func(m map[string]string) { m["template.json"] = manifest("pay", "index3") }), "pay", "保留"},
		{with(func(m map[string]string) { m["template.json"] = manifest("pay", "Bad_Name") }), "pay", "小写"},
		{with(func(m map[string]string) {
			m["template.json"] = `{"type":"pay","name":"shop-pay","title":"t","extra":1}`
		}), "pay", "不认识的字段"},
		{with(func(m map[string]string) {
			m["template.json"] = `{"type":"pay","name":"shop-pay","title":"t","title":"u"}`
		}), "pay", "重复"},
		{with(func(m map[string]string) { m["template.json"] = `{"type":"pay","name":"shop-pay","title":"a\nb"}` }), "pay", "title"},
		{with(func(m map[string]string) { m["template.json"] = `{"type":"pay","name":"shop-pay","title":1}` }), "pay", "文字"},
		{with(func(m map[string]string) { m["../evil.css"] = "x" }), "pay", "文件名"},
		{with(func(m map[string]string) { m["css/空格 a.css"] = "x" }), "pay", "文件名"},
		{with(func(m map[string]string) { m["CSS/A.css"] = "x" }), "pay", "大小写"},
		{with(func(m map[string]string) { m["a.php"] = "<?php" }), "pay", "不支持"},
		{with(func(m map[string]string) { m["js/app.js"] = "alert(1)" }), "pay", "不运行脚本"},
		{with(func(m map[string]string) { m["about.html"] = "<p>x</p>" }), "pay", "index.html"},
		{with(func(m map[string]string) { m["images/a.png"] = "<svg>not png</svg>" }), "pay", "内容和类型不符"},
		{with(func(m map[string]string) { m["index.html"] = "\xff\xfe" }), "pay", "UTF-8"},
		{with(func(m map[string]string) { m["big.css"] = strings.Repeat("a", maxPackageFile+1) }), "pay", "3 MiB"},
	}
	for i, c := range cases {
		_, e := ParsePackage(c.kind, c.raw)
		refusedWith(t, e, c.want)
		_ = i
	}
	// Encrypted entries and links are refused; the zip header says so.
	for _, mutate := range []func(h *zip.FileHeader){
		func(h *zip.FileHeader) { h.Flags |= 0x1 },
		func(h *zip.FileHeader) { h.SetMode(os.ModeSymlink | 0o777) },
	} {
		var b bytes.Buffer
		zw := zip.NewWriter(&b)
		for _, p := range sortedPaths(stringFiles(good)) {
			h := &zip.FileHeader{Name: p, Method: zip.Deflate}
			if p == "css/a.css" {
				mutate(h)
			}
			w, _ := zw.CreateHeader(h)
			w.Write([]byte(good[p]))
		}
		zw.Close()
		if _, e := ParsePackage("pay", b.Bytes()); e == nil {
			t.Fatal("special entry accepted")
		}
	}
}

func TestParsePackageStripsOneFolderAndIgnoresZipLeftovers(t *testing.T) {
	raw := zipOf(t, map[string]string{
		"mine/template.json": manifest("home", "mine"), "mine/index.html": "<p>hi</p>", "mine/js/app.js": "1",
		"mine/README.md": "x", "mine/.DS_Store": "x", "__MACOSX/mine/._index.html": "x", "mine/js/app.js.map": "{}",
	})
	p, e := ParsePackage("home", raw)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Files) != 3 || p.Files["js/app.js"] == nil || len(p.Ignored) != 4 {
		t.Fatalf("files %v ignored %v", sortedPaths(p.Files), p.Ignored)
	}
	// Windows separators are read as folders.
	raw = zipOf(t, map[string]string{`template.json`: manifest("home", "mine"), `index.html`: "<p>hi</p>", `js\app.js`: "1"})
	if p, e = ParsePackage("home", raw); e != nil || p.Files["js/app.js"] == nil {
		t.Fatal("backslash paths", e)
	}
}

// TestWriteUploadSamplePages renders the sample packages with the real
// renderer into GOEPAY_TEMPLATE_PREVIEW_DIR (sample-<kind>.html, and the
// package files under template-files/<key>/), for screenshots. Skipped
// without the variable.
func TestWriteUploadSamplePages(t *testing.T) {
	dir := os.Getenv("GOEPAY_TEMPLATE_PREVIEW_DIR")
	if dir == "" {
		t.Skip("GOEPAY_TEMPLATE_PREVIEW_DIR not set")
	}
	for _, kind := range []string{"home", "pay", "qrpay"} {
		raw, e := SamplePackage(kind)
		if e != nil {
			t.Fatal(e)
		}
		pkg, e := ParsePackage(kind, raw)
		if e != nil {
			t.Fatal(e)
		}
		key := strings.Repeat(map[string]string{"home": "a", "pay": "b", "qrpay": "c"}[kind], 32)
		c, e := compilePackage(kind, key, pkg.Files, pathSet(pkg.Files), true, &warnList{})
		if e != nil {
			t.Fatal(e)
		}
		for p, b := range pkg.Files {
			target := filepath.Join(dir, "template-files", key, filepath.FromSlash(p))
			if e = os.MkdirAll(filepath.Dir(target), 0o755); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(target, b, 0o644); e != nil {
				t.Fatal(e)
			}
		}
		r := Resolved{Type: kind, Name: pkg.Manifest.Name, Config: DefaultConfig(kind, pkg.Manifest.Name), Upload: &UploadedTemplate{Name: pkg.Manifest.Name, Version: 1, Key: key, compiled: c}}
		var b bytes.Buffer
		switch kind {
		case "home":
			e = RenderHome(&b, r, HomeData{SiteName: "GOEPAY", Title: "让每一笔收款都有清晰的来路与去向", Description: "聚合支付、订单通知与账务核对", Org: "示例科技有限公司", Email: "support@example.com", QQ: "10001", TestOpen: true, RegistrationOpen: true, PageURL: "/sample-home.html"})
		case "pay":
			var tail string
			if tail, e = Open(&b, r, Page{Title: "支付收银台", TradeNo: "2026103112345678", Amount: "100.00", OrderName: "示例商品"}); e == nil {
				b.WriteString(`<p>请使用支付宝扫描支付二维码。</p><img width="320" height="320" alt="支付二维码" src="/demo-qr.png"><p>请在有效期内完成付款，付款结果以平台确认为准。</p>` + tail)
			}
		default:
			var tail string
			if tail, e = Open(&b, r, Page{Title: "向示例商户付款", MerchantName: "示例商户"}); e == nil {
				b.WriteString(`<p>收款说明：向示例商户付款</p><form method="post" action="#"><label>金额（元）<input name="amount" type="number" value="100.00"></label><label>支付方式<select name="type"><option>微信支付</option><option>支付宝</option></select></label><button type="submit">前往支付</button></form>` + tail)
			}
		}
		if e != nil {
			t.Fatal(kind, e)
		}
		if e = os.WriteFile(filepath.Join(dir, "sample-"+kind+".html"), b.Bytes(), 0o644); e != nil {
			t.Fatal(e)
		}
	}
}

func TestValidUploadName(t *testing.T) {
	for _, name := range []string{"shop", "my-pay-2", "a1"} {
		if !ValidUploadName(name) {
			t.Fatal(name)
		}
	}
	for _, name := range []string{"default", "compact", "anime", "sakura", "minimal", "index", "index7", "index12", "custom", "uploads", "sample", "a", "-a", "a-", "A", "a_b", strings.Repeat("a", 33)} {
		if ValidUploadName(name) {
			t.Fatal(name)
		}
	}
}

func TestCompileHomeKeepsThePageAndEditsOnlyWhatItMust(t *testing.T) {
	files := map[string]bool{"index.html": true, "template.json": true, "css/site.css": true, "images/logo.png": true, "js/app.js": true}
	src := "\ufeff<!DOCTYPE html>\n<html lang=\"zh\"><head><meta charset=utf-8><base href=\"https://evil.example/\"><title>{{site_name}} - {{nope}}</title><link rel=stylesheet href=css/site.css></head>\n" +
		`<body class="x"><a href="#top">顶部</a> <a href="index.html#faq">问答</a> <a href="">首页</a> <a href=./>首页</a> <a href="{{login_url}}">登录</a> <a href="https://x.example/">外站</a>` +
		`<img src="images/logo.png" alt={{brand_name}}><img src="images/missing.png"><p title='{{site_name}} "x"'>{{notice}}</p>` +
		`<button onclick="go('{{site_name}}')">b</button><script>var a = "{{site_name}}"; if (a < b) {}</script><style>.x{background:url(images/gone.png)}</style>` +
		`<a href="mailto:{{email}}">mail</a><!-- {{site_name}} --></body></html>`
	warn := &warnList{}
	out := compileHome("0123456789abcdef0123456789abcdef", src, files, warn)
	for _, want := range []string{
		"<!DOCTYPE html>\n<html lang=\"zh\"><head>" + `<base href="/template-files/0123456789abcdef0123456789abcdef/">` + "\x01$\x02<meta charset=utf-8><title>\x01site_name\x02 - {{nope}}</title><link rel=stylesheet href=css/site.css>",
		`<body class="x">` + "\x01!\x02",
		`<a href="` + "\x01^\x02" + `#top">`, `<a href="` + "\x01^\x02" + `#faq">`, `<a href="` + "\x01^\x02" + `">首页</a> <a href="` + "\x01^\x02" + `">首页</a>`,
		`<a href="` + "\x01@login_url\x02" + `">`, `<a href="https://x.example/">`,
		`<img src="images/logo.png" alt="` + "\x01brand_name\x02" + `">`, `<p title="` + "\x01site_name\x02" + ` &quot;x&quot;">` + "\x01notice\x02</p>",
		`<button onclick="go('{{site_name}}')">`, `<script>var a = "{{site_name}}"; if (a < b) {}</script>`, `<!-- {{site_name}} -->`, `<a href="mailto:{{email}}">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%q", want, out)
		}
	}
	if strings.Contains(out, "evil.example") || strings.HasPrefix(out, "\ufeff") {
		t.Fatal("the template's <base> or the BOM survived", out)
	}
	got := strings.Join(warn.list(), "\n")
	for _, want := range []string{"<base>", "{{nope}}", "images/missing.png", "onclick", "href 属性里只能单独写一个"} {
		if !strings.Contains(got, want) {
			t.Errorf("warning %q missing in\n%s", want, got)
		}
	}
	// Without <head>, the <base> goes after <html>, or after the doctype.
	if out = compileHome("k", "<!doctype html><p>x</p>", files, &warnList{}); !strings.HasPrefix(out, `<!doctype html><base href="/template-files/k/">`) {
		t.Fatal(out)
	}
	if out = compileHome("k", "<p>x</p>", files, &warnList{}); !strings.HasPrefix(out, `<base href="/template-files/k/">`) {
		t.Fatal(out)
	}
	if out = compileHome("k", "<html><body>x</body></html>", files, &warnList{}); !strings.HasPrefix(out, `<html><base href="/template-files/k/">`+"\x01$\x02<body>\x01!\x02x") {
		t.Fatal(out)
	}
	// Source sentinels are removed.
	if out = compileHome("k", "<p>\x01@login_url\x02\x00</p>", files, &warnList{}); strings.Contains(out, "\x01@") || strings.Contains(out, "\x00") {
		t.Fatal("sentinel injected", out)
	}
}

func TestExpandEscapesEveryValue(t *testing.T) {
	values := map[string]string{"site_name": `<b>"&'`, "login_url": "javascript:alert(1)", "qq_url": "https://q.example/?a=1&b=2", "logo_url": "data:image/png;base64,AAAA", "home_url": "/"}
	got := expand("<p>\x01site_name\x02</p><a href=\"\x01@login_url\x02\"><a href=\"\x01@qq_url\x02\"><img src=\"\x01@logo_url\x02\"><a href=\"\x01^\x02#x\">\x01!\x02\x01$\x02\x01#\x02", values, "/p?a=<1>", "<p>bar</p>", "<style>v</style>")
	want := `<p>&lt;b&gt;&#34;&amp;&#39;</p><a href="/p?a=&lt;1&gt;#"><a href="https://q.example/?a=1&amp;b=2"><img src="data:image/png;base64,AAAA"><a href="/p?a=&lt;1&gt;#x"><p>bar</p><style>v</style>`
	if got != want {
		t.Fatalf("\n%q\n%q", got, want)
	}
	for _, bad := range []string{"//evil.example/", "vbscript:x", "data:text/html,x", "/a b", "\x01x"} {
		if safeURL(bad, "/") != "/#" {
			t.Fatal(bad)
		}
	}
}
