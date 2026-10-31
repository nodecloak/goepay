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
// <main> and finish with the closing Open returned (an uploaded template has
// more markup after <main>). The CSS below therefore styles those plain
// elements as they arrive: no handler has to know the template.

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

// The anime (二次元) variant: a starry cloud band, a kitten peeking over the
// card (decorative inline SVG) and rounded, candy-coloured details. Text and
// buttons keep the validated colours; motion runs only when allowed.
const frameAnimeCSS = `body{background:radial-gradient(circle,color-mix(in srgb,$ACCENT 13%,transparent) 0 1.8px,transparent 2.4px) 0 0/24px 24px,linear-gradient(180deg,$BG 30%,color-mix(in srgb,#e0f2fe 60%,$BG))}
.template-band{height:260px;background:radial-gradient(circle,#fff 0 1.5px,transparent 2.2px) 8px 10px/56px 48px,radial-gradient(circle,rgb(255 255 255/.7) 0 1px,transparent 1.8px) 30px 34px/72px 60px,linear-gradient(160deg,$ACCENT,color-mix(in srgb,$ACCENT 50%,#a78bfa) 55%,color-mix(in srgb,#7dd3fc 70%,$BG))}
.template-band::after{content:"";position:absolute;inset:auto 0 -1px 0;height:44px;background:radial-gradient(34px 26px at 50% 100%,$BG 97%,transparent) 0 100%/64px 44px repeat-x}
.template-anime{margin-top:104px;padding-bottom:calc($PADpx + 24px);border:4px solid #fff;border-radius:30px;box-shadow:0 28px 60px -34px color-mix(in srgb,$ACCENT 75%,transparent),0 3px 0 color-mix(in srgb,$ACCENT 14%,transparent)}
.template-anime::after{content:"";position:absolute;right:16px;bottom:12px;width:28px;height:26px;opacity:.16;pointer-events:none;background:radial-gradient(circle at 50% 70%,$ACCENT 0 7.5px,transparent 8px),radial-gradient(circle at 16% 38%,$ACCENT 0 3.4px,transparent 3.8px),radial-gradient(circle at 37% 13%,$ACCENT 0 3.4px,transparent 3.8px),radial-gradient(circle at 63% 13%,$ACCENT 0 3.4px,transparent 3.8px),radial-gradient(circle at 84% 38%,$ACCENT 0 3.4px,transparent 3.8px)}
.template-mascot{position:absolute;top:-85px;right:30px;width:112px;pointer-events:none}
.template-mascot svg{display:block;width:100%;height:auto;overflow:visible}
.pm-acc{fill:$ACCENT}
.pm-spark{fill:#ffd166}
.template-anime .template-head{border-bottom:2px dashed color-mix(in srgb,$ACCENT 22%,#fff)}
.template-anime .template-summary h1::before,.template-anime .template-summary h1::after{content:"";display:inline-block;width:7px;height:7px;margin:0 10px;border-radius:2px;background:$ACCENT;transform:rotate(45deg);vertical-align:2px}
.template-anime .template-amount strong{display:inline-block;padding:0 10px;background:linear-gradient(transparent 62%,color-mix(in srgb,$ACCENT 20%,transparent) 0)}
.template-anime .template-meta{border:2px dashed color-mix(in srgb,$ACCENT 24%,#fff);border-radius:18px;background:color-mix(in srgb,$ACCENT 5%,#fff)}
.template-anime .template-notice{border:2px dashed color-mix(in srgb,$ACCENT 30%,#fff);border-radius:16px}
.template-anime>img,.template-anime>svg{border:3px solid color-mix(in srgb,$ACCENT 28%,#fff);border-radius:24px;box-shadow:7px 7px 0 color-mix(in srgb,$ACCENT 16%,#fff)}
.template-anime>a,.template-anime button{border-radius:999px;background:linear-gradient(180deg,rgb(255 255 255/.2),rgb(255 255 255/0) 60%),$ACCENT;box-shadow:inset 0 -3px 0 rgb(0 0 0/.14),0 12px 22px -14px $ACCENT}
.template-anime fieldset label{border-width:2px;border-radius:18px}
.template-anime input:not([type=radio]):not([type=hidden]),.template-anime select{border-radius:16px}
@media(prefers-reduced-motion:no-preference){.pm-eyes{animation:pm-blink 4.6s infinite;transform-box:fill-box;transform-origin:center}.pm-spark{animation:pm-twinkle 2.8s ease-in-out infinite;transform-box:fill-box;transform-origin:center}.pm-spark+.pm-spark{animation-delay:-1.4s}}
@keyframes pm-blink{0%,93%,100%{transform:scaleY(1)}96%{transform:scaleY(.12)}}
@keyframes pm-twinkle{0%,100%{opacity:.4;transform:scale(.75)}50%{opacity:1;transform:scale(1.1)}}
@media(max-width:520px){.template-band{display:block;height:150px}.template-anime{min-height:calc(100vh - 88px);margin:88px 0 0;border-width:4px 0 0;border-radius:28px 28px 0 0;box-shadow:none}.template-mascot{top:-76px;right:20px;width:100px}}
`

