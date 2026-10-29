package pagetemplate

import (
	"html/template"
	"io"
	"strings"
	"time"
)

// Home landing templates. Unlike the pay/qrpay checkout frames (a single
// branded card rendered by Open), the home page is a full server-rendered
// landing page. Ten built-in styles share one safe renderer: every piece of
// operator text (site name, notice, contacts) is escaped by html/template, no
// script runs, decoration is CSS and inline SVG only (img-src 'self'), and the
// page never contains a form (navigation links only). The styles differ in
// layout and composition; colours stay driven by the selected template's
// typed accent/background/foreground so the console colour controls keep
// working across every style.

// HomeStyleInfo names a built-in home layout. CTA is the main button label
// shown while merchant self-registration is open.
type HomeStyleInfo struct{ Name, Title, Description, CTA string }

var homeStyleList = []HomeStyleInfo{
	{"default", "经典宣传版", "居中横幅 + 功能卡片 + 合作伙伴", "注册商户"},
	{"cloud", "云收银企业版", "左右分栏横幅 + 功能宫格 + 服务指标", "注册使用"},
	{"aggregate", "聚合场景版", "支付场景宫格 + 渠道标签", "成为商户"},
	{"fintech", "金融科技版", "整幅色块横幅 + 指标 + 能力矩阵", "立即开启"},
	{"signup", "免签约版", "卖点清单 + 接入流程", "申请接入"},
	{"faq", "帮助中心版", "横幅 + 常见问题", "注册商户"},
	{"startup", "极简创业版", "极简横幅 + 接入流程", "开始使用"},
	{"modern", "全新体验版", "六步接入 + 功能卡片", "立即注册"},
	{"assistant", "支付帮手版", "收款场景 + 渠道 + 合作伙伴", "注册商户"},
	{"minimal", "单卡极简版", "单卡极简，仅品牌与说明", "注册商户"},
}

func homeStyleNames() []string {
	out := make([]string, 0, len(homeStyleList))
	for _, s := range homeStyleList {
		out = append(out, s.Name)
	}
	return out
}
func homeStyle(name string) (HomeStyleInfo, bool) {
	for _, s := range homeStyleList {
		if s.Name == name {
			return s, true
		}
	}
	return HomeStyleInfo{}, false
}

// HomeTemplates returns the built-in home styles as catalogue entries.
func HomeTemplates() []Template {
	out := make([]Template, 0, len(homeStyleList))
	for _, s := range homeStyleList {
		out = append(out, Template{Type: "home", Name: s.Name, Title: s.Title, Author: "GOEPAY", Version: "1", Description: s.Description, PreviewURL: "/console/v1/admin/templates/home/" + s.Name + "/preview", HasConfig: true})
	}
	return out
}

type homeFeat struct{ Icon, Title, Desc string }
type homeStep struct{ Title, Desc string }
type homeStat struct{ Value, Label string }
type homeQA struct{ Q, A string }

