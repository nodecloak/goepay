package pagetemplate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

const testKey = "0123456789abcdef0123456789abcdef"

var (
	canonicalTag  = regexp.MustCompile(`^<(/?)([a-z0-9]+)((?: [a-z][a-z0-9-]*(?:="[^"<>]*")?)*)>`)
	canonicalAttr = regexp.MustCompile(` ([a-z][a-z0-9-]*)(?:="([^"<>]*)")?`)
	packageImage  = regexp.MustCompile(`^/template-files/` + testKey + `/images/[a-z]+\.png([?#][^"]*)?$`)
)

func payContextFor(files ...string) *payContext {
	paths := map[string]bool{"index.html": true, "template.json": true}
	for _, f := range files {
		paths[f] = true
	}
	return &payContext{kind: "pay", key: testKey, files: paths, warn: &warnList{}}
}

// assertCanonical checks the sanitizer's output grammar: every '<' starts an
// allowlisted, balanced tag whose attributes are allowlisted, quoted and
// escaped; nothing can run a script, load from outside the package or end
// the page's <main> element.
func assertCanonical(t *testing.T, in, out string) {
	t.Helper()
	var stack []string
	for i := 0; i < len(out); {
		j := strings.IndexByte(out[i:], '<')
		if j < 0 {
			break
		}
		m := canonicalTag.FindStringSubmatch(out[i+j:])
		if m == nil {
			t.Fatalf("non-canonical markup %q\ninput %q", out[i+j:min(len(out), i+j+40)], in)
		}
		closing, name, attrs := m[1] == "/", m[2], m[3]
		if !customKeep[name] || name == "main" {
			t.Fatalf("element %s kept from %q", name, in)
		}
		if closing {
			if attrs != "" || len(stack) == 0 || stack[len(stack)-1] != name {
				t.Fatalf("unbalanced </%s> in %q from %q", name, out, in)
			}
			stack = stack[:len(stack)-1]
		} else {
			for _, a := range canonicalAttr.FindAllStringSubmatch(attrs, -1) {
				attr, value := a[1], strings.ToLower(a[2])
				if strings.HasPrefix(attr, "on") || attr == "srcdoc" || attr == "formaction" || attr == "action" || attr == "xlink" {
					t.Fatalf("attribute %s kept from %q", attr, in)
				}
				if attr == "href" || attr == "cite" {
					plain := strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(strings.TrimSpace(value))
					for _, scheme := range []string{"javascript:", "data:", "vbscript:", "file:", "blob:"} {
						if strings.HasPrefix(plain, scheme) {
							t.Fatalf("unsafe %s %q kept from %q", attr, value, in)
						}
					}
				}
				if attr == "src" && !packageImage.MatchString(a[2]) {
					t.Fatalf("image source %q kept from %q", a[2], in)
				}
				if attr == "style" && (strings.Contains(strings.ReplaceAll(value, "url(/template-files/", ""), "url(") || strings.Contains(value, "expression")) {
					t.Fatalf("style %q kept from %q", value, in)
				}
			}
			if !customVoid[name] {
				stack = append(stack, name)
			}
		}
		i += j + len(m[0])
	}
	if len(stack) != 0 {
		t.Fatalf("unclosed %v in %q from %q", stack, out, in)
	}
	if strings.ContainsAny(out, "\x00\x01\x02") {
		t.Fatalf("control bytes in %q from %q", out, in)
	}
	if !strings.Contains(out, "<img") {
		again, _ := sanitizeMarkup(out, payContextFor("images/a.png"))
		if again != out {
			t.Fatalf("sanitizing is not stable:\n%q\n%q", out, again)
		}
	}
}

