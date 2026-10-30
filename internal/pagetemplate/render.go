package pagetemplate

import (
	"encoding/json"
	"html/template"
	"io"
	"strconv"
	"strings"
	"unicode"
)

// The pay and qrpay frames. Open writes everything up to the page content;
// the checkout, cashier, mini-launch and collection handlers then write their
// own content (paragraphs, the QR picture, a jump link or button, a form) into
// <main> and close it. The CSS below therefore styles those plain elements as
// they arrive: no handler has to know the template.

type frame struct {
	Resolved
	Page
	Width, Padding int
	LogoURL        string
	TextColor      string
	CSS            template.CSS
	Initial        string
}

// $ACCENT, $ONACCENT, $BG, $FG, $WIDTH and $PAD are replaced with validated
// values at render time; $ONACCENT is picked for contrast on $ACCENT.
const frameBaseCSS = `*{box-sizing:border-box}
body{margin:0;min-height:100vh;font:15px/1.6 system-ui,-apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;background:$BG;color:$FG;overflow-wrap:anywhere}
.template-band{position:absolute;inset:0 0 auto 0;height:220px;background:linear-gradient(160deg,$ACCENT,color-mix(in srgb,$ACCENT 55%,$BG))}
main{position:relative;max-width:$WIDTHpx;margin:48px auto 32px;padding:0 $PADpx $PADpx;background:#fff;border-radius:18px;box-shadow:0 24px 48px -28px rgb(15 35 60/.45),0 2px 6px rgb(15 35 60/.06)}
.template-head{display:flex;align-items:center;justify-content:space-between;gap:12px;margin:0 -$PADpx;padding:16px $PADpx;border-bottom:1px solid #edf0f4}
.template-brand{display:flex;align-items:center;gap:10px;min-width:0;font-weight:700;font-size:16px}
.template-logo{width:auto;height:30px;max-width:120px;object-fit:contain}
.template-brand-name{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.template-secure{display:inline-flex;align-items:center;gap:4px;flex:none;padding:3px 10px;border-radius:999px;background:#e8f6ef;color:#17663f;font-size:12px;font-weight:600}
.template-secure svg{width:13px;height:13px}
h1{margin:0;font-size:15px;font-weight:600;color:color-mix(in srgb,$FG 70%,#fff)}
.template-summary{padding:24px 0 6px;text-align:center}
.template-amount{margin:10px 0 18px}
.template-amount span{display:block;font-size:13px;color:color-mix(in srgb,$FG 55%,#fff)}
.template-amount strong{display:block;margin-top:4px;font-size:40px;line-height:1.15;font-weight:700;color:$FG;font-variant-numeric:tabular-nums}
.template-amount small{margin-right:2px;font-size:24px;font-weight:600}
.template-meta{display:grid;grid-template-columns:auto 1fr;gap:10px 16px;margin:0;padding:14px 16px;border-radius:12px;background:#f6f8fb;font-size:13px;text-align:left}
.template-meta dt{color:color-mix(in srgb,$FG 55%,#fff)}
.template-meta dd{margin:0;text-align:right;word-break:break-all;font-variant-numeric:tabular-nums}
.template-payee{padding:26px 0 8px;text-align:center}
.template-avatar{display:inline-grid;place-content:center;width:60px;height:60px;margin-bottom:12px;border-radius:50%;background:$ACCENT;color:$ONACCENT;font-size:26px;font-weight:700;box-shadow:0 8px 20px -10px $ACCENT}
.template-payee h1{font-size:19px;color:$FG}
.template-payee-name{margin:6px 0 0;font-size:13px;color:color-mix(in srgb,$FG 55%,#fff)}
.template-notice{margin:16px 0 0;padding:10px 12px;border-left:3px solid $ACCENT;border-radius:8px;background:color-mix(in srgb,$ACCENT 7%,#fff);font-size:13px;white-space:pre-wrap}
.template-preview-note{margin:16px 0 0;padding:10px 12px;border:1px dashed #c98a1a;border-radius:8px;background:#fff8ea;color:#7a4b00;font-size:13px;text-align:center}
.template-sandbox{width:max-content;max-width:100%;margin:14px auto 0;padding:3px 12px;border-radius:999px;background:#fff4e5;color:#7a4b00;font-size:12px}
main>p{margin:14px 0;font-size:14px;line-height:1.7;text-align:center;color:color-mix(in srgb,$FG 72%,#fff)}
main>img,main>svg{display:block;width:248px;height:248px;margin:18px auto 8px;padding:12px;background:#fff;border:1px solid #e3e8ef;border-radius:14px;box-shadow:0 10px 24px -18px rgb(15 35 60/.6)}
main>a,main button{display:flex;align-items:center;justify-content:center;width:100%;min-height:46px;margin:18px 0 6px;padding:12px 16px;border:0;border-radius:12px;background:$ACCENT;color:$ONACCENT;font-family:inherit;font-size:16px;font-weight:600;line-height:1.2;text-decoration:none;cursor:pointer;box-shadow:0 10px 20px -14px $ACCENT}
main button:disabled{opacity:.6;cursor:not-allowed}
main form{margin:18px 0 0}
main fieldset{min-width:0;margin:0;padding:0;border:0}
main legend{margin:0 0 10px;padding:0;font-size:13px;color:color-mix(in srgb,$FG 55%,#fff)}
main label{display:block;margin:14px 0;font-size:13px;color:color-mix(in srgb,$FG 70%,#fff)}
main fieldset label{display:flex;align-items:center;gap:10px;margin:8px 0;padding:13px 14px;border:1px solid #e3e8ef;border-radius:12px;font-size:15px;color:$FG;cursor:pointer}
main fieldset label:has(input:checked){border-color:$ACCENT;background:color-mix(in srgb,$ACCENT 7%,#fff)}
main input[type=radio]{width:18px;height:18px;margin:0;accent-color:$ACCENT}
main input:not([type=radio]):not([type=hidden]),main select{display:block;width:100%;margin-top:6px;padding:11px 12px;border:1px solid #d5dce5;border-radius:12px;background:#fff;color:$FG;font:inherit;font-size:15px}
main input:focus,main select:focus{border-color:$ACCENT;outline:2px solid color-mix(in srgb,$ACCENT 40%,transparent);outline-offset:1px}
main input[name=amount]:not([type=radio]):not([type=hidden]){padding:14px 12px;font-size:28px;font-weight:700;text-align:center}
main pre{margin:12px 0;padding:12px;border-radius:10px;background:#f6f8fb;font-size:12px;white-space:pre-wrap;word-break:break-all}
.template-demo-label{display:block;margin:14px 0 6px;font-size:13px;color:color-mix(in srgb,$FG 70%,#fff)}
.template-demo-input{display:block;padding:11px 12px;border:1px solid #d5dce5;border-radius:12px;font-size:15px;color:color-mix(in srgb,$FG 55%,#fff)}
.template-demo-amount{padding:14px 12px;font-size:28px;font-weight:700;text-align:center;color:$FG}
.template-demo-button{display:flex;align-items:center;justify-content:center;min-height:46px;margin:18px 0 6px;border-radius:12px;background:$ACCENT;color:$ONACCENT;font-size:16px;font-weight:600}
@media(max-width:520px){.template-band{display:none}main{max-width:none;min-height:100vh;margin:0;border-radius:0;box-shadow:none}}
`