var (
	homeFeatures = []homeFeat{
		{"wallet", "多种支付方式", "支付宝、微信、QQ钱包、银联等主流渠道一站接入"},
		{"rate", "对接费率超低", "按用户组费率结算，综合成本更低"},
		{"settle", "自动结算", "满额自动结算到账，无需手动提现"},
		{"shield", "安全稳定", "HTTPS 加密、请求签名校验、风控拦截"},
		{"code", "快速接入", "完善的开发文档与 SDK，几行代码完成对接"},
		{"chart", "实时监控", "订单与资金实时可查，异常及时预警"},
	}
	homeScenarios = []homeFeat{
		{"scan", "扫码支付", "主扫被扫，支付宝、微信双通道"},
		{"media", "公众号支付", "公众号、服务窗内唤起支付"},
		{"app", "APP 支付", "App 内集成多种支付方式"},
		{"h5", "H5 支付", "手机网页内快捷支付"},
		{"mini", "小程序支付", "微信小程序内微信支付"},
		{"gateway", "网关支付", "PC 网页收银台支付"},
	}
	homeSteps = []homeStep{
		{"注册商户", "在线自助注册账号"},
		{"配置通道", "在后台配置支付通道与费率"},
		{"对接接口", "按文档与 SDK 对接下单与回调"},
		{"开始收款", "收款实时入账，自动结算"},
	}
	homeSteps6 = []homeStep{
		{"注册商户", "在线自助注册"},
		{"绑定结算", "设置收款与结算账户"},
		{"配置通道", "选择并配置支付通道"},
		{"对接接口", "按 SDK 对接下单与回调"},
		{"自助下单", "会员支付一键直达"},
		{"自动结算", "满额自动结算到账"},
	}
	homeStats = []homeStat{
		{"7×24", "全天候运行与监控"},
		{"T+1", "满额自动结算到账"},
		{"多渠道", "支付宝 · 微信 · QQ · 银联"},
		{"签名校验", "请求签名与风控保护"},
	}
	homeFAQ = []homeQA{
		{"如何入驻成为商户？", "通过注册功能自助入驻，审核通过后即可接入。"},
		{"如何快速接入？", "开发文档提供对接手册与 SDK，几行代码即可完成。"},
		{"支持哪些支付方式？", "支付宝、微信、QQ钱包、银联等主流支付渠道。"},
		{"结算方式有哪些？", "满额自动结算到账，也可按规则手动结算。"},
		{"资金是否安全？", "全程请求签名校验与风控拦截，账目可追溯。"},
	}
	homeChannels = []string{"支付宝", "微信支付", "QQ钱包", "银联"}
	homePartners = []string{"支付宝", "微信支付", "QQ钱包", "银联"}
)

// HomeData is the input for a landing page. Sandbox shows the local
// simulation banner; Preview serves template images from the console path.
type HomeData struct {
	Style                                  string
	SiteName, Title, Keywords, Description string
	Org, Email, QQ, QQURL, QQGroup, WeWork string
	Notice, LogoURL                        string
	TestOpen, RegistrationOpen             bool
	Sandbox, Preview                       bool
}

type homeFrame struct {
	HomeData
	Year         int
	CTA, Density string
	CSS          template.CSS
	FeatureCards []homeFeat
	FeatureGrid  []homeFeat
	FeatureList  []homeFeat
	Scenarios    []homeFeat
	Steps        []homeStep
	Steps6       []homeStep
	Stats        []homeStat
	FAQ          []homeQA
	Channels     []string
	Partners     []string
}