func TestPaySanitizerRemovesEverythingThatCouldRunOrLoad(t *testing.T) {
	vectors := []string{
		`<script>alert(1)</script>`,
		`<SCRIPT SRC=//x.example/x.js></SCRIPT>after`,
		`<script>document.write("</scr"+"ipt>")</script>`,
		`<img src=x onerror=alert(1)>`,
		`<img src="images/a.png" onerror="alert(1)" onload=alert(2)>`,
		`<a href="javascript:alert(1)">x</a>`,
		`<a href="JaVaScRiPt:alert(1)">x</a>`,
		`<a href=" javascript:alert(1)">x</a>`,
		`<a href="&#106;avascript:alert(1)">x</a>`,
		`<a href="&#x6A;avascript:alert(1)">x</a>`,
		`<a href="&#0000106&#0000097&#0000118&#0000097&#0000115&#0000099&#0000114&#0000105&#0000112&#0000116&#0000058alert(1)">x</a>`,
		`<a href="java&#x09;script:alert(1)">x</a>`,
		"<a href=\"java\nscript:alert(1)\">x</a>",
		"<a href=\"\x01javascript:alert(1)\">x</a>",
		`<a href="javascript&colon;alert(1)">x</a>`,
		`<a href="javascript&#58alert(1)">x</a>`,
		`<a href="data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==">x</a>`,
		`<a href="vbscript:msgbox(1)">x</a>`,
		`<a href="https://ok.example/" onmouseover="alert(1)">ok</a>`,
		`<iframe src="https://x.example" srcdoc="<script>alert(1)</script>"></iframe>`,
		`<svg onload=alert(1)><script>alert(1)</script><a xlink:href="javascript:alert(1)">x</a></svg>`,
		`<math><mi xlink:href="javascript:alert(1)">x</mi></math>`,
		`<object data="https://x.example/x.swf"></object><embed src="https://x.example/x.swf">`,
		`<form action="https://evil.example"><input name=a><button formaction="javascript:alert(1)">go</button></form>`,
		`<base href="https://evil.example/">`,
		`<meta http-equiv="refresh" content="0;url=https://evil.example">`,
		`<div style="width:expression(alert(1))">x</div>`,
		`<p style="color:red;}</style><script>alert(1)</script>">x</p>`,
		`<!--><script>alert(1)</script>-->`,
		`<!-- --!><script>alert(1)</script>`,
		`<!--<script>alert(1)</script>`,
		`<scr<script>ipt>alert(1)</scr</script>ipt>`,
		`</main><script>alert(1)</script>`,
		`</div></div></main></body><img src=x onerror=alert(1)>`,
		`<textarea><img src=x onerror=alert(1)></textarea>`,
		`<noscript><p title="</noscript><img src=x onerror=alert(1)>"></noscript>`,
		`<title><img src=x onerror=alert(1)></title>`,
		`<template><img src=x onerror=alert(1)></template>`,
		`<x onclick=alert(1)>text</x>`,
		`<a href="x" """><b>`,
		`<img src="images/../../console/v1/admin/x">`,
		`<img src="/template-files/` + testKey + `/images/a.png">`,
		`<img src="https://evil.example/x.png">`,
		`<img src="data:image/png;base64,AAAA">`,
		`<img src="images/a.png#x" alt="ok">`,
		`<a href=javascript:alert(1)>x</a>`,
		`<a/href="javascript:alert(1)">x</a>`,
		`<a href="https://ok.example/"`,
		`<img src="images/a.png"`,
		`<details open ontoggle=alert(1)><summary>Q</summary>A</details>`,
		`<p><plaintext><img src=x onerror=alert(1)>`,
		`<div id="x" class="a b" style="color:red" data-x="1" ping="https://x.example" draggable=true>x</div>`,
		`<a href="https://ok.example/" target="_top">x</a>`,
		`<table><tr><td colspan="999999">x</td></tr></table>`,
		`<div>` + strings.Repeat(`<div>`, 300) + `deep`,
		`<p>unclosed <b>bold <i>italic`,
		"<p>\x00null</p>",
		"<p>&#1;@login_url&#2;</p>",
		`<main><div>x</div></main>`,
	}
	for _, in := range vectors {
		c := payContextFor("images/a.png")
		out, _ := sanitizeMarkup(in, c)
		low := strings.ToLower(out)
		for _, never := range []string{"<script", "<iframe", "<svg", "<math", "<object", "<embed", "<form", "<input", "<button", "<base", "<meta", "<link", "<style", "<template", "<noscript", "<textarea", "<title", "<plaintext", "<main", "\x00"} {
			if strings.Contains(low, never) {
				t.Fatalf("%q kept %q: %q", in, never, out)
			}
		}
		assertCanonical(t, in, out)
		if c.css.Len() != 0 || len(c.links) != 0 {
			t.Fatalf("%q collected style %q %v", in, c.css.String(), c.links)
		}
	}
}