// The compact variant: a narrower card with a top accent rule instead of the
// colour band, the amount on one line and a lighter order summary.
const frameCompactCSS = `.template-compact{margin:5vh auto;border-top:4px solid $ACCENT;border-radius:14px}
.template-compact .template-head{padding-bottom:6px;border-bottom:0}
.template-compact .template-summary{padding:12px 0 4px;text-align:left}
.template-compact .template-amount{display:flex;align-items:baseline;justify-content:space-between;margin:8px 0 14px;padding:0 0 12px;border-bottom:1px dashed #e3e8ef}
.template-compact .template-amount strong{margin:0;font-size:30px}
.template-compact .template-amount small{font-size:18px}
.template-compact .template-meta{padding:0;background:none}
.template-compact .template-payee{display:grid;grid-template-columns:auto 1fr;column-gap:14px;align-items:center;padding:14px 0 6px;text-align:left}
.template-compact .template-avatar{grid-row:span 2;width:48px;height:48px;margin:0;font-size:20px}
.template-compact .template-payee h1{font-size:17px}
.template-compact .template-payee-name{margin:2px 0 0}
@media(max-width:520px){.template-compact{margin:0;border-radius:0}}
`

const lockSVG = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/></svg>`

var opening = template.Must(template.New("page-open").Parse(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">{{if .Page.Keywords}}<meta name="keywords" content="{{.Page.Keywords}}">{{end}}{{if .Page.Description}}<meta name="description" content="{{.Page.Description}}">{{end}}<title>{{.Page.Title}}</title><style>{{.CSS}}</style><body data-template="{{.Name}}" data-template-type="{{.Type}}">{{if ne .Name "compact"}}<div class="template-band" aria-hidden="true"></div>{{end}}<main class="template-{{.Name}}"><header class="template-head"><span class="template-brand">{{if .LogoURL}}<img class="template-logo" alt="品牌图片" src="{{.LogoURL}}">{{end}}<span class="template-brand-name">{{.Config.BrandName}}</span></span><span class="template-secure">` + lockSVG + `安全支付</span></header>{{if eq .Type "qrpay"}}<section class="template-payee"><span class="template-avatar" aria-hidden="true">{{.Initial}}</span><h1>{{.Page.Title}}</h1>{{if .MerchantName}}<p class="template-payee-name">收款商户：{{.MerchantName}}</p>{{end}}</section>{{else}}<section class="template-summary"><h1>{{.Page.Title}}</h1>{{if .Amount}}<p class="template-amount"><span>支付金额</span><strong><small>¥</small>{{.Amount}}</strong></p>{{end}}{{if or .TradeNo (and .Config.ShowOrderName .OrderName) .MerchantName}}<dl class="template-meta">{{if .TradeNo}}<dt>订单号</dt><dd>{{.TradeNo}}</dd>{{end}}{{if and .Config.ShowOrderName .OrderName}}<dt>商品名称</dt><dd>{{.OrderName}}</dd>{{end}}{{if .MerchantName}}<dt>收款商户</dt><dd>{{.MerchantName}}</dd>{{end}}</dl>{{end}}</section>{{end}}{{if .Preview}}<p class="template-preview-note">仅为模板预览，不发起支付。</p>{{end}}{{if .Config.Notice}}<p class="template-notice">{{.Config.Notice}}</p>{{end}}{{if .Sandbox}}<p class="template-sandbox">本地模拟环境，不涉及真实资金。</p>{{end}}`))

