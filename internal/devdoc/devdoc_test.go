package devdoc

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

var testSite = Site{SiteName: "示例支付", API: "https://pay.example.com/", PayTypes: []PayType{{Type: "alipay", Name: "支付宝"}, {Type: "wxpay", Name: "微信支付"}}}

func allSlugs() []string {
	var out []string
	for _, n := range append(append([]Node{}, Catalog...), hidden...) {
		if n.Slug != "" {
			out = append(out, n.Slug)
		}
		for _, c := range n.Children {
			out = append(out, c.Slug)
		}
	}
	return out
}

func render(t *testing.T, slug string, site Site) string {
	t.Helper()
	var b bytes.Buffer
	found, err := Render(&b, slug, site, time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC))
	if err != nil || !found {
		t.Fatalf("%s: found=%v err=%v", slug, found, err)
	}
	return b.String()
}

var (
	hrefPattern = regexp.MustCompile(`href="([^"]*)"`)
	idPattern   = regexp.MustCompile(`\sid="([^"]*)"`)
)

func TestEveryCatalogPageRenders(t *testing.T) {
	for _, slug := range allSlugs() {
		t.Run(slug, func(t *testing.T) {
			if !Has(slug) {
				t.Fatalf("page %s is listed but pages/%s.html is missing", slug, slug)
			}
			out := render(t, slug, testSite)
			if !strings.Contains(out, "<h1") || !strings.Contains(out, "<title>"+titles[slug]+" - 开发文档 - 示例支付</title>") {
				t.Fatalf("%s: missing heading or title", slug)
			}
			for _, bad := range []string{"{{", "}}", "{:", "{$", "<script", "<form", "<iframe", "码上付", "ma3fu", "pay.www.com", "src=\"http", "style=\""} {
				if strings.Contains(out, bad) {
					t.Fatalf("%s: rendered page contains %q", slug, bad)
				}
			}
			ids := map[string]bool{}
			for _, m := range idPattern.FindAllStringSubmatch(out, -1) {
				if ids[m[1]] {
					t.Fatalf("%s: duplicate id %q", slug, m[1])
				}
				ids[m[1]] = true
			}
			// The 目录 links are covered by Has above; check the article's own links.
			_, article, _ := strings.Cut(out, `<article class="doc-article">`)
			article, _, _ = strings.Cut(article, "</article>")
			if strings.Count(article, "<h1") != 1 {
				t.Fatalf("%s: the article needs exactly one <h1>", slug)
			}
			if strings.Contains(article, "<table") && strings.Count(article, "<table") != strings.Count(article, `<div class="doc-table"><table`) {
				t.Fatalf("%s: every table must sit directly in <div class=\"doc-table\">", slug)
			}
			for _, m := range hrefPattern.FindAllStringSubmatch(article, -1) {
				href := m[1]
				switch {
				case strings.HasPrefix(href, "#"):
					if !ids[strings.TrimPrefix(href, "#")] {
						t.Fatalf("%s: in-page link %q has no target", slug, href)
					}
				case strings.HasPrefix(href, "/doc/sdk/"):
					if _, ok := sdk[strings.TrimPrefix(href, "/doc/sdk/")]; !ok {
						t.Fatalf("%s: SDK link %q has no archive", slug, href)
					}
				case strings.HasPrefix(href, "/doc/doc.css"), href == "/":
				case strings.HasPrefix(href, "/doc/"):
					target, anchor, _ := strings.Cut(strings.TrimPrefix(href, "/doc/"), "#")
					target = strings.TrimSuffix(target, ".html")
					if !Has(target) {
						t.Fatalf("%s: link %q points to a missing page", slug, href)
					}
					if anchor != "" && !strings.Contains(render(t, target, testSite), ` id="`+anchor+`"`) {
						t.Fatalf("%s: link %q points to a missing anchor", slug, href)
					}
				case strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "http://"):
					// Example URLs in the text are allowed; nothing is loaded from them.
				default:
					t.Fatalf("%s: unexpected link %q", slug, href)
				}
			}
			if slug != "v1_legacy_api" && !strings.Contains(out, `aria-current="page"`) {
				t.Fatalf("%s: 目录 does not mark the current page", slug)
			}
		})
	}
}