// TestWriteSanitizerCorpus runs attack corpora through the pay sanitizer for
// the browser oracle: GOEPAY_SANITIZER_CORPUS lists JSON files of
// {name,in} cases, GOEPAY_SANITIZER_OUT receives {name,in,out}, where out is
// the template CSS in a <style> plus the body with every placeholder filled
// with a hostile value, as a page would show it. Skipped without them.
func TestWriteSanitizerCorpus(t *testing.T) {
	inputs, output := os.Getenv("GOEPAY_SANITIZER_CORPUS"), os.Getenv("GOEPAY_SANITIZER_OUT")
	if inputs == "" || output == "" {
		t.Skip("GOEPAY_SANITIZER_CORPUS and GOEPAY_SANITIZER_OUT not set")
	}
	type item struct {
		Name string `json:"name"`
		In   string `json:"in"`
		Out  string `json:"out"`
	}
	var cases []item
	for _, f := range strings.Split(inputs, ",") {
		raw, e := os.ReadFile(f)
		if e != nil {
			t.Fatal(e)
		}
		var list []struct{ Name, In string }
		if e = json.Unmarshal(raw, &list); e != nil {
			t.Fatal(f, e)
		}
		for _, c := range list {
			cases = append(cases, item{Name: c.Name, In: c.In})
		}
	}
	hostile := `"><img src=x onerror=alert(1)>'` + "`" + `<script>alert(2)</script>`
	values := map[string]string{"logo_url": "javascript:alert(3)"}
	for name := range payTokens {
		values[name] = hostile
	}
	extra := []string{
		`<img src="images/a.png" alt="{{brand_name}}" title="{{title}}"><p>{{amount}} {{trade_no}} {{order_name}}</p><a href="{{logo_url}}">x</a><img src="{{logo_url}}">`,
		`<div style="background:url(images/a.png)" aria-label="{{merchant_name}}">{{content}}</div>`,
		`<style>.a{background:url("images/a.png")}.b{color:red}</style><link rel=stylesheet href=css/a.css><div>{{content}}</div>`,
		`<style>.a{background:url(images/a.png?x=</style><img src=x onerror=alert(1)>)}</style>`,
		`<img src="images/a.png?&quot;onerror=&quot;alert(1)">`, `<img src='images/a.png?" onerror="alert(1)'>`,
		`<img src="images/a.png#\"><script>alert(1)</script>">`, `<div style="background:url(images/a.png?\");x:expression(alert(1))">x</div>`,
		"<p>&#1;!&#2;&#1;$&#2;&#1;@logo_url&#2;</p>", "<p title=\"&#1;!&#2;\">x</p>",
	}
	for i, in := range extra {
		cases = append(cases, item{Name: fmt.Sprintf("package-%d", i), In: in})
	}
	for i := range cases {
		c := payContextFor("images/a.png", "css/a.css")
		out, _ := sanitizeMarkup(cases[i].In, c)
		if c.err != nil {
			continue // refused uploads never render
		}
		before, after, _ := strings.Cut(out, "\x01#\x02")
		cases[i].Out = "<style>" + c.css.String() + "</style>" + expand(before, values, "", "", "") + "<main>CHECKOUT</main>" + expand(after, values, "", "", "")
	}
	raw, _ := json.Marshal(cases)
	if e := os.WriteFile(output, raw, 0o644); e != nil {
		t.Fatal(e)
	}
	t.Logf("wrote %d cases", len(cases))
}