// mascotSVG is the kitten of the anime pay template: it peeks over the top
// edge of the card, so the head is cut where the card begins and the paws
// rest on it. Decorative only.
const mascotSVG = `<span class="template-mascot" aria-hidden="true"><svg viewBox="0 0 140 118" focusable="false"><defs><clipPath id="pm-cut"><rect width="140" height="101"/></clipPath></defs><path class="pm-spark" d="M14 20l2.5 6 6 2.5-6 2.5-2.5 6-2.5-6-6-2.5 6-2.5z"/><path class="pm-spark" d="M128 8l2 4.5 4.5 2-4.5 2-2 4.5-2-4.5-4.5-2 4.5-2z"/><g clip-path="url(#pm-cut)" stroke="#5b3a4f" stroke-width="3" stroke-linejoin="round"><path d="M28 54L37 12 66 36Z" fill="#fff"/><path d="M112 54L103 12 74 36Z" fill="#fff"/><ellipse cx="70" cy="70" rx="48" ry="40" fill="#fff"/></g><path d="M38 20L41 34 52 30ZM102 20L99 34 88 30Z" fill="#ffb3cf"/><path class="pm-acc" d="M104 38l-12-8v16zM104 38l12-8v16z" stroke="#fff" stroke-width="1.5" stroke-linejoin="round"/><circle class="pm-acc" cx="104" cy="38" r="4.5" stroke="#fff" stroke-width="1.5"/><g class="pm-eyes"><ellipse cx="52" cy="70" rx="7" ry="9.5" fill="#3b2a4a"/><ellipse cx="88" cy="70" rx="7" ry="9.5" fill="#3b2a4a"/><circle cx="49.5" cy="66" r="3" fill="#fff"/><circle cx="85.5" cy="66" r="3" fill="#fff"/></g><ellipse cx="37" cy="83" rx="8" ry="4.5" fill="#ff9ec0" opacity=".7"/><ellipse cx="103" cy="83" rx="8" ry="4.5" fill="#ff9ec0" opacity=".7"/><path d="M67 80h6l-3 3z" fill="#f472b6"/><path d="M62 86q4 5 8 0q4 5 8 0M18 74l14 3M18 85l14-2M122 74l-14 3M122 85l-14-2" fill="none" stroke="#5b3a4f" stroke-width="2.4" stroke-linecap="round"/><g fill="#fff" stroke="#5b3a4f" stroke-width="3"><ellipse cx="44" cy="104" rx="15" ry="11"/><ellipse cx="96" cy="104" rx="15" ry="11"/></g><path d="M39 100v6M49 100v6M91 100v6M101 100v6" stroke="#5b3a4f" stroke-width="2" stroke-linecap="round"/></svg></span>`

const lockSVG = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/></svg>`

var opening = template.Must(template.New("page-open").Parse(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">{{if .Page.Keywords}}<meta name="keywords" content="{{.Page.Keywords}}">{{end}}{{if .Page.Description}}<meta name="description" content="{{.Page.Description}}">{{end}}<title>{{.Page.Title}}</title><style>{{.CSS}}</style><body data-template="{{.Name}}" data-template-type="{{.Type}}">{{if ne .Name "compact"}}<div class="template-band" aria-hidden="true"></div>{{end}}<main class="template-{{.Name}}">{{template "block" .}}`))