func TestAPIAddressComesFromSite(t *testing.T) {
	out := render(t, "index", testSite)
	if !strings.Contains(out, "https://pay.example.com/") {
		t.Fatal("configured API address not shown")
	}
	out = render(t, "index", Site{SiteName: "示例支付"})
	if strings.Contains(out, "https://pay.example.com/") || !strings.Contains(out, `<code class="doc-url">/</code>`) {
		t.Fatal("without a configured origin the page must show plain paths")
	}
}

func TestSiteValuesAreEscaped(t *testing.T) {
	out := render(t, "index", Site{SiteName: `<b>"x"</b>`, API: "/"})
	if strings.Contains(out, `<b>"x"</b>`) || !strings.Contains(out, "&lt;b&gt;") {
		t.Fatal("site name was not escaped")
	}
}

func TestCurrentGroupIsOpen(t *testing.T) {
	out := render(t, "index", testSite)
	if strings.Contains(out, "<details open>") {
		t.Fatal("no group should be open on a top-level page")
	}
	if !Has("pay_query") {
		t.Skip("pay_query not written yet")
	}
	out = render(t, "pay_query", testSite)
	if !strings.Contains(out, `<details open><summary>支付相关接口</summary>`) || !strings.Contains(out, `<a href="/doc/pay_query.html" aria-current="page">订单查询</a>`) {
		t.Fatal("the group of the current page is not open")
	}
}

func serve(t *testing.T, method, target string, load func(*http.Request) (Site, error)) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	Serve(w, httptest.NewRequest(method, target, nil), load)
	return w
}

func okSite(*http.Request) (Site, error) { return testSite, nil }

func TestServeRoutes(t *testing.T) {
	for _, p := range []string{"/doc", "/doc/"} {
		w := serve(t, "GET", p, okSite)
		if w.Code != http.StatusFound || w.Header().Get("Location") != "/doc/index.html" {
			t.Fatalf("%s: %d %q", p, w.Code, w.Header().Get("Location"))
		}
	}
	for _, p := range []string{"/doc/index.html", "/doc/index"} {
		w := serve(t, "GET", p, okSite)
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "接口说明及规范") {
			t.Fatalf("%s: %d %s", p, w.Code, w.Header().Get("Content-Type"))
		}
		if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "style-src 'self'") || strings.Contains(csp, "script-src") {
			t.Fatalf("%s: CSP %q", p, csp)
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: headers %v", p, w.Header())
		}
	}
	for _, p := range []string{"/doc/missing.html", "/doc/notfound.html", "/doc/../index.html", "/doc/index.html/x", "/doc/complain_batch.html"} {
		w := serve(t, "GET", p, okSite)
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "页面不存在") {
			t.Fatalf("%s: %d", p, w.Code)
		}
	}
	if w := serve(t, "POST", "/doc/index.html", okSite); w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST: %d", w.Code)
	}
	if w := serve(t, "HEAD", "/doc/index.html", okSite); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("HEAD: %d %d", w.Code, w.Body.Len())
	}
	if w := serve(t, "GET", "/doc/index.html", func(*http.Request) (Site, error) { return Site{}, errors.New("db down") }); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("load failure: %d", w.Code)
	}
	w := serve(t, "GET", "/doc/doc.css", okSite)
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/css; charset=utf-8" || !bytes.Equal(w.Body.Bytes(), css) {
		t.Fatalf("css: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	for name, body := range sdk {
		w := serve(t, "GET", "/doc/sdk/"+name, okSite)
		if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" || !bytes.Equal(w.Body.Bytes(), body) || !bytes.HasPrefix(body, []byte("PK")) {
			t.Fatalf("sdk %s: %d", name, w.Code)
		}
		if w.Header().Get("Content-Disposition") != `attachment; filename="`+name+`"` {
			t.Fatalf("sdk %s: disposition %q", name, w.Header().Get("Content-Disposition"))
		}
	}
	if len(sdk) != 2 {
		t.Fatalf("expected the V1 and V2 SDK archives, got %d", len(sdk))
	}
	if w := serve(t, "GET", "/doc/sdk/missing.zip", okSite); w.Code != http.StatusNotFound {
		t.Fatalf("missing sdk: %d", w.Code)
	}
}

func TestMatch(t *testing.T) {
	for p, want := range map[string]bool{"/doc": true, "/doc/": true, "/doc/index.html": true, "/docs": false, "/document": false, "/": false, "/api/pay/submit": false} {
		if Match(p) != want {
			t.Fatalf("Match(%q) != %v", p, want)
		}
	}
}
