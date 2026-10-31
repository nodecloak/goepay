package pagetemplate

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

var testAsset = strings.Repeat("ab", 32)

var canonicalTag = regexp.MustCompile(`^<(/?)([a-z0-9]+)((?: [a-z][a-z0-9-]*(?:="[^"<>]*")?)*)>`)
var canonicalAttr = regexp.MustCompile(` ([a-z][a-z0-9-]*)(?:="([^"<>]*)")?`)

// assertCanonical checks the sanitizer's output grammar: every '<' starts an
// allowlisted, balanced tag whose attributes are allowlisted, quoted and
// escaped; nothing can run a script, load from elsewhere or end the page's
// <main> element.
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
				if attr == "src" && !customAssetSrc.MatchString(a[2]) {
					t.Fatalf("image source %q kept from %q", a[2], in)
				}
				if attr == "style" && (strings.Contains(value, "url(") || strings.Contains(value, "expression")) {
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
	again, e := SanitizeCustomHTML(out)
	if e != nil || again.HTML != out {
		t.Fatalf("sanitizing is not stable:\n%q\n%q", out, again.HTML)
	}
}

func TestSanitizeCustomHTMLRemovesEverythingThatCouldRunOrLoad(t *testing.T) {
	vectors := []string{
		`<script>alert(1)</script>`,
		`<SCRIPT SRC=//x.example/x.js></SCRIPT>after`,
		`<script>document.write("</scr"+"ipt>")</script>`,
		`<img src=x onerror=alert(1)>`,
		`<img src="/template-assets/` + testAsset + `" onerror="alert(1)" onload=alert(2)>`,
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
		`<link rel=stylesheet href=https://evil.example/x.css>`,
		`<style>body{background:url(https://evil.example)}</style>`,
		`<div style="background:url(https://evil.example)">x</div>`,
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
		`<img src="/template-assets/../../console/v1/admin/x">`,
		`<img src="https://evil.example/x.png">`,
		`<img src="data:image/png;base64,AAAA">`,
		`<a href=javascript:alert(1)>x</a>`,
		`<a/href="javascript:alert(1)">x</a>`,
		`<a href="https://ok.example/"`,
		`<img src="/template-assets/` + testAsset + `"`,
		`<details open ontoggle=alert(1)><summary>Q</summary>A</details>`,
		`<p><plaintext><img src=x onerror=alert(1)>`,
		`<div id="x" class="a b" style="color:red" data-x="1" ping="https://x.example" draggable=true>x</div>`,
		`<a href="https://ok.example/" target="_top">x</a>`,
		`<table><tr><td colspan="999999">x</td></tr></table>`,
		`<div>` + strings.Repeat(`<div>`, 300) + `deep`,
		`<p>unclosed <b>bold <i>italic`,
		"<p>\x00null</p>",
	}
	for _, in := range vectors {
		out, e := SanitizeCustomHTML(in)
		if e != nil {
			t.Fatal(in, e)
		}
		low := strings.ToLower(out.HTML)
		for _, never := range []string{"<script", "<iframe", "<svg", "<math", "<object", "<embed", "<form", "<input", "<button", "<base", "<meta", "<link", "<style", "<template", "<noscript", "<textarea", "<title", "<plaintext", "\x00"} {
			if strings.Contains(low, never) {
				t.Fatalf("%q kept %q: %q", in, never, out.HTML)
			}
		}
		assertCanonical(t, in, out.HTML)
	}
}

