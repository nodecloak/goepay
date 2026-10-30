// Package devdoc renders the public developer documentation at /doc/, modelled
// on epay_pro app/view/doc: a top bar, a 目录 sidebar and one page per
// interface. Pages are html/template fragments in pages/<slug>.html that define
// "content"; every value that differs per site (site name, logo, merchant API
// address, enabled pay types) comes from Site, so no page hard-codes a host.
//
// The pages carry no script, form or third-party asset: the stylesheet is
// served from /doc/doc.css and the SDK archives from /doc/sdk/.
package devdoc

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

//go:embed layout.html doc.css pages/*.html sdk/*.zip
var files embed.FS

// PayType is one enabled 支付方式: Type is the 调用值 a merchant sends as
// the type parameter, Name is its 描述.
type PayType struct {
	Type string
	Name string
}

// Site holds the values that differ between deployments.
type Site struct {
	SiteName string
	// LogoURL is empty when the site has no uploaded logo.
	LogoURL string
	// API is the merchant API base address ending in "/", for example
	// "https://pay.example.com/". It is "/" when no public origin is set, so
	// the pages show plain paths instead of guessing a host.
	API      string
	PayTypes []PayType
}

// Node is one 目录 entry: a page (Slug set) or a group of pages.
type Node struct {
	Title    string
	Slug     string
	Children []Node
}

// Catalog mirrors the original 目录 (epay_pro app/view/doc catalogList) and
// keeps its order. The original 投诉相关接口 group is left out because GOEPAY
// has no merchant complaint API: /api/complain/* answers "capability not
// implemented".
var Catalog = []Node{
	{Title: "接口说明", Slug: "index"},
	{Title: "签名规则", Slug: "sign_note"},
	{Title: "支付方式列表", Slug: "paytype"},
	{Title: "支付相关接口", Children: []Node{
		{Title: "页面跳转支付", Slug: "pay_submit"},
		{Title: "统一下单接口", Slug: "pay_create"},
		{Title: "订单查询", Slug: "pay_query"},
		{Title: "支付结果通知", Slug: "pay_notify"},
		{Title: "订单退款", Slug: "pay_refund"},
		{Title: "订单退款查询", Slug: "pay_refundquery"},
		{Title: "关闭订单", Slug: "pay_close"},
	}},
	{Title: "商户相关接口", Children: []Node{
		{Title: "查询商户信息", Slug: "merchant_info"},
		{Title: "查询订单列表", Slug: "merchant_orders"},
	}},
	{Title: "代付相关接口", Children: []Node{
		{Title: "转账发起", Slug: "transfer_submit"},
		{Title: "转账查询", Slug: "transfer_query"},
		{Title: "可用余额查询", Slug: "transfer_balance"},
	}},
	{Title: "SDK下载", Slug: "sdk"},
}

// hidden pages are reached by a link from another page but are not listed in
// 目录, as in the original.
var hidden = []Node{{Title: "V1旧版接口文档", Slug: "v1_legacy_api"}}

type navItem struct {
	Title    string
	Href     string
	Current  bool
	Open     bool
	Children []navItem
}

type view struct {
	Site
	Title string
	Slug  string
	Nav   []navItem
	CSS   string
	Year  int
}

var (
	pages    = map[string]*template.Template{}
	titles   = map[string]string{}
	notFound *template.Template
	css      []byte
	cssURL   string
	sdk      = map[string][]byte{}
)