func TestPaySanitizerUsesPackageFilesOnly(t *testing.T) {
	in := `<!doctype html><html><head><title>t</title><meta charset="utf-8"><link rel="icon" href="images/a.png"><link rel="stylesheet" href="css/site.css"><link rel="stylesheet" href="./css/site.css?v=2">
<style>.hero{background:url("images/a.png") no-repeat, url(data:image/png;base64,AA) ;}.ok{color:red}</style></head>
<body class="tpl bad|class"><header class="top"><img src="images/a.png?v=1" alt="{{brand_name}}"><img src="images/gone.png"><img src="https://cdn.example/x.png"></header>
<div class="box" style="background-image:url(images/a.png)">{{content}}</div><p>{{year}} {{brand_name}} {{amount}} {{nope}}</p><footer><a href="{{logo_url}}">x</a></footer>
<script>alert(1)</script><form><button>go</button></form></body></html>`
	c := payContextFor("css/site.css", "images/a.png")
	out, removed := sanitizeMarkup(in, c)
	for _, want := range []string{
		`<header class="top"><img src="/template-files/` + testKey + `/images/a.png?v=1" alt="` + "\x01brand_name\x02" + `"></header>`,
		`<div class="box" style="background-image:url(/template-files/` + testKey + `/images/a.png)">` + "\x01#\x02</div>",
		"<p>\x01year\x02 \x01brand_name\x02 \x01amount\x02 {{nope}}</p>",
		`<a href="` + "\x01@logo_url\x02" + `">x</a>`,
		"go",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%q", want, out)
		}
	}
	if len(c.links) != 1 || c.links[0] != "/template-files/"+testKey+"/css/site.css" {
		t.Fatal("links", c.links)
	}
	if css := c.css.String(); !strings.Contains(css, `url("/template-files/`+testKey+`/images/a.png") no-repeat, url(data:image/png;base64,AA)`) || !strings.Contains(css, ".ok{color:red}") {
		t.Fatal("inline css", css)
	}
	if c.bodyClass != "tpl" || c.contents != 1 || c.misplaced != "" || c.err != nil {
		t.Fatal("context", c.bodyClass, c.contents, c.misplaced, c.err)
	}
	if removed["<script>"] != 1 || removed["图片（只能用模板包里的图片）"] != 2 || removed["<title>"] != 0 || removed["<meta>"] != 0 || removed["<link>"] != 0 {
		t.Fatal("removed", removed)
	}
	warnings := strings.Join(c.warn.list(), "\n")
	if !strings.Contains(warnings, "images/gone.png") || !strings.Contains(warnings, "{{nope}}") {
		t.Fatal("warnings", warnings)
	}
	for _, bad := range []struct{ in, want string }{
		{`<link rel="stylesheet" href="https://cdn.example/a.css"><div>{{content}}</div>`, "外部样式"},
		{`<style>@import "css/site.css";</style><div>{{content}}</div>`, "@import"},
		{`<style>.a{background:url(https://evil.example/x.png)}</style><div>{{content}}</div>`, "外部地址"},
		{`<style>.a{background:url(//evil.example/x.png)}</style><div>{{content}}</div>`, "外部地址"},
		{`<style>.a{background:url(/static/x.png)}</style><div>{{content}}</div>`, "外部地址"},
		{`<style>.a{background:url(images\/a.png)}</style><div>{{content}}</div>`, "反斜杠"},
		{`<style>.a{width:expression(alert(1))}</style><div>{{content}}</div>`, "expression"},
		{`<div style="background:url(https://evil.example/x)">{{content}}</div>`, "外部地址"},
	} {
		c := payContextFor("css/site.css", "images/a.png")
		sanitizeMarkup(bad.in, c)
		refusedWith(t, c.err, bad.want)
	}
}

func TestPayTemplatesNeedOneWellPlacedContent(t *testing.T) {
	paths := map[string]bool{"index.html": true, "template.json": true}
	for _, c := range []struct{ index, want string }{
		{`<div>no content</div>`, "必须包含一次"},
		{`<div>{{content}}</div><div>{{ content }}</div>`, "出现了 2 次"},
		{`<p>{{content}}</p>`, "<p>"},
		{`<div><a href="/">{{content}}</a></div>`, "<a>"},
		{`<div title="{{content}}">x</div>`, "属性"},
		{`<template>{{content}}</template>`, "必须包含一次"},
	} {
		_, e := compilePackage("pay", testKey, map[string][]byte{"index.html": []byte(c.index)}, paths, false, &warnList{})
		refusedWith(t, e, c.want)
	}
	for _, index := range []string{`{{content}}`, `<section><article><div>{{content}}</div></article></section>`, `<main>{{content}}</main>`, `<header>a</header><aside>{{content}}</aside><footer>b</footer>`} {
		if _, e := compilePackage("qrpay", testKey, map[string][]byte{"index.html": []byte(index)}, paths, false, &warnList{}); e != nil {
			t.Fatal(index, e)
		}
	}
	// Style sheets of the package follow the same rules as inline styles.
	files := map[string][]byte{"index.html": []byte(`<link rel="stylesheet" href="css/a.css"><div>{{content}}</div>`), "css/a.css": []byte(`@import url(b.css);`)}
	_, e := compilePackage("pay", testKey, files, pathSet(files), true, &warnList{})
	refusedWith(t, e, "@import")
	files["css/a.css"] = []byte(`.a{background:url(../images/a.png)}.b{background:url(../images/gone.png)}`)
	files["images/a.png"] = []byte("png")
	warn := &warnList{}
	if _, e = compilePackage("pay", testKey, files, pathSet(files), true, warn); e != nil || len(warn.list()) != 1 || !strings.Contains(warn.list()[0], "gone.png") {
		t.Fatal(e, warn.list())
	}
}