// initial is the first character of the payee shown in the qrpay avatar.
func initial(names ...string) string {
	for _, n := range names {
		for _, r := range strings.TrimSpace(n) {
			return string(unicode.ToUpper(r))
		}
	}
	return "G"
}

func Open(w io.Writer, r Resolved, p Page) error {
	if p.BrandName != "" {
		r.Config.BrandName = p.BrandName
	}
	raw, _ := json.Marshal(r.Config.Public(r.Type))
	cfg, e := ParseConfig(r.Type, r.Name, raw)
	if e != nil {
		return e
	}
	r.Config = cfg
	f := frame{Resolved: r, Page: p, Width: 520, Padding: 28, TextColor: "#163047"}
	if p.ForegroundColor != "" {
		if !colorPattern.MatchString(p.ForegroundColor) {
			return ErrInvalid
		}
		f.TextColor = p.ForegroundColor
	}
	if r.Name == "compact" {
		f.Width = 440
	}
	if cfg.Density == "compact" {
		f.Padding = 18
	}
	if cfg.LogoAssetID != "" {
		if p.Preview {
			f.LogoURL = "/console/v1/admin/templates/images/" + cfg.LogoAssetID
		} else {
			f.LogoURL = "/template-assets/" + cfg.LogoAssetID
		}
	}
	if p.SiteLogo {
		if r.Type != "home" || p.Preview {
			return ErrInvalid
		}
		f.LogoURL = "/static/img/logo.png"
	}
	f.Initial = initial(p.MerchantName, cfg.BrandName)
	css := frameBaseCSS
	if r.Name == "compact" {
		css += frameCompactCSS
	}
	f.CSS = template.CSS(strings.NewReplacer(
		"$ONACCENT", readable(cfg.AccentColor, "#ffffff", "#0b1220"),
		"$ACCENT", cfg.AccentColor,
		"$BG", cfg.BackgroundColor,
		"$FG", f.TextColor,
		"$WIDTH", strconv.Itoa(f.Width),
		"$PAD", strconv.Itoa(f.Padding),
	).Replace(css))
	return opening.Execute(w, f)
}
func Close(w io.Writer) error { _, e := io.WriteString(w, "</main></body></html>"); return e }