func TestSanitizeCustomHTMLKeepsUsefulMarkupAndReportsRemovals(t *testing.T) {
	in := `<!doctype html><html><head><title>t</title><style>p{}</style></head><body>
<header class="top bad|class" id="top"><h1 title="标题">欢迎 &amp; 你好 &lt;b&gt;</h1></header>
<p>链接：<a href="/doc/">文档</a> <a href="https://ok.example/a?b=1&amp;c=2" target="_blank" rel="opener">外站</a> <a href="mailto:ops@example.com">邮件</a> <a href="tel:+8610001">电话</a> <a href="#faq">问答</a></p>
<img src="/template-assets/` + testAsset + `" alt="图" width="320" height="200" loading="lazy">
<img src="/template-assets/` + testAsset + `" alt="again">
<table><thead><tr><th scope="col">A</th></tr></thead><tbody><tr><td rowspan="2">1</td></tr></tbody></table>
<details open><summary>问题</summary><p style="color:#c00;font-weight:700">答案</p></details>
<ol start="3" reversed><li value="5">x</li></ol>
<script>alert(1)</script><div onclick="alert(1)">文字</div><font color=red>旧</font>
</body></html>`
	out, e := SanitizeCustomHTML(in)
	if e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{
		`<header class="top" id="top">`, `<h1 title="标题">欢迎 &amp; 你好 &lt;b&gt;</h1>`,
		`<a href="/doc/">文档</a>`, `<a href="https://ok.example/a?b=1&amp;c=2" target="_blank" rel="noopener noreferrer">外站</a>`,
		`<a href="mailto:ops@example.com">`, `<a href="tel:+8610001">`, `<a href="#faq">`,
		`<img src="/template-assets/` + testAsset + `" alt="图" width="320" height="200" loading="lazy">`,
		`<th scope="col">A</th>`, `<td rowspan="2">1</td>`, `<details open><summary>问题</summary>`,
		`<p style="color:#c00;font-weight:700">答案</p>`, `<ol start="3" reversed><li value="5">x</li></ol>`,
		`<div>文字</div>`, `旧`,
	} {
		if !strings.Contains(out.HTML, want) {
			t.Errorf("missing %q in\n%s", want, out.HTML)
		}
	}
	if strings.Contains(out.HTML, "<title") || strings.Contains(out.HTML, "<style") || strings.Contains(out.HTML, "<font") || strings.Contains(out.HTML, `rel="opener"`) {
		t.Fatal("dropped markup kept", out.HTML)
	}
	if len(out.Assets) != 1 || out.Assets[0] != testAsset {
		t.Fatal("assets", out.Assets)
	}
	for key, n := range map[string]int{"<script>": 1, "onclick": 1, "<font>": 1, "<title>": 1, "<style>": 1} {
		if out.Removed[key] != n {
			t.Errorf("removed[%q]=%d, want %d (%v)", key, out.Removed[key], n, out.Removed)
		}
	}
	if _, ok := out.Removed["<html>"]; ok {
		t.Fatal("a pasted document wrapper is not worth reporting")
	}
	assertCanonical(t, in, out.HTML)
}

func TestSanitizeCustomHTMLLimits(t *testing.T) {
	var ce *CustomError
	if _, e := SanitizeCustomHTML(strings.Repeat("a", MaxCustomHTML+1)); !errors.As(e, &ce) || !errors.Is(e, ErrInvalid) {
		t.Fatal("oversized HTML accepted", e)
	}
	if _, e := SanitizeCustomHTML("\xff\xfe"); !errors.As(e, &ce) {
		t.Fatal("invalid UTF-8 accepted", e)
	}
	var many strings.Builder
	for i := 0; i <= maxCustomAssets; i++ {
		many.WriteString(`<img src="/template-assets/` + strings.Repeat("0", 60) + string("0123456789abcdef"[i%16]) + string("0123456789abcdef"[i/16]) + `00">`)
	}
	if _, e := SanitizeCustomHTML(many.String()); !errors.As(e, &ce) {
		t.Fatal("too many images accepted", e)
	}
	if _, e := SanitizeCustomHTML(strings.Repeat("<p>x</p>", MaxCustomHTML/8)); e != nil {
		t.Fatal("HTML at the limit refused", e)
	}
}

func TestCheckCustomCSSRefusesLoadsEscapesAndBreakouts(t *testing.T) {
	for _, bad := range []string{
		`body{background:url(https://evil.example/x.png)}`,
		`body{background:URL( "x.png" )}`,
		`body{background:ur/**/l(x)}`,
		`@import "https://evil.example/x.css";`,
		`@IMPORT url(x)`,
		`p{width:expression(alert(1))}`,
		`p{color:red}</style><script>alert(1)</script>`,
		`p{background:u\72l(x)}`,
		`p{behavior:url(x.htc)}`,
		`p{-moz-binding:url(x.xml#y)}`,
		`p{background:image-set("x.png" 1x)}`,
		`p{background:-webkit-image-set("x.png" 1x)}`,
		`p{background:image("x.png")}`,
		`p{background:cross-fade(a,b)}`,
		`p{background:element(#x)}`,
		`p{x:javascript:alert(1)}`,
		strings.Repeat("a", MaxCustomCSS+1),
		"p{color:red}\x00",
	} {
		var ce *CustomError
		if e := CheckCustomCSS(bad); !errors.As(e, &ce) || ce.Reason == "" {
			t.Errorf("CSS accepted: %q", bad)
		}
	}
	good := `/* 首页 */ :root{--x:#123} @media(max-width:600px){.a{display:none}} @keyframes spin{to{transform:rotate(1turn)}}
.hero{background:linear-gradient(135deg,var(--accent),#7c3aed);color:var(--on-accent);content:"a > b"}
.card>p:first-child{margin:0} .x:hover{opacity:.8}`
	if e := CheckCustomCSS(good); e != nil {
		t.Fatal("ordinary CSS refused", e)
	}
}