func init() {
	for _, n := range append(append([]Node{}, Catalog...), hidden...) {
		if n.Slug != "" {
			titles[n.Slug] = n.Title
		}
		for _, c := range n.Children {
			titles[c.Slug] = c.Title
		}
	}
	for slug := range titles {
		// A page that is not written yet stays a 404; TestEveryCatalogPageRenders
		// fails until it exists, so a missing page never ships silently.
		if _, err := fs.Stat(files, "pages/"+slug+".html"); err != nil {
			continue
		}
		pages[slug] = template.Must(template.New("layout.html").ParseFS(files, "layout.html", "pages/"+slug+".html"))
	}
	notFound = template.Must(template.New("layout.html").ParseFS(files, "layout.html", "pages/notfound.html"))
	var err error
	if css, err = files.ReadFile("doc.css"); err != nil {
		panic(err)
	}
	sum := sha256.Sum256(css)
	cssURL = "/doc/doc.css?v=" + hex.EncodeToString(sum[:5])
	archives, err := fs.Glob(files, "sdk/*.zip")
	if err != nil {
		panic(err)
	}
	for _, name := range archives {
		b, err := files.ReadFile(name)
		if err != nil {
			panic(err)
		}
		sdk[strings.TrimPrefix(name, "sdk/")] = b
	}
}

// Match reports whether path belongs to the documentation site.
func Match(path string) bool {
	return path == "/doc" || strings.HasPrefix(path, "/doc/")
}

// Has reports whether slug is a published page.
func Has(slug string) bool {
	_, ok := pages[slug]
	return ok
}

func nav(current string) []navItem {
	var build func([]Node) []navItem
	build = func(nodes []Node) []navItem {
		out := make([]navItem, 0, len(nodes))
		for _, n := range nodes {
			item := navItem{Title: n.Title, Current: n.Slug != "" && n.Slug == current}
			if n.Slug != "" {
				item.Href = "/doc/" + n.Slug + ".html"
			}
			item.Children = build(n.Children)
			for _, c := range item.Children {
				if c.Current {
					item.Open = true
				}
			}
			out = append(out, item)
		}
		return out
	}
	return build(Catalog)
}

// Render writes the page for slug. It reports false when slug is unknown,
// after writing the 404 page instead.
func Render(b *bytes.Buffer, slug string, site Site, now time.Time) (bool, error) {
	if site.API == "" {
		site.API = "/"
	}
	t, ok := pages[slug]
	title := titles[slug]
	if !ok {
		t, title, slug = notFound, "页面不存在", ""
	}
	return ok, t.ExecuteTemplate(b, "layout.html", view{Site: site, Title: title, Slug: slug, Nav: nav(slug), CSS: cssURL, Year: now.Year()})
}

// Serve answers one /doc/ request. load supplies the per-site values; it is
// called only for HTML pages.
func Serve(w http.ResponseWriter, r *http.Request, load func(*http.Request) (Site, error)) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := r.URL.Path
	switch {
	case path == "/doc" || path == "/doc/":
		http.Redirect(w, r, "/doc/index.html", http.StatusFound)
		return
	case path == "/doc/doc.css":
		h.Set("Content-Type", "text/css; charset=utf-8")
		h.Set("Cache-Control", "public, max-age=86400")
		write(w, r, http.StatusOK, css)
		return
	case strings.HasPrefix(path, "/doc/sdk/"):
		name := strings.TrimPrefix(path, "/doc/sdk/")
		b, ok := sdk[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		h.Set("Content-Type", "application/zip")
		h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
		h.Set("Cache-Control", "public, max-age=86400")
		write(w, r, http.StatusOK, b)
		return
	}
	// The original accepts /doc/<page> with or without the .html suffix.
	slug := strings.TrimSuffix(strings.TrimPrefix(path, "/doc/"), ".html")
	site, err := load(r)
	if err != nil {
		http.Error(w, "documentation unavailable", http.StatusServiceUnavailable)
		return
	}
	var b bytes.Buffer
	found, err := Render(&b, slug, site, time.Now())
	if err != nil {
		http.Error(w, "documentation unavailable", http.StatusInternalServerError)
		return
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	status := http.StatusOK
	if !found {
		status = http.StatusNotFound
	}
	write(w, r, status, b.Bytes())
}

func write(w http.ResponseWriter, r *http.Request, status int, body []byte) {
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