func TestOpenUploadedPayTemplateFramesTheCheckoutBlock(t *testing.T) {
	files := map[string][]byte{
		"index.html": []byte(`<html><head><link rel="stylesheet" href="css/a.css"><style>.shell{padding:8px}</style></head><body class="shop"><div class="shell"><header>{{brand_name}} · {{trade_no}}</header><div class="body">{{content}}</div><footer>© {{year}} {{order_name}}</footer></div></body></html>`),
		"css/a.css":  []byte(`.shell{color:#333}`),
	}
	c, e := compilePackage("pay", testKey, files, pathSet(files), true, &warnList{})
	if e != nil {
		t.Fatal(e)
	}
	cfg := DefaultConfig("pay", "shop-pay")
	cfg.BrandName = `<b>品牌</b>`
	r := Resolved{Type: "pay", Name: "shop-pay", Config: cfg, Upload: &UploadedTemplate{Name: "shop-pay", Version: 1, Key: testKey, compiled: c}}
	var b bytes.Buffer
	tail, e := Open(&b, r, Page{Title: "支付收银台", TradeNo: "T<1>", Amount: "1.00", OrderName: `<img src=x>`, Sandbox: true})
	if e != nil {
		t.Fatal(e)
	}
	b.WriteString(`<p>请扫码</p>` + tail)
	out := b.String()
	for _, want := range []string{
		`<title>支付收银台</title><style>:root{--goepay-accent:#007c91;`, `.goepay-checkout .template-head{`,
		`<link rel="stylesheet" href="/template-files/` + testKey + `/css/a.css"><style>.shell{padding:8px}`,
		`<body class="shop" data-template="shop-pay" data-template-type="pay" data-template-source="upload"><div class="shell"><header>&lt;b&gt;品牌&lt;/b&gt; · T&lt;1&gt;</header><div class="body"><main class="goepay-checkout goepay-checkout-pay"><header class="template-head">`,
		`<strong><small>¥</small>1.00</strong>`, `本地模拟环境，不涉及真实资金。`,
		`<p>请扫码</p></main></div><footer>© `, ` &lt;img src=x&gt;</footer></div></body></html>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Count(out, "<body") != 1 || strings.Count(out, "</html>") != 1 || strings.Contains(out, "<img src=x>") {
		t.Fatal("document structure", out)
	}
	// A home upload never opens a pay page, and a kind mismatch is refused.
	if _, e = Open(&b, Resolved{Type: "qrpay", Name: "shop-pay", Config: DefaultConfig("qrpay", "shop-pay"), Upload: r.Upload}, Page{Title: "x"}); e == nil {
		t.Fatal("pay template opened a qrpay page")
	}
}

func TestRenderUploadedHomeFillsPlaceholders(t *testing.T) {
	paths := map[string]bool{"index.html": true, "template.json": true}
	index := `<html><head><title>{{site_name}}</title></head><body><a href="{{register_url}}">注册</a><a href="{{qq_url}}">QQ</a><a href="#faq">问答</a><img src="{{logo_url}}" alt="{{brand_name}}"><p>{{notice}}|{{registration_open}}|{{email}}</p></body></html>`
	c, _ := compilePackage("home", testKey, map[string][]byte{"index.html": []byte(index)}, paths, false, &warnList{})
	cfg := DefaultConfig("home", "mine")
	cfg.Notice = "配置说明"
	r := Resolved{Type: "home", Name: "mine", Config: cfg, Upload: &UploadedTemplate{Name: "mine", Key: testKey, compiled: c}}
	var b bytes.Buffer
	if e := RenderHome(&b, r, HomeData{SiteName: `<站点>`, QQURL: "tencent://message/?uin=1", Email: "a@b.example", RegistrationOpen: true, Sandbox: true}); e != nil {
		t.Fatal(e)
	}
	out := b.String()
	for _, want := range []string{
		`<html><head><base href="/template-files/` + testKey + `/"><style>:root{--goepay-accent:#007c91;`, `<title>&lt;站点&gt;</title>`,
		`本地模拟环境，不涉及真实资金。</p><a href="/merchant/login?enroll=register">`, `<a href="/#">QQ</a>`, `<a href="/#faq">`,
		`<img src="/static/goepay-wordmark-onlight.svg?v=90cafbd35591" alt="GOEPAY">`, `<p>配置说明|true|a@b.example</p>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	b.Reset()
	if e := RenderHome(&b, r, HomeData{Preview: true, PageURL: "/console/v1/admin/templates/home/mine/preview"}); e != nil || !strings.Contains(b.String(), `<a href="/console/v1/admin/templates/home/mine/preview#faq">`) || !strings.Contains(b.String(), "仅为模板预览") {
		t.Fatal("preview", e, b.String())
	}
	// The agreement page keeps the built-in default style.
	b.Reset()
	if e := RenderAgreement(&b, r, HomeData{SiteName: "S"}); e != nil || !strings.Contains(b.String(), `class="home home-default"`) || strings.Contains(b.String(), "/template-files/") {
		t.Fatal("agreement", e)
	}
}