func TestCustomHomeRendersSanitizedContentInsideTheSystemFrameOrAlone(t *testing.T) {
	evil := `<h1 onclick="alert(1)">你好</h1><img src="/template-assets/` + testAsset + `" alt="图"><script>alert(1)</script><a href="javascript:alert(1)">x</a>`
	render := func(c *CustomContent, preview bool) string {
		t.Helper()
		var b strings.Builder
		r := Resolved{Type: "home", Name: "custom", Config: DefaultConfig("home", "custom"), Custom: c}
		if e := RenderHome(&b, r, HomeData{SiteName: "站点", RegistrationOpen: true, Preview: preview}); e != nil {
			t.Fatal(e)
		}
		return b.String()
	}
	framed := render(&CustomContent{Version: 7, HTML: evil, CSS: `.mine{color:red}`, Frame: true}, false)
	for _, want := range []string{`data-home-style="custom"`, `data-custom-version="7"`, `class="home home-custom"`, `<header class="site-header">`, `<h1>你好</h1>`, `<img src="/template-assets/` + testAsset + `" alt="图">`, `<style>.mine{color:red}</style>`, `href="/agreement.html">服务协议</a>`, `id="main-content"`} {
		if !strings.Contains(framed, want) {
			t.Errorf("framed page missing %q", want)
		}
	}
	alone := render(&CustomContent{Version: 8, HTML: evil, CSS: `.mine{color:blue}`, Frame: false}, false)
	if strings.Contains(alone, `class="site-header"`) || strings.Contains(alone, "home-footer") || !strings.Contains(alone, "home-custom-full") || !strings.Contains(alone, `<h1>你好</h1>`) || !strings.Contains(alone, "--accent:") {
		t.Fatal("full-page custom HTML must replace the system frame", alone)
	}
	preview := render(&CustomContent{Version: 9, HTML: evil, Frame: true}, true)
	if !strings.Contains(preview, `<img src="/console/v1/admin/templates/images/`+testAsset+`"`) || strings.Contains(preview, "/template-assets/") {
		t.Fatal("preview must show template images from the console", preview)
	}
	for _, page := range []string{framed, alone, preview} {
		low := strings.ToLower(page)
		if strings.Contains(low, "<script") || strings.Contains(low, "javascript:") || strings.Contains(low, "onclick") {
			t.Fatal("custom page carries script", page)
		}
	}
	sample := render(nil, false)
	if !strings.Contains(sample, "把这里换成你自己的首页") || !strings.Contains(sample, `data-custom-version="0"`) {
		t.Fatal("the sample is shown before a version is saved")
	}
	var b strings.Builder
	bad := Resolved{Type: "home", Name: "custom", Config: DefaultConfig("home", "custom"), Custom: &CustomContent{HTML: "<p>x</p>", CSS: `p{background:url(x)}`}}
	if e := RenderHome(&b, bad, HomeData{SiteName: "站点"}); !errors.Is(e, ErrInvalid) {
		t.Fatal("unchecked CSS rendered", e)
	}
	if e := RenderAgreement(&b, Resolved{Type: "home", Name: "custom", Config: DefaultConfig("home", "custom")}, HomeData{SiteName: "站点"}); e != nil {
		t.Fatal(e)
	}
}

func TestCustomSampleIsAcceptedAsIs(t *testing.T) {
	c := CustomSample()
	clean, e := checkCustom(c)
	if e != nil || len(clean.Removed) != 0 {
		t.Fatal("the starter page must pass the checks unchanged", e, clean.Removed)
	}
}