// The header, summary and notices: the top of <main> in built-in templates,
// and of the checkout block in uploaded ones.
var _ = template.Must(opening.New("block").Parse(`<header class="template-head"><span class="template-brand">{{if .LogoURL}}<img class="template-logo" alt="品牌图片" src="{{.LogoURL}}">{{end}}<span class="template-brand-name">{{.Config.BrandName}}</span></span><span class="template-secure">` + lockSVG + `安全支付</span></header>{{if eq .Type "qrpay"}}<section class="template-payee"><span class="template-avatar" aria-hidden="true">{{.Initial}}</span><h1>{{.Page.Title}}</h1>{{if .MerchantName}}<p class="template-payee-name">收款商户：{{.MerchantName}}</p>{{end}}</section>{{else}}<section class="template-summary">{{if eq .Name "anime"}}` + mascotSVG + `{{end}}<h1>{{.Page.Title}}</h1>{{if .Amount}}<p class="template-amount"><span>支付金额</span><strong><small>¥</small>{{.Amount}}</strong></p>{{end}}{{if or .TradeNo (and .Config.ShowOrderName .OrderName) .MerchantName}}<dl class="template-meta">{{if .TradeNo}}<dt>订单号</dt><dd>{{.TradeNo}}</dd>{{end}}{{if and .Config.ShowOrderName .OrderName}}<dt>商品名称</dt><dd>{{.OrderName}}</dd>{{end}}{{if .MerchantName}}<dt>收款商户</dt><dd>{{.MerchantName}}</dd>{{end}}</dl>{{end}}</section>{{end}}{{if .Preview}}<p class="template-preview-note">仅为模板预览，不发起支付。</p>{{end}}{{if .Config.Notice}}<p class="template-notice">{{.Config.Notice}}</p>{{end}}{{if .Sandbox}}<p class="template-sandbox">本地模拟环境，不涉及真实资金。</p>{{end}}`))

// initial is the first character of the payee shown in the qrpay avatar.
func initial(names ...string) string {
	for _, n := range names {
		for _, r := range strings.TrimSpace(n) {
			return string(unicode.ToUpper(r))
		}
	}
	return "G"
}

// Open writes a pay or qrpay page up to its content and returns the closing
// that the caller writes after the content.
func Open(w io.Writer, r Resolved, p Page) (string, error) {
	if p.BrandName != "" {
		r.Config.BrandName = p.BrandName
	}
	raw, _ := json.Marshal(r.Config.Public(r.Type))
	cfg, e := ParseConfig(r.Type, r.Name, raw)
	if e != nil {
		return "", e
	}
	r.Config = cfg
	f := frame{Resolved: r, Page: p, Width: 520, Padding: 28, TextColor: "#163047"}
	if p.ForegroundColor != "" {
		if !colorPattern.MatchString(p.ForegroundColor) {
			return "", ErrInvalid
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
			return "", ErrInvalid
		}
		f.LogoURL = "/static/img/logo.png"
	}
	f.Initial = initial(p.MerchantName, cfg.BrandName)
	if r.Upload != nil {
		if r.Type == "home" {
			return "", ErrInvalid
		}
		return openUpload(w, f)
	}
	css := frameBaseCSS
	switch r.Name {
	case "compact":
		css += frameCompactCSS
	case "anime":
		css += frameAnimeCSS
	}
	f.CSS = template.CSS(strings.NewReplacer(
		"$ONACCENT", readable(cfg.AccentColor, "#ffffff", "#0b1220"),
		"$ACCENT", cfg.AccentColor,
		"$BG", cfg.BackgroundColor,
		"$FG", f.TextColor,
		"$WIDTH", strconv.Itoa(f.Width),
		"$PAD", strconv.Itoa(f.Padding),
	).Replace(css))
	if e = opening.Execute(w, f); e != nil {
		return "", e
	}
	return "</main></body></html>", nil
}