var homeIcons = map[string]template.HTML{
	"wallet":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="3" y="6" width="18" height="13" rx="2"/><path d="M3 10h18M16 14h2"/></svg>`,
	"rate":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M7 17 17 7M8 9h.01M16 15h.01"/><circle cx="12" cy="12" r="9"/></svg>`,
	"settle":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M12 3v18M7 8l5-5 5 5M17 16l-5 5-5-5"/></svg>`,
	"shield":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M12 3 5 6v6c0 4 3 6.5 7 9 4-2.5 7-5 7-9V6z"/><path d="m9 12 2 2 4-4"/></svg>`,
	"code":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="m8 8-4 4 4 4M16 8l4 4-4 4M13 6l-2 12"/></svg>`,
	"chart":   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M4 20V4M4 20h16M8 16l4-5 3 3 4-6"/></svg>`,
	"scan":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M4 8V5a1 1 0 0 1 1-1h3M20 8V5a1 1 0 0 0-1-1h-3M4 16v3a1 1 0 0 0 1 1h3M20 16v3a1 1 0 0 1-1 1h-3M3 12h18"/></svg>`,
	"media":   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 12a4 4 0 0 1 8 0M12 12v4"/></svg>`,
	"app":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="7" y="3" width="10" height="18" rx="2"/><path d="M11 18h2"/></svg>`,
	"h5":      `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="4" y="4" width="16" height="16" rx="2"/><path d="M8 9v6M8 12h3M11 9v6M14 9h3M15.5 9v6"/></svg>`,
	"mini":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="9"/><path d="M8 13c1.5 2 6.5 2 8 0"/></svg>`,
	"gateway": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="3" y="5" width="18" height="12" rx="2"/><path d="M3 20h18M9 17v3M15 17v3"/></svg>`,
}

func homeIcon(key string) template.HTML { return homeIcons[key] }

const homeBaseCSS = `*{box-sizing:border-box}
body{margin:0;font:16px/1.65 system-ui,-apple-system,"Segoe UI","Microsoft YaHei",sans-serif;background:$BG;color:$FG;overflow-wrap:anywhere}
a{color:$ACCENT;text-decoration:none}
img{max-width:100%;height:auto}svg{display:block}
.home{min-height:100vh;display:flex;flex-direction:column}
.home-main{flex:1}
.wrap{max-width:1120px;margin:0 auto;padding:0 24px;width:100%}
.home-nav{display:flex;align-items:center;gap:24px;max-width:1120px;margin:0 auto;padding:16px 24px;width:100%}
.home-nav .brand{display:flex;align-items:center;gap:10px;font-weight:700;font-size:18px;color:$FG}
.home-nav .brand img{height:34px;width:auto;max-width:160px;object-fit:contain}
.nav-links{margin-left:auto;display:flex;align-items:center;gap:8px;flex-wrap:wrap}
.nav-links a{padding:8px 12px;border-radius:8px;font-size:14px;color:$FG;opacity:.82}
.nav-links a:hover{opacity:1;background:color-mix(in srgb,$ACCENT 12%,transparent)}
.nav-links a.cta{background:$ACCENT;color:#fff;opacity:1}
.nav-links a.ghost{border:1px solid color-mix(in srgb,$ACCENT 55%,transparent);color:$ACCENT;opacity:1}
.btn{display:inline-block;padding:11px 24px;border-radius:10px;font-size:15px;font-weight:600}
.btn.primary{background:$ACCENT;color:#fff}
.btn.ghost{border:1px solid color-mix(in srgb,$ACCENT 55%,transparent);color:$ACCENT}
section{padding:56px 0}
h1{font-size:34px;margin:0 0 16px;line-height:1.25}
h2{font-size:24px;margin:0 0 6px;text-align:center}
.sub{opacity:.68;margin:0 0 32px;text-align:center}
.lead{font-size:18px;opacity:.76;margin:0 0 24px}
.cta-row{display:flex;gap:14px;flex-wrap:wrap}
.hero{text-align:center;padding:72px 0 40px}
.hero .cta-row{justify-content:center}
.notice{max-width:1120px;margin:0 auto;padding:0 24px}
.notice p{margin:20px 0 0;padding:12px 16px;border-radius:10px;background:color-mix(in srgb,$ACCENT 10%,transparent);border:1px solid color-mix(in srgb,$ACCENT 28%,transparent);font-size:14px;white-space:pre-wrap}
.cards{display:grid;gap:20px}
.cols-2{grid-template-columns:repeat(2,1fr)}
.cols-3{grid-template-columns:repeat(3,1fr)}
.card{background:color-mix(in srgb,$FG 4%,$BG);border:1px solid color-mix(in srgb,$FG 10%,transparent);border-radius:14px;padding:22px}
.card .ic{width:40px;height:40px;color:$ACCENT;margin-bottom:12px}
.card h3{margin:0 0 6px;font-size:17px}
.card p{margin:0;opacity:.72;font-size:14px}
.chips{display:flex;flex-wrap:wrap;gap:12px;justify-content:center}
.chip{display:inline-flex;align-items:center;padding:9px 18px;border-radius:999px;background:color-mix(in srgb,$FG 5%,$BG);border:1px solid color-mix(in srgb,$FG 12%,transparent);font-size:14px;font-weight:500}
.badges{display:flex;flex-wrap:wrap;gap:16px;justify-content:center}
.badge{flex:1 1 150px;max-width:220px;text-align:center;padding:20px;border-radius:14px;background:color-mix(in srgb,$ACCENT 8%,$BG);border:1px solid color-mix(in srgb,$ACCENT 18%,transparent)}
.badge b{display:block;font-size:22px;color:$ACCENT;margin-bottom:4px}
.badge span{font-size:13px;opacity:.72}
.steps{display:grid;gap:16px;grid-template-columns:repeat(4,1fr)}
.step{padding:18px;border-radius:12px;background:color-mix(in srgb,$FG 4%,$BG);border:1px solid color-mix(in srgb,$FG 10%,transparent)}
.step .n{width:30px;height:30px;border-radius:50%;background:$ACCENT;color:#fff;display:grid;place-content:center;font-weight:700;margin-bottom:10px}
.step h3{margin:0 0 4px;font-size:16px}
.step p{margin:0;font-size:13px;opacity:.72}
.faq{display:grid;gap:12px;max-width:820px;margin:0 auto}
.qa{padding:16px 18px;border-radius:12px;background:color-mix(in srgb,$FG 4%,$BG);border:1px solid color-mix(in srgb,$FG 10%,transparent)}
.qa b{display:block;margin-bottom:4px}.qa b::before{content:"Q ";color:$ACCENT}
.qa p{margin:0;opacity:.75;font-size:14px}
.home-footer{margin-top:24px;background:color-mix(in srgb,$FG 7%,$BG);border-top:1px solid color-mix(in srgb,$FG 12%,transparent);padding:28px 0;font-size:13px}
.home-footer .wrap{display:flex;flex-wrap:wrap;gap:12px 48px}
.home-footer .col h4{margin:0 0 8px;font-size:14px}
.home-footer .col a{display:block;opacity:.8;padding:2px 0}
.home-footer .col p{margin:2px 0;opacity:.8}
.home-footer .copy{width:100%;opacity:.6;border-top:1px solid color-mix(in srgb,$FG 10%,transparent);margin-top:12px;padding-top:12px}
.sandbox-bar{margin:0;padding:8px 16px;text-align:center;font-size:13px;background:#fff4e5;color:#7a4b00;border-bottom:1px solid #f3c27a}
body[data-density=compact] section{padding:36px 0}
body[data-density=compact] .hero{padding-top:48px;padding-bottom:28px}
body[data-density=compact] .card{padding:16px}
@media(max-width:860px){.split{grid-template-columns:1fr!important}.cols-2,.cols-3{grid-template-columns:1fr}.steps{grid-template-columns:repeat(2,1fr)}h1{font-size:27px}section{padding:40px 0}.hero{padding:48px 0 32px}}
@media(max-width:560px){.home-nav{flex-wrap:wrap;gap:12px}.nav-links{margin-left:0;width:100%}.steps{grid-template-columns:1fr}}
`

// Per-style skins. Each targets .home-<name> to give the shared sections a
// distinct look while leaving the colour controls in charge of the palette.
var homeSkins = map[string]string{
	"default": `.home-default .hero{background:linear-gradient(180deg,color-mix(in srgb,$ACCENT 14%,$BG),$BG)}`,
	"cloud": `.home-cloud .hero{text-align:left}
.home-cloud .split{display:grid;grid-template-columns:1.1fr .9fr;gap:40px;align-items:center}
.home-cloud .hero .cta-row{justify-content:flex-start}
.home-cloud .hero-art{aspect-ratio:4/3;border-radius:20px;background:linear-gradient(135deg,$ACCENT,color-mix(in srgb,$ACCENT 35%,$BG));box-shadow:0 20px 50px -24px color-mix(in srgb,$ACCENT 70%,transparent)}
.home-cloud .hero-art svg{width:100%;height:100%}
.home-cloud .hero-art .w{fill:#fff}.home-cloud .hero-art .a{fill:$ACCENT}.home-cloud .hero-art .m{fill:color-mix(in srgb,$ACCENT 22%,#fff)}
.home-cloud .card{background:$BG;box-shadow:0 10px 30px -22px color-mix(in srgb,$FG 60%,transparent)}`,
	"aggregate": `.home-aggregate .scn{display:grid;gap:18px;grid-template-columns:repeat(3,1fr)}
.home-aggregate .scn .card{text-align:center}.home-aggregate .scn .ic{margin:0 auto 12px}
@media(max-width:860px){.home-aggregate .scn{grid-template-columns:repeat(2,1fr)}}@media(max-width:560px){.home-aggregate .scn{grid-template-columns:1fr}}`,
	"fintech": `.home-fintech .hero{background:$ACCENT;color:#fff;border-radius:0 0 28px 28px;padding:84px 0 56px}
.home-fintech .hero a.btn.ghost{border-color:#fff;color:#fff}.home-fintech .hero .btn.primary{background:#fff;color:$ACCENT}
.home-fintech .hero .lead{opacity:.92}
.home-fintech .badge{background:color-mix(in srgb,$ACCENT 10%,$BG)}`,
	"signup": `.home-signup .hero{text-align:left}
.home-signup .split{display:grid;grid-template-columns:1fr 1fr;gap:40px;align-items:center}
.home-signup .hero .cta-row{justify-content:flex-start}
.home-signup .list{display:grid;gap:14px}
.home-signup .li{display:flex;gap:12px;align-items:flex-start;padding:14px 16px;border-radius:12px;background:color-mix(in srgb,$FG 4%,$BG)}
.home-signup .li .ic{flex:none;width:28px;height:28px;color:$ACCENT}
.home-signup .li h3{margin:0 0 2px;font-size:15px}.home-signup .li p{margin:0;font-size:13px;opacity:.72}`,
	"faq": `.home-faq .hero{background:radial-gradient(60% 120% at 50% 0,color-mix(in srgb,$ACCENT 16%,$BG),$BG)}`,
	"startup": `.home-startup .hero{padding:96px 0 48px}.home-startup h1{font-size:40px}
.home-startup .steps{grid-template-columns:repeat(4,1fr);counter-reset:n}
.home-startup .step{background:$BG;border:1px dashed color-mix(in srgb,$ACCENT 40%,transparent)}`,
	"modern": `.home-modern .hero{background:linear-gradient(120deg,$ACCENT,color-mix(in srgb,$ACCENT 55%,$FG));color:#fff;padding:84px 0 56px}
.home-modern .hero .btn.primary{background:#fff;color:$ACCENT}.home-modern .hero a.btn.ghost{border-color:#fff;color:#fff}
.home-modern .steps{grid-template-columns:repeat(3,1fr)}
@media(max-width:860px){.home-modern .steps{grid-template-columns:repeat(2,1fr)}}`,
	"assistant": `.home-assistant .scn{display:grid;gap:18px;grid-template-columns:repeat(2,1fr)}
.home-assistant .scn .card{display:flex;gap:14px;align-items:flex-start}.home-assistant .scn .ic{flex:none;margin:0}
@media(max-width:720px){.home-assistant .scn{grid-template-columns:1fr}}`,
	"minimal": `.home-minimal .home-main{display:grid;place-items:center}
.home-minimal .mcard{max-width:520px;margin:8vh auto;padding:40px;border-radius:20px;background:color-mix(in srgb,$FG 4%,$BG);border:1px solid color-mix(in srgb,$FG 10%,transparent);text-align:center}
.home-minimal .mcard .cta-row{justify-content:center;margin-top:24px}`,
}

const homeTemplateText = `{{define "navlinks"}}<a href="/">首页</a>{{if .TestOpen}}<a href="/test">支付测试</a>{{end}}{{if .RegistrationOpen}}<a class="ghost" href="/merchant/login">商户登录</a><a class="cta" href="/merchant/login">注册商户</a>{{else}}<a class="cta" href="/merchant/login">商户登录</a>{{end}}{{end}}
{{define "nav"}}<nav class="home-nav"><span class="brand">{{if .LogoURL}}<img src="{{.LogoURL}}" alt="">{{end}}{{.SiteName}}</span><span class="nav-links">{{template "navlinks" .}}</span></nav>{{end}}
{{define "notice"}}{{if .Notice}}<div class="notice"><p>{{.Notice}}</p></div>{{end}}{{end}}
{{define "cta"}}<div class="cta-row">{{if .RegistrationOpen}}<a class="btn primary" href="/merchant/login">{{.CTA}}</a><a class="btn ghost" href="/merchant/login">商户登录</a>{{else}}<a class="btn primary" href="/merchant/login">商户登录</a>{{end}}{{if .TestOpen}}<a class="btn ghost" href="/test">支付测试</a>{{end}}</div>{{end}}
{{define "sandbox"}}{{if .Sandbox}}<p class="sandbox-bar">本地模拟环境，不涉及真实资金。</p>{{end}}{{end}}
{{define "channels"}}<section><h2>支持的支付渠道</h2><p class="sub">聚合主流通道，覆盖常见收款场景</p><div class="wrap"><div class="chips">{{range .Channels}}<span class="chip">{{.}}</span>{{end}}</div></div></section>{{end}}
{{define "partners"}}<section><h2>平台合作伙伴</h2><div class="wrap"><div class="chips">{{range .Partners}}<span class="chip">{{.}}</span>{{end}}</div></div></section>{{end}}
{{define "cards3"}}<section><h2>为什么选择{{.SiteName}}</h2><p class="sub">稳定、安全、易接入的收款技术服务</p><div class="wrap"><div class="cards cols-3">{{range .FeatureCards}}<div class="card"><div class="ic">{{icon .Icon}}</div><h3>{{.Title}}</h3><p>{{.Desc}}</p></div>{{end}}</div></div></section>{{end}}
{{define "grid4"}}<section><h2>核心能力</h2><p class="sub">一站式支付技术服务</p><div class="wrap"><div class="cards cols-2">{{range .FeatureGrid}}<div class="card"><div class="ic">{{icon .Icon}}</div><h3>{{.Title}}</h3><p>{{.Desc}}</p></div>{{end}}</div></div></section>{{end}}
{{define "steps"}}<section><h2>接入流程</h2><p class="sub">自助开户，快速上线</p><div class="wrap"><div class="steps">{{range $i,$s := .Steps}}<div class="step"><div class="n">{{inc $i}}</div><h3>{{$s.Title}}</h3><p>{{$s.Desc}}</p></div>{{end}}</div></div></section>{{end}}
{{define "steps6"}}<section><h2>六步开始收款</h2><p class="sub">{{.SiteName}}让你轻松做生意</p><div class="wrap"><div class="steps">{{range $i,$s := .Steps6}}<div class="step"><div class="n">{{inc $i}}</div><h3>{{$s.Title}}</h3><p>{{$s.Desc}}</p></div>{{end}}</div></div></section>{{end}}
{{define "stats"}}<section><div class="wrap"><div class="badges">{{range .Stats}}<div class="badge"><b>{{.Value}}</b><span>{{.Label}}</span></div>{{end}}</div></div></section>{{end}}
{{define "faq"}}<section><h2>常见问题</h2><p class="sub">关于接入与结算</p><div class="wrap"><div class="faq">{{range .FAQ}}<div class="qa"><b>{{.Q}}</b><p>{{.A}}</p></div>{{end}}</div></div></section>{{end}}
{{define "footer"}}<footer class="home-footer"><div class="wrap"><div class="col"><h4>商户</h4><a href="/merchant/login">商户登录</a>{{if .RegistrationOpen}}<a href="/merchant/login">注册商户</a>{{end}}{{if .TestOpen}}<a href="/test">支付测试</a>{{end}}</div>{{if .Org}}<div class="col"><h4>关于我们</h4><p>{{.Org}}</p></div>{{end}}{{if or .Email .QQ .QQURL .WeWork .QQGroup}}<div class="col"><h4>联系我们</h4>{{if .Email}}<a href="mailto:{{.Email}}">{{.Email}}</a>{{end}}{{if .QQURL}}<a href="{{.QQURL}}" target="_blank" rel="noopener noreferrer">客服QQ{{if .QQ}}：{{.QQ}}{{end}}</a>{{else if .QQ}}<p>客服QQ：{{.QQ}}</p>{{end}}{{if .WeWork}}<a href="{{.WeWork}}" target="_blank" rel="noopener noreferrer">企业微信</a>{{end}}{{if .QQGroup}}<a href="{{.QQGroup}}" target="_blank" rel="noopener noreferrer">加入QQ群</a>{{end}}</div>{{end}}<p class="copy">付款请使用商户提供的付款链接，并核对订单金额和系统确认状态。<br>{{.SiteName}} © {{.Year}} All Rights Reserved.</p></div></footer>{{end}}

{{define "hero-default"}}<section class="hero"><div class="wrap"><h1>欢迎使用{{.SiteName}}</h1><p class="lead">提供免签约支付宝、微信支付、QQ钱包等聚合收款服务</p>{{template "cta" .}}{{template "notice" .}}</div></section>{{end}}
{{define "layout-default"}}{{template "hero-default" .}}{{template "cards3" .}}{{template "partners" .}}{{end}}

{{define "layout-cloud"}}<section class="hero"><div class="wrap split"><div><h1>专业的聚合收款系统</h1><p class="lead">全面集成主流支付渠道，为企业提供收款、结算、对账一体化技术方案。</p>{{template "cta" .}}</div><div class="hero-art" aria-hidden="true"><svg viewBox="0 0 400 300"><rect class="w" x="40" y="36" width="320" height="228" rx="18"/><rect class="a" x="64" y="62" width="110" height="12" rx="6"/><rect class="m" x="64" y="86" width="170" height="8" rx="4"/><rect class="a" x="72" y="196" width="22" height="40" rx="4"/><rect class="a" x="106" y="168" width="22" height="68" rx="4"/><rect class="a" x="140" y="182" width="22" height="54" rx="4"/><rect class="a" x="174" y="148" width="22" height="88" rx="4"/><rect class="a" x="208" y="160" width="22" height="76" rx="4"/><rect class="a" x="252" y="120" width="84" height="84" rx="10"/><path class="w" d="M264 132h22v22h-22zM302 132h22v22h-22zM264 170h22v22h-22zM300 168h9v9h-9zM313 181h9v9h-9z"/></svg></div></div></section>{{template "notice" .}}{{template "grid4" .}}{{template "stats" .}}{{end}}

{{define "layout-aggregate"}}<section class="hero"><div class="wrap"><h1>聚合支付，全场景覆盖</h1><p class="lead">在各种业务场景中轻松收款</p>{{template "cta" .}}{{template "notice" .}}</div></section><section><h2>支付场景</h2><p class="sub">按业务选择合适的收款方式</p><div class="wrap"><div class="scn">{{range .Scenarios}}<div class="card"><div class="ic">{{icon .Icon}}</div><h3>{{.Title}}</h3><p>{{.Desc}}</p></div>{{end}}</div></div></section>{{template "channels" .}}{{end}}

{{define "layout-fintech"}}<section class="hero"><div class="wrap"><h1>让支付接入前所未有的简单</h1><p class="lead">更稳定的接口、更快的速度，直达支付。</p>{{template "cta" .}}</div></section>{{template "notice" .}}{{template "stats" .}}{{template "grid4" .}}{{end}}

{{define "layout-signup"}}<section class="hero"><div class="wrap split"><div><h1>免签约收款平台</h1><p class="lead">免去签约烦恼，也能享受实时到账的乐趣。稳定、安全、值得信赖。</p>{{template "cta" .}}</div><div class="list">{{range .FeatureList}}<div class="li"><div class="ic">{{icon .Icon}}</div><div><h3>{{.Title}}</h3><p>{{.Desc}}</p></div></div>{{end}}</div></div></section>{{template "notice" .}}{{template "steps" .}}{{end}}

{{define "layout-faq"}}<section class="hero"><div class="wrap"><h1>帮助中心</h1><p class="lead">聚合主流通道，告别繁琐的接入流程</p>{{template "cta" .}}{{template "notice" .}}</div></section>{{template "faq" .}}{{template "channels" .}}{{end}}

{{define "layout-startup"}}<section class="hero"><div class="wrap"><h1>每个梦想，都值得灌溉</h1><p class="lead">正规、安全、稳定、可靠的支付接口服务，让创造价值的人体现价值。</p>{{template "cta" .}}{{template "notice" .}}</div></section>{{template "steps" .}}{{template "grid4" .}}{{end}}

{{define "layout-modern"}}<section class="hero"><div class="wrap"><h1>全新支付体验</h1><p class="lead">资金记录、订单记录、收益统计、渠道分析，全响应式界面，安全便捷。</p>{{template "cta" .}}</div></section>{{template "notice" .}}{{template "steps6" .}}{{template "cards3" .}}{{end}}

{{define "layout-assistant"}}<section class="hero"><div class="wrap"><h1>最专业的支付帮手</h1><p class="lead">覆盖线上线下收款场景，让支付简单、专业、快捷。</p>{{template "cta" .}}{{template "notice" .}}</div></section><section><h2>收款场景</h2><p class="sub">适配不同业务的收款方式</p><div class="wrap"><div class="scn">{{range .Scenarios}}<div class="card"><div class="ic">{{icon .Icon}}</div><div><h3>{{.Title}}</h3><p>{{.Desc}}</p></div></div>{{end}}</div></div></section>{{template "partners" .}}{{end}}

{{define "layout-minimal"}}<div class="mcard"><h1>{{.SiteName}}</h1><p class="lead">聚合支付技术服务</p>{{if .Notice}}<div class="notice"><p>{{.Notice}}</p></div>{{end}}{{template "cta" .}}</div>{{end}}

{{define "doc"}}<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">{{if .Keywords}}<meta name="keywords" content="{{.Keywords}}">{{end}}{{if .Description}}<meta name="description" content="{{.Description}}">{{end}}<title>{{.Title}}</title><style>{{.CSS}}</style></head><body data-home-style="{{.Style}}" data-density="{{.Density}}"><div class="home home-{{.Style}}">{{template "sandbox" .}}{{template "nav" .}}<main class="home-main">{{if eq .Style "cloud"}}{{template "layout-cloud" .}}{{else if eq .Style "aggregate"}}{{template "layout-aggregate" .}}{{else if eq .Style "fintech"}}{{template "layout-fintech" .}}{{else if eq .Style "signup"}}{{template "layout-signup" .}}{{else if eq .Style "faq"}}{{template "layout-faq" .}}{{else if eq .Style "startup"}}{{template "layout-startup" .}}{{else if eq .Style "modern"}}{{template "layout-modern" .}}{{else if eq .Style "assistant"}}{{template "layout-assistant" .}}{{else if eq .Style "minimal"}}{{template "layout-minimal" .}}{{else}}{{template "layout-default" .}}{{end}}</main>{{template "footer" .}}</div></body></html>{{end}}`

var homeTmpl = template.Must(template.New("home").Funcs(template.FuncMap{
	"icon": homeIcon,
	"inc":  func(i int) int { return i + 1 },
}).Parse(homeTemplateText))

// RenderHome writes a complete landing page for the resolved home template.
func RenderHome(w io.Writer, r Resolved, d HomeData) error {
	if r.Type != "home" {
		return ErrInvalid
	}
	style, ok := homeStyle(r.Name)
	if !ok {
		style, _ = homeStyle("default")
	}
	d.Style = style.Name
	if strings.TrimSpace(d.SiteName) == "" {
		d.SiteName = "GOEPAY"
	}
	if strings.TrimSpace(d.Title) == "" {
		d.Title = d.SiteName
	}
	accent, background, foreground := r.Config.AccentColor, r.Config.BackgroundColor, "#163047"
	if !colorPattern.MatchString(accent) {
		accent = "#007c91"
	}
	if !colorPattern.MatchString(background) {
		background = "#f4f8fa"
	}
	css := homeBaseCSS + homeSkins[style.Name]
	css = strings.NewReplacer("$ACCENT", accent, "$BG", background, "$FG", foreground).Replace(css)
	if d.Notice == "" {
		d.Notice = r.Config.Notice
	}
	// The site logo (set by the caller) wins over the template's own image,
	// as on the earlier home card.
	if d.LogoURL == "" && assetPattern.MatchString(r.Config.LogoAssetID) {
		if d.Preview {
			d.LogoURL = "/console/v1/admin/templates/images/" + r.Config.LogoAssetID
		} else {
			d.LogoURL = "/template-assets/" + r.Config.LogoAssetID
		}
	}
	density := "comfortable"
	if r.Config.Density == "compact" {
		density = "compact"
	}
	f := homeFrame{
		HomeData:     d,
		Year:         time.Now().Year(),
		CTA:          style.CTA,
		Density:      density,
		CSS:          template.CSS(css),
		FeatureCards: homeFeatures[:3],
		FeatureGrid:  homeFeatures[:4],
		FeatureList:  homeFeatures,
		Scenarios:    homeScenarios,
		Steps:        homeSteps,
		Steps6:       homeSteps6,
		Stats:        homeStats,
		FAQ:          homeFAQ,
		Channels:     homeChannels,
		Partners:     homePartners,
	}
	return homeTmpl.ExecuteTemplate(w, "doc", f)
}
