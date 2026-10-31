package pagetemplate

import (
	"html/template"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// compiledTemplate is an uploaded version ready to render.
type compiledTemplate struct {
	kind string
	key  string
	// home: the whole page, with sentinels.
	home string
	// pay and qrpay: the body markup around {{content}}, the classes of the
	// template's <body>, its style sheets and its <style> CSS.
	before, after string
	bodyClass     string
	links         []string
	css           string
	removed       map[string]int
}

// UploadedTemplate is the version of an uploaded template that is shown.
type UploadedTemplate struct {
	Name, Title string
	Version     int
	Key         string
	compiled    *compiledTemplate
}

// Compiled reports whether the version has been compiled for rendering.
func (u *UploadedTemplate) Compiled() bool { return u != nil && u.compiled != nil }

// compilePackage compiles index.html for kind with the version key. With
// full, every style sheet of the package is checked too (uploads); rendering
// only needs index.html and the list of paths.
func compilePackage(kind, key string, files map[string][]byte, paths map[string]bool, full bool, warn *warnList) (*compiledTemplate, error) {
	index := string(files["index.html"])
	if kind == "home" {
		c := &compiledTemplate{kind: kind, key: key, home: compileHome(key, index, paths, warn)}
		if full {
			for _, p := range sortedPaths(files) {
				if extension(p) == "css" {
					warnMissingCSS(strings.TrimPrefix(string(files[p]), "\ufeff"), path.Dir(p), p, paths, warn)
				}
			}
		}
		return c, nil
	}
	ctx := &payContext{kind: kind, key: key, files: paths, warn: warn}
	out, removed := sanitizeMarkup(strings.TrimPrefix(index, "\ufeff"), ctx)
	if ctx.err != nil {
		return nil, ctx.err
	}
	if ctx.misplaced != "" {
		return nil, refuse("%s", ctx.misplaced)
	}
	switch {
	case ctx.contents == 0:
		return nil, refuse("支付页和收款页模板必须包含一次 {{content}}，系统在那里写入金额、订单、二维码和按钮")
	case ctx.contents > 1:
		return nil, refuse("{{content}} 出现了 %d 次，只能出现一次", ctx.contents)
	}
	if full {
		for _, p := range sortedPaths(files) {
			if extension(p) != "css" {
				continue
			}
			dir := path.Dir(p)
			if dir == "." {
				dir = ""
			}
			if _, e := checkPackageCSS(strings.TrimPrefix(string(files[p]), "\ufeff"), dir, p, key, paths, false, warn); e != nil {
				return nil, e
			}
		}
	}
	before, after, _ := strings.Cut(out, "\x01#\x02")
	return &compiledTemplate{kind: kind, key: key, before: before, after: after, bodyClass: ctx.bodyClass, links: ctx.links, css: ctx.css.String(), removed: removed}, nil
}

func sortedPaths(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for p := range files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func pathSet(files map[string][]byte) map[string]bool {
	out := make(map[string]bool, len(files))
	for p := range files {
		out[p] = true
	}
	return out
}

// compiledCache keeps compiled versions by file key. Versions never change,
// so an entry never goes stale; the cache only bounds its size.
var compiledCache = struct {
	sync.Mutex
	m     map[string]*compiledTemplate
	order []string
}{m: map[string]*compiledTemplate{}}

func cachedCompiled(key string) *compiledTemplate {
	compiledCache.Lock()
	defer compiledCache.Unlock()
	return compiledCache.m[key]
}

func cacheCompiled(c *compiledTemplate) {
	compiledCache.Lock()
	defer compiledCache.Unlock()
	if _, ok := compiledCache.m[c.key]; ok {
		return
	}
	if len(compiledCache.order) >= 32 {
		delete(compiledCache.m, compiledCache.order[0])
		compiledCache.order = compiledCache.order[1:]
	}
	compiledCache.m[c.key] = c
	compiledCache.order = append(compiledCache.order, c.key)
}

// colours are the validated appearance colours of a page.
type colours struct{ accent, onAccent, background, foreground string }

func pageColours(cfg Config, foreground string) colours {
	c := colours{accent: cfg.AccentColor, background: cfg.BackgroundColor}
	if !colorPattern.MatchString(c.accent) {
		c.accent = "#007c91"
	}
	if !colorPattern.MatchString(c.background) {
		c.background = "#f4f8fa"
	}
	c.onAccent = readable(c.accent, "#ffffff", "#0b1220")
	if contrast(c.onAccent, c.accent) < 4.5 {
		c.onAccent = readable(c.accent, "#ffffff", "#000000")
	}
	c.foreground = foreground
	if !colorPattern.MatchString(c.foreground) {
		c.foreground = readable(c.background, "#e6edf5", "#163047")
		if contrast(c.foreground, c.background) < 4.5 {
			c.foreground = readable(c.background, "#ffffff", "#000000")
		}
	}
	return c
}

func (c colours) vars(pad int) string {
	return ":root{--goepay-accent:" + c.accent + ";--goepay-on-accent:" + c.onAccent + ";--goepay-bg:" + c.background + ";--goepay-fg:" + c.foreground + ";--goepay-pad:" + strconv.Itoa(pad) + "px}"
}

const noticeBarStyle = `margin:0;padding:8px 16px;font:13px/1.6 system-ui,-apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;text-align:center;background:#fff4e5;color:#7a4b00`

// renderUploadHome writes an uploaded home page.
func renderUploadHome(w io.Writer, r Resolved, d HomeData) error {
	if r.Type != "home" || !r.Upload.Compiled() || r.Upload.compiled.kind != "home" {
		return ErrInvalid
	}
	cfg := r.Config
	if strings.TrimSpace(d.SiteName) == "" {
		d.SiteName = "GOEPAY"
	}
	if strings.TrimSpace(d.Title) == "" {
		d.Title = d.SiteName
	}
	if d.Notice == "" {
		d.Notice = cfg.Notice
	}
	c := pageColours(cfg, "")
	logo := d.LogoURL
	if logo == "" && assetPattern.MatchString(cfg.LogoAssetID) && !d.Preview {
		logo = "/template-assets/" + cfg.LogoAssetID
	}
	if logo == "" {
		logo = "/static/goepay-wordmark-onlight.svg?v=90cafbd35591"
		if contrast("#ffffff", c.background) > contrast("#0A0F1F", c.background) {
			logo = "/static/goepay-wordmark-ondark.svg?v=306905c49705"
		}
	}
	email := ""
	if d.Email != "" {
		email = "mailto:" + d.Email
	}
	values := map[string]string{
		"site_name": d.SiteName, "title": d.Title, "keywords": d.Keywords, "description": d.Description,
		"org": d.Org, "email": d.Email, "qq": d.QQ, "notice": d.Notice, "brand_name": cfg.BrandName,
		"year": strconv.Itoa(time.Now().Year()), "accent_color": c.accent, "background_color": c.background,
		"registration_open": strconv.FormatBool(d.RegistrationOpen), "test_open": strconv.FormatBool(d.TestOpen),
		"logo_url": logo, "qq_url": d.QQURL, "qq_group_url": d.QQGroup, "wework_url": d.WeWork, "email_url": email,
		"login_url": "/merchant/login", "register_url": "/merchant/login?enroll=register", "test_url": "/test",
		"doc_url": "/doc/", "agreement_url": "/agreement.html", "home_url": "/",
	}
	bar := ""
	switch {
	case d.Preview:
		bar = `<p style='` + noticeBarStyle + `'>仅为模板预览：脚本照常运行，但读不到登录状态，也不会下单。</p>`
	case d.Sandbox:
		bar = `<p style='` + noticeBarStyle + `'>本地模拟环境，不涉及真实资金。</p>`
	}
	page := d.PageURL
	if page == "" {
		page = "/"
	}
	_, e := io.WriteString(w, expand(r.Upload.compiled.home, values, page, bar, "<style>"+c.vars(28)+"</style>"))
	return e
}

// The CSS of the checkout block of an uploaded pay or qrpay template: the
// system styles, scoped to the block, in the --goepay-* variables. The
// template's own CSS loads after it and may override them.
const uploadBlockCSS = `.goepay-checkout,.goepay-checkout *,.goepay-checkout *::before,.goepay-checkout *::after{box-sizing:border-box}
.goepay-checkout{display:block;position:relative;width:100%;max-width:520px;margin:24px auto;padding:0 var(--goepay-pad) var(--goepay-pad);background:#fff;color:var(--goepay-fg);border-radius:18px;box-shadow:0 24px 48px -28px rgb(15 35 60/.45),0 2px 6px rgb(15 35 60/.06);font:15px/1.6 system-ui,-apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;text-align:left;overflow-wrap:anywhere}
.goepay-checkout .template-head{display:flex;align-items:center;justify-content:space-between;gap:12px;margin:0 calc(var(--goepay-pad)*-1);padding:16px var(--goepay-pad);border-bottom:1px solid #edf0f4}
.goepay-checkout .template-brand{display:flex;align-items:center;gap:10px;min-width:0;font-weight:700;font-size:16px}
.goepay-checkout .template-logo{width:auto;height:30px;max-width:120px;object-fit:contain}
.goepay-checkout .template-brand-name{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.goepay-checkout .template-secure{display:inline-flex;align-items:center;gap:4px;flex:none;padding:3px 10px;border-radius:999px;background:#e8f6ef;color:#17663f;font-size:12px;font-weight:600}
.goepay-checkout .template-secure svg{width:13px;height:13px}
.goepay-checkout h1{margin:0;font-size:15px;font-weight:600;line-height:1.6;color:color-mix(in srgb,var(--goepay-fg) 70%,#fff)}
.goepay-checkout .template-summary{padding:24px 0 6px;text-align:center}
.goepay-checkout .template-amount{margin:10px 0 18px}
.goepay-checkout .template-amount span{display:block;font-size:13px;color:color-mix(in srgb,var(--goepay-fg) 55%,#fff)}
.goepay-checkout .template-amount strong{display:block;margin-top:4px;font-size:40px;line-height:1.15;font-weight:700;color:var(--goepay-fg);font-variant-numeric:tabular-nums}
.goepay-checkout .template-amount small{margin-right:2px;font-size:24px;font-weight:600}
.goepay-checkout .template-meta{display:grid;grid-template-columns:auto 1fr;gap:10px 16px;margin:0;padding:14px 16px;border-radius:12px;background:#f6f8fb;font-size:13px;text-align:left}
.goepay-checkout .template-meta dt{color:color-mix(in srgb,var(--goepay-fg) 55%,#fff)}
.goepay-checkout .template-meta dd{margin:0;text-align:right;word-break:break-all;font-variant-numeric:tabular-nums}
.goepay-checkout .template-payee{padding:26px 0 8px;text-align:center}
.goepay-checkout .template-avatar{display:inline-grid;place-content:center;width:60px;height:60px;margin-bottom:12px;border-radius:50%;background:var(--goepay-accent);color:var(--goepay-on-accent);font-size:26px;font-weight:700}
.goepay-checkout .template-payee h1{font-size:19px;color:var(--goepay-fg)}
.goepay-checkout .template-payee-name{margin:6px 0 0;font-size:13px;color:color-mix(in srgb,var(--goepay-fg) 55%,#fff)}
.goepay-checkout .template-notice{margin:16px 0 0;padding:10px 12px;border-left:3px solid var(--goepay-accent);border-radius:8px;background:color-mix(in srgb,var(--goepay-accent) 7%,#fff);font-size:13px;white-space:pre-wrap}
.goepay-checkout .template-preview-note{margin:16px 0 0;padding:10px 12px;border:1px dashed #c98a1a;border-radius:8px;background:#fff8ea;color:#7a4b00;font-size:13px;text-align:center}
.goepay-checkout .template-sandbox{width:max-content;max-width:100%;margin:14px auto 0;padding:3px 12px;border-radius:999px;background:#fff4e5;color:#7a4b00;font-size:12px}
.goepay-checkout>p{margin:14px 0;font-size:14px;line-height:1.7;text-align:center;color:color-mix(in srgb,var(--goepay-fg) 72%,#fff)}
.goepay-checkout>img,.goepay-checkout>svg{display:block;width:248px;max-width:100%;height:auto;aspect-ratio:1;margin:18px auto 8px;padding:12px;background:#fff;border:1px solid #e3e8ef;border-radius:14px;box-shadow:0 10px 24px -18px rgb(15 35 60/.6)}
.goepay-checkout>a,.goepay-checkout button{display:flex;align-items:center;justify-content:center;width:100%;min-height:46px;margin:18px 0 6px;padding:12px 16px;border:0;border-radius:12px;background:var(--goepay-accent);color:var(--goepay-on-accent);font-family:inherit;font-size:16px;font-weight:600;line-height:1.2;text-decoration:none;cursor:pointer}
.goepay-checkout button:disabled{opacity:.6;cursor:not-allowed}
.goepay-checkout form{margin:18px 0 0}
.goepay-checkout fieldset{min-width:0;margin:0;padding:0;border:0}
.goepay-checkout legend{margin:0 0 10px;padding:0;font-size:13px;color:color-mix(in srgb,var(--goepay-fg) 55%,#fff)}
.goepay-checkout label{display:block;margin:14px 0;font-size:13px;color:color-mix(in srgb,var(--goepay-fg) 70%,#fff)}
.goepay-checkout fieldset label{display:flex;align-items:center;gap:10px;margin:8px 0;padding:13px 14px;border:1px solid #e3e8ef;border-radius:12px;font-size:15px;color:var(--goepay-fg);cursor:pointer}
.goepay-checkout fieldset label:has(input:checked){border-color:var(--goepay-accent);background:color-mix(in srgb,var(--goepay-accent) 7%,#fff)}
.goepay-checkout input[type=radio]{width:18px;height:18px;margin:0;accent-color:var(--goepay-accent)}
.goepay-checkout input:not([type=radio]):not([type=hidden]),.goepay-checkout select{display:block;width:100%;margin-top:6px;padding:11px 12px;border:1px solid #d5dce5;border-radius:12px;background:#fff;color:var(--goepay-fg);font:inherit;font-size:15px}
.goepay-checkout input:focus,.goepay-checkout select:focus{border-color:var(--goepay-accent);outline:2px solid color-mix(in srgb,var(--goepay-accent) 40%,transparent);outline-offset:1px}
.goepay-checkout input[name=amount]:not([type=radio]):not([type=hidden]){padding:14px 12px;font-size:28px;font-weight:700;text-align:center}
.goepay-checkout pre{margin:12px 0;padding:12px;border-radius:10px;background:#f6f8fb;font-size:12px;white-space:pre-wrap;word-break:break-all}
.goepay-checkout .template-demo-label{display:block;margin:14px 0 6px;font-size:13px;color:color-mix(in srgb,var(--goepay-fg) 70%,#fff)}
.goepay-checkout .template-demo-input{display:block;padding:11px 12px;border:1px solid #d5dce5;border-radius:12px;font-size:15px;color:color-mix(in srgb,var(--goepay-fg) 55%,#fff)}
.goepay-checkout .template-demo-amount{padding:14px 12px;font-size:28px;font-weight:700;text-align:center;color:var(--goepay-fg)}
.goepay-checkout .template-demo-button{display:flex;align-items:center;justify-content:center;min-height:46px;margin:18px 0 6px;border-radius:12px;background:var(--goepay-accent);color:var(--goepay-on-accent);font-size:16px;font-weight:600}
@media(max-width:560px){.goepay-checkout{margin:12px auto;border-radius:14px}}
`

// uploadFrame is what the opening of an uploaded pay or qrpay page shows.
type uploadFrame struct {
	frame
	UploadLinks  []string
	UploadCSS    template.CSS
	UploadClass  string
	UploadBefore template.HTML
}

var uploadOpening = template.Must(opening.New("upload-open").Parse(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">{{if .Page.Keywords}}<meta name="keywords" content="{{.Page.Keywords}}">{{end}}{{if .Page.Description}}<meta name="description" content="{{.Page.Description}}">{{end}}<title>{{.Page.Title}}</title><style>{{.CSS}}</style>{{range .UploadLinks}}<link rel="stylesheet" href="{{.}}">{{end}}{{if .UploadCSS}}<style>{{.UploadCSS}}</style>{{end}}<body{{if .UploadClass}} class="{{.UploadClass}}"{{end}} data-template="{{.Name}}" data-template-type="{{.Type}}" data-template-source="upload">{{.UploadBefore}}<main class="goepay-checkout goepay-checkout-{{.Type}}">{{template "block" .}}`))

// openUpload writes an uploaded pay or qrpay page up to the checkout content
// and returns the closing to write after it.
func openUpload(w io.Writer, f frame) (string, error) {
	c := f.Upload.compiled
	if c == nil || c.kind != f.Type {
		return "", ErrInvalid
	}
	orderName := ""
	if f.Config.ShowOrderName {
		orderName = f.Page.OrderName
	}
	values := map[string]string{
		"title": f.Page.Title, "amount": f.Page.Amount, "trade_no": f.Page.TradeNo, "order_name": orderName,
		"merchant_name": f.Page.MerchantName, "brand_name": f.Config.BrandName,
		"year": strconv.Itoa(time.Now().Year()), "logo_url": f.LogoURL,
	}
	colour := pageColours(f.Config, f.TextColor)
	f.CSS = template.CSS(colour.vars(f.Padding) + "\n" + uploadBlockCSS)
	u := uploadFrame{frame: f, UploadLinks: c.links, UploadCSS: template.CSS(c.css), UploadClass: c.bodyClass, UploadBefore: template.HTML(expand(c.before, values, "", "", ""))}
	if e := uploadOpening.ExecuteTemplate(w, "upload-open", u); e != nil {
		return "", e
	}
	return "</main>" + expand(c.after, values, "", "", "") + "</body></html>", nil
}
