package pagetemplate

import (
	"html/template"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// Home landing templates. Unlike the pay/qrpay checkout frames (a single
// branded card rendered by Open), the home page is a full server-rendered
// landing page. The built-in styles keep the original console's template
// names (default, index1 ... index10), each rebuilt from that template's
// content structure, plus GOEPAY's own single-card style. All of them share
// one safe renderer: every piece of operator text (site name, notice,
// contacts) is escaped by html/template, no script runs, decoration is CSS and
// inline SVG only (img-src 'self'), and the page never contains a form
// (navigation links only). Colours stay driven by the selected template's
// typed accent and background, so the console colour controls keep working
// across every style; each style only brings its own default palette, and the
// text colours are chosen for contrast against the colours in force.

// HomeStyleInfo names a built-in home layout. CTA is the main button label
// shown while merchant self-registration is open.
type HomeStyleInfo struct{ Name, Title, Description, CTA string }

// The titles of default and index1-index10 are the original market titles, so
// 应用市场 and 模板配置 show the same name for the same template.
var homeStyleList = []HomeStyleInfo{
	{"default", "默认首页模板", "居中横幅 + 功能卡片 + 合作伙伴", "注册商户"},
	{"index1", "简约通用风首页模板", "左右分栏横幅 + 核心能力 + 服务指标", "注册使用"},
	{"index2", "清新扁平风首页模板", "扁平横幅 + 常见问题 + 支付渠道", "注册商户"},
	{"index3", "粒子特效首页模板", "粒子背景横幅 + 支付场景 + 支付渠道", "成为商户"},
	{"index4", "Bootstrap企业风首页模板", "企业官网结构：服务指标 + 平台优势 + 选择理由", "立即入驻"},
	{"index5", "蓝色科技风首页模板", "整幅色块横幅 + 服务指标 + 核心能力", "立即开启"},
	{"index6", "紫色科技风首页模板", "分栏横幅 + 卖点清单 + 接入流程", "申请接入"},
	{"index7", "深色科技风首页模板", "深色视觉 + 解决方案 + 行动号召", "马上加入"},
	{"index8", "经典商务风首页模板", "四项保障 + 收款场景 + 合作伙伴", "注册商户"},
	{"index9", "简洁大气风首页模板", "大标题横幅 + 接入流程 + 核心能力", "开始使用"},
	{"index10", "动效展示风首页模板", "渐变横幅 + 六步接入 + 进场动效", "立即注册"},
	{"minimal", "单卡极简首页模板", "GOEPAY 附加样式：一张卡片，只有品牌与说明", "注册商户"},
}

// homePalettes are each style's default accent and background, used until
// the operator saves their own colours for that style. Styles not listed use
// the shared GOEPAY default.
var homePalettes = map[string][2]string{
	"index2":  {"#047857", "#f3faf6"},
	"index3":  {"#2b6cb0", "#f4f7fb"},
	"index4":  {"#0b5ed7", "#f8f9fa"},
	"index5":  {"#1e4fd8", "#f3f6ff"},
	"index6":  {"#7c3aed", "#f7f4ff"},
	"index7":  {"#38bdf8", "#0b1220"},
	"index8":  {"#1f3a5f", "#f6f7f9"},
	"index9":  {"#006bb8", "#f7fbff"},
	"index10": {"#c2410c", "#fff8f3"},
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

// luminance is the WCAG relative luminance of a #rrggbb colour.
func luminance(hex string) float64 {
	v, e := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if e != nil || len(hex) != 7 {
		return 1
	}
	channel := func(c uint64) float64 {
		s := float64(c) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(v>>16&0xff) + 0.7152*channel(v>>8&0xff) + 0.0722*channel(v&0xff)
}

// contrast is the WCAG contrast ratio of two #rrggbb colours.
func contrast(a, b string) float64 {
	x, y := luminance(a), luminance(b)
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}

// readable returns whichever of the two candidates reads better on bg.
func readable(bg, light, dark string) string {
	if contrast(bg, light) >= contrast(bg, dark) {
		return light
	}
	return dark
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
	homeAdvantages = []homeFeat{
		{"chart", "数据直观", "后台数据详尽直观，轻松查看交易统计与各阶段占比"},
		{"code", "降低成本", "节省支付模块的开发与维护成本，解决技术难点"},
		{"shield", "安全稳定", "安全稳定的系统保障，专业的技术团队支持"},
		{"gateway", "聚合渠道", "一个操作平台，聚合多种交易渠道"},
	}
	homeAssurances = []homeFeat{
		{"chart", "全天候运行", "7×24 小时无人值守，高效运转"},
		{"settle", "即时回调", "付款后立即通知商户，流程顺畅"},
		{"wallet", "自动结算", "满额后系统自动结算到账"},
		{"shield", "账户安全", "结算账户受保护，防止他人修改"},
	}
	homeReasons = []string{
		"支付宝、微信、QQ钱包等主流支付渠道",
		"PC 网页、扫码、移动 H5 等多种支付方式",
		"开发文档与 SDK 齐全，快速完成对接",
		"满额自动结算，订单与资金实时可查",
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
		{"自动结算", "满额自动结算到账"},
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
	Year                                   int
	CTA, Density                           string
	CSS                                    template.CSS
	FeatureCards, FeatureGrid, FeatureList []homeFeat
	Scenarios, Advantages, Assurances      []homeFeat
	Reasons                                []string
	Steps, Steps6                          []homeStep
	Stats                                  []homeStat
	FAQ                                    []homeQA
	Channels, Partners                     []string
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
	"check":   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"/><path d="m8 12 3 3 5-6"/></svg>`,
}

func homeIcon(key string) template.HTML { return homeIcons[key] }

// $ACCENT, $BG, $FG and $ONACCENT are replaced with validated #rrggbb values
// at render time; $FG and $ONACCENT are picked for contrast.
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
.nav-links a.cta{background:$ACCENT;color:$ONACCENT;opacity:1}
.nav-links a.ghost{border:1px solid color-mix(in srgb,$ACCENT 55%,transparent);color:$ACCENT;opacity:1}
.btn{display:inline-block;padding:11px 24px;border-radius:10px;font-size:15px;font-weight:600}
.btn.primary{background:$ACCENT;color:$ONACCENT}
.btn.ghost{border:1px solid color-mix(in srgb,$ACCENT 55%,transparent);color:$ACCENT}
section{padding:56px 0}
h1{font-size:34px;margin:0 0 16px;line-height:1.25}
h2{font-size:24px;margin:0 0 6px;text-align:center}
.sub{opacity:.68;margin:0 0 32px;text-align:center}
.lead{font-size:18px;opacity:.76;margin:0 0 24px}
.cta-row{display:flex;gap:14px;flex-wrap:wrap}
.hero{text-align:center;padding:72px 0 40px}
.hero .cta-row{justify-content:center}
.split{display:grid;grid-template-columns:1.1fr .9fr;gap:40px;align-items:center}
.hero .split{text-align:left}.hero .split .cta-row{justify-content:flex-start}
.notice{max-width:1120px;margin:0 auto;padding:0 24px}
.notice p{margin:20px 0 0;padding:12px 16px;border-radius:10px;background:color-mix(in srgb,$ACCENT 10%,transparent);border:1px solid color-mix(in srgb,$ACCENT 28%,transparent);font-size:14px;white-space:pre-wrap}
.cards{display:grid;gap:20px}
.cols-2{grid-template-columns:repeat(2,1fr)}
.cols-3{grid-template-columns:repeat(3,1fr)}
.cols-4{grid-template-columns:repeat(4,1fr)}
.card{background:color-mix(in srgb,$FG 4%,$BG);border:1px solid color-mix(in srgb,$FG 10%,transparent);border-radius:14px;padding:22px}
.card .ic{width:40px;height:40px;color:$ACCENT;margin-bottom:12px}
.card h3{margin:0 0 6px;font-size:17px}
.card p{margin:0;opacity:.72;font-size:14px}
.scn{display:grid;gap:18px;grid-template-columns:repeat(3,1fr)}
.chips{display:flex;flex-wrap:wrap;gap:12px;justify-content:center}
.chip{display:inline-flex;align-items:center;padding:9px 18px;border-radius:999px;background:color-mix(in srgb,$FG 5%,$BG);border:1px solid color-mix(in srgb,$FG 12%,transparent);font-size:14px;font-weight:500}
.badges{display:flex;flex-wrap:wrap;gap:16px;justify-content:center}
.badge{flex:1 1 150px;max-width:220px;text-align:center;padding:20px;border-radius:14px;background:color-mix(in srgb,$ACCENT 8%,$BG);border:1px solid color-mix(in srgb,$ACCENT 18%,transparent)}
.badge b{display:block;font-size:22px;color:$ACCENT;margin-bottom:4px}
.badge span{font-size:13px;opacity:.72}
.steps{display:grid;gap:16px;grid-template-columns:repeat(4,1fr)}
.step{padding:18px;border-radius:12px;background:color-mix(in srgb,$FG 4%,$BG);border:1px solid color-mix(in srgb,$FG 10%,transparent)}
.step .n{width:30px;height:30px;border-radius:50%;background:$ACCENT;color:$ONACCENT;display:grid;place-content:center;font-weight:700;margin-bottom:10px}
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
@media(max-width:860px){.split{grid-template-columns:1fr!important}.cols-2,.cols-3{grid-template-columns:1fr}.cols-4,.scn,.steps{grid-template-columns:repeat(2,1fr)}h1{font-size:27px}section{padding:40px 0}.hero{padding:48px 0 32px}}
@media(max-width:560px){.home-nav{flex-wrap:wrap;gap:12px}.nav-links{margin-left:0;width:100%}.cols-4,.scn,.steps{grid-template-columns:1fr}}
`

// Per-style skins. Each targets .home-<name> to give the shared sections a
// distinct look while leaving the colour controls in charge of the palette.
var homeSkins = map[string]string{
	"default": `.home-default .hero{background:linear-gradient(180deg,color-mix(in srgb,$ACCENT 14%,$BG),$BG)}`,
	"index1": `.home-index1 .hero-art{aspect-ratio:4/3;border-radius:20px;background:linear-gradient(135deg,$ACCENT,color-mix(in srgb,$ACCENT 35%,$BG));box-shadow:0 20px 50px -24px color-mix(in srgb,$ACCENT 70%,transparent)}
.home-index1 .hero-art svg{width:100%;height:100%}
.home-index1 .hero-art .w{fill:#fff}.home-index1 .hero-art .a{fill:$ACCENT}.home-index1 .hero-art .m{fill:color-mix(in srgb,$ACCENT 22%,#fff)}
.home-index1 .card{background:$BG;box-shadow:0 10px 30px -22px color-mix(in srgb,$FG 60%,transparent)}`,
	"index2": `.home-index2 .hero{background:color-mix(in srgb,$ACCENT 10%,$BG)}
.home-index2 .qa,.home-index2 .chip{border:0;border-radius:6px;background:color-mix(in srgb,$ACCENT 7%,$BG)}
.home-index2 .qa{border-left:4px solid $ACCENT}
.home-index2 .btn,.home-index2 .nav-links a{border-radius:6px}`,
	"index3": `.home-index3 .hero{color:$ONACCENT;background-color:$ACCENT;background-image:radial-gradient(color-mix(in srgb,$ONACCENT 40%,transparent) 1.5px,transparent 2px),radial-gradient(color-mix(in srgb,$ONACCENT 22%,transparent) 1px,transparent 1.5px);background-size:48px 48px,22px 22px;background-position:0 0,11px 15px;padding:88px 0 60px}
.home-index3 .hero .lead{opacity:.9}
.home-index3 .hero .btn.primary{background:$ONACCENT;color:$ACCENT}.home-index3 .hero a.btn.ghost{border-color:$ONACCENT;color:$ONACCENT}
.home-index3 .scn .card{text-align:center}.home-index3 .scn .ic{margin:0 auto 12px}`,
	"index4": `.home-index4 .kpis{display:grid;grid-template-columns:repeat(2,1fr);gap:14px}
.home-index4 .kpi{padding:22px;border-radius:8px;background:$BG;border:1px solid color-mix(in srgb,$FG 12%,transparent);box-shadow:0 8px 24px -18px color-mix(in srgb,$FG 60%,transparent)}
.home-index4 .kpi b{display:block;font-size:24px;color:$ACCENT}.home-index4 .kpi span{font-size:13px;opacity:.72}
.home-index4 .hero{background:linear-gradient(180deg,color-mix(in srgb,$ACCENT 7%,$BG),$BG)}
.home-index4 .card,.home-index4 .btn,.home-index4 .nav-links a{border-radius:6px}
.home-index4 .why{background:color-mix(in srgb,$ACCENT 6%,$BG)}
.home-index4 .why h2{text-align:left;margin-bottom:12px}.home-index4 .why p{margin:0;opacity:.78}
.home-index4 .ticks{list-style:none;margin:0;padding:0;display:grid;gap:12px}
.home-index4 .ticks li{display:flex;gap:10px;align-items:center;padding:12px 16px;border-radius:6px;background:$BG;border:1px solid color-mix(in srgb,$FG 10%,transparent)}
.home-index4 .ticks .ic{flex:none;width:22px;height:22px;color:$ACCENT}`,
	"index5": `.home-index5 .hero{background:$ACCENT;color:$ONACCENT;border-radius:0 0 28px 28px;padding:84px 0 56px}
.home-index5 .hero .lead{opacity:.92}
.home-index5 .hero .btn.primary{background:$ONACCENT;color:$ACCENT}.home-index5 .hero a.btn.ghost{border-color:$ONACCENT;color:$ONACCENT}
.home-index5 .badge{background:color-mix(in srgb,$ACCENT 10%,$BG)}`,
	"index6": `.home-index6 .split{grid-template-columns:1fr 1fr}
.home-index6 .hero{background:linear-gradient(160deg,color-mix(in srgb,$ACCENT 12%,$BG),$BG 60%)}
.home-index6 .list{display:grid;gap:14px}
.home-index6 .li{display:flex;gap:12px;align-items:flex-start;padding:14px 16px;border-radius:12px;background:color-mix(in srgb,$FG 4%,$BG)}
.home-index6 .li .ic{flex:none;width:28px;height:28px;color:$ACCENT}
.home-index6 .li h3{margin:0 0 2px;font-size:15px}.home-index6 .li p{margin:0;font-size:13px;opacity:.72}`,
	"index7": `.home-index7 .hero{padding:96px 0 64px;background:radial-gradient(60% 80% at 85% 0,color-mix(in srgb,$ACCENT 26%,transparent),transparent),radial-gradient(50% 70% at 0 100%,color-mix(in srgb,$ACCENT 14%,transparent),transparent)}
.home-index7 .eyebrow{margin:0 0 12px;color:$ACCENT;font-size:13px;letter-spacing:.14em}
.home-index7 .card{background:color-mix(in srgb,$FG 5%,$BG);border-color:color-mix(in srgb,$ACCENT 24%,transparent)}
.home-index7 .badge{background:color-mix(in srgb,$ACCENT 10%,$BG);border-color:color-mix(in srgb,$ACCENT 30%,transparent)}
.home-index7 .band{text-align:center;background:linear-gradient(90deg,color-mix(in srgb,$ACCENT 16%,$BG),color-mix(in srgb,$ACCENT 6%,$BG))}
.home-index7 .band .cta-row{justify-content:center}`,
	"index8": `.home-index8 h1,.home-index8 h2{font-family:Georgia,"Songti SC","SimSun",serif}
.home-index8 .hero{background:linear-gradient(180deg,color-mix(in srgb,$ACCENT 9%,$BG),$BG);border-bottom:3px solid $ACCENT}
.home-index8 .card{border-radius:4px;border-top:3px solid $ACCENT}
.home-index8 .btn,.home-index8 .nav-links a{border-radius:4px}
.home-index8 .scn{grid-template-columns:repeat(2,1fr)}
.home-index8 .scn .card{display:flex;gap:14px;align-items:flex-start}.home-index8 .scn .ic{flex:none;margin:0}
@media(max-width:720px){.home-index8 .scn{grid-template-columns:1fr}}`,
	"index9": `.home-index9 .hero{padding:96px 0 48px}.home-index9 h1{font-size:40px}
.home-index9 .step{background:$BG;border:1px dashed color-mix(in srgb,$ACCENT 40%,transparent)}
@media(max-width:860px){.home-index9 h1{font-size:30px}}`,
	"index10": `.home-index10 .hero{background:linear-gradient(120deg,$ACCENT,color-mix(in srgb,$ACCENT 55%,$FG));color:$ONACCENT;padding:84px 0 56px}
.home-index10 .hero .btn.primary{background:$ONACCENT;color:$ACCENT}.home-index10 .hero a.btn.ghost{border-color:$ONACCENT;color:$ONACCENT}
.home-index10 .steps{grid-template-columns:repeat(3,1fr)}
@media(max-width:860px){.home-index10 .steps{grid-template-columns:repeat(2,1fr)}}
@media(max-width:560px){.home-index10 .steps{grid-template-columns:1fr}}
@media(prefers-reduced-motion:no-preference){.home-index10 .hero h1,.home-index10 .hero .lead,.home-index10 .hero .cta-row,.home-index10 .step,.home-index10 .card{animation:home-rise .6s ease-out both}.home-index10 .hero .lead{animation-delay:.1s}.home-index10 .hero .cta-row{animation-delay:.2s}.home-index10 .step:nth-child(2),.home-index10 .card:nth-child(2){animation-delay:.08s}.home-index10 .step:nth-child(3),.home-index10 .card:nth-child(3){animation-delay:.16s}.home-index10 .step:nth-child(4){animation-delay:.24s}.home-index10 .step:nth-child(5){animation-delay:.32s}.home-index10 .step:nth-child(6){animation-delay:.4s}}
@keyframes home-rise{from{opacity:0;transform:translateY(14px)}to{opacity:1;transform:none}}`,
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

{{define "layout-default"}}<section class="hero"><div class="wrap"><h1>欢迎使用{{.SiteName}}</h1><p class="lead">提供免签约支付宝、微信支付、QQ钱包等聚合收款服务</p>{{template "cta" .}}{{template "notice" .}}</div></section>{{template "cards3" .}}{{template "partners" .}}{{end}}

{{define "layout-index1"}}<section class="hero"><div class="wrap split"><div><h1>专业的聚合收款系统</h1><p class="lead">全面集成主流支付渠道，为企业提供收款、结算、对账一体化技术方案。</p>{{template "cta" .}}</div><div class="hero-art" aria-hidden="true"><svg viewBox="0 0 400 300"><rect class="w" x="40" y="36" width="320" height="228" rx="18"/><rect class="a" x="64" y="62" width="110" height="12" rx="6"/><rect class="m" x="64" y="86" width="170" height="8" rx="4"/><rect class="a" x="72" y="196" width="22" height="40" rx="4"/><rect class="a" x="106" y="168" width="22" height="68" rx="4"/><rect class="a" x="140" y="182" width="22" height="54" rx="4"/><rect class="a" x="174" y="148" width="22" height="88" rx="4"/><rect class="a" x="208" y="160" width="22" height="76" rx="4"/><rect class="a" x="252" y="120" width="84" height="84" rx="10"/><path class="w" d="M264 132h22v22h-22zM302 132h22v22h-22zM264 170h22v22h-22zM300 168h9v9h-9zM313 181h9v9h-9z"/></svg></div></div></section>{{template "notice" .}}{{template "grid4" .}}{{template "stats" .}}{{end}}

{{define "layout-index2"}}<section class="hero"><div class="wrap"><h1>帮助中心</h1><p class="lead">聚合主流通道，告别繁琐的接入流程</p>{{template "cta" .}}{{template "notice" .}}</div></section>{{template "faq" .}}{{template "channels" .}}{{end}}

{{define "layout-index3"}}<section class="hero"><div class="wrap"><h1>让生意简单好做</h1><p class="lead">提供支付接入方案，在各种业务场景中轻松收款</p>{{template "cta" .}}</div></section>{{template "notice" .}}<section><h2>支付场景</h2><p class="sub">按业务选择合适的收款方式</p><div class="wrap"><div class="scn">{{range .Scenarios}}<div class="card"><div class="ic">{{icon .Icon}}</div><h3>{{.Title}}</h3><p>{{.Desc}}</p></div>{{end}}</div></div></section>{{template "channels" .}}{{end}}

{{define "layout-index4"}}<section class="hero"><div class="wrap split"><div><h1>我们专注的每一面，都是为了给你更好的体验</h1><p class="lead">以信誉求市场，以稳定求发展。简单易用，专业的技术团队，放心的聚合收款平台。</p>{{template "cta" .}}</div><div class="kpis">{{range .Stats}}<div class="kpi"><b>{{.Value}}</b><span>{{.Label}}</span></div>{{end}}</div></div></section>{{template "notice" .}}<section><h2>平台优势</h2><p class="sub">一个操作平台，聚合多种交易渠道</p><div class="wrap"><div class="cards cols-4">{{range .Advantages}}<div class="card"><div class="ic">{{icon .Icon}}</div><h3>{{.Title}}</h3><p>{{.Desc}}</p></div>{{end}}</div></div></section><section class="why"><div class="wrap split"><div><h2>你凭什么选择我们？</h2><p>提供多种支付接入方式，方便、简单、快捷，快速集成到你的业务，效率高、见效快。</p></div><ul class="ticks">{{range .Reasons}}<li><span class="ic">{{icon "check"}}</span>{{.}}</li>{{end}}</ul></div></section>{{template "partners" .}}{{end}}

{{define "layout-index5"}}<section class="hero"><div class="wrap"><h1>让支付接入前所未有的简单</h1><p class="lead">一个 SDK 即可接入完整的支付系统，以更稳定的接口、更快的速度直达支付。</p>{{template "cta" .}}</div></section>{{template "notice" .}}{{template "stats" .}}{{template "grid4" .}}{{end}}

{{define "layout-index6"}}<section class="hero"><div class="wrap split"><div><h1>免签约收款平台</h1><p class="lead">免去签约烦恼，也能享受及时到账的乐趣。稳定、安全、值得信赖。</p>{{template "cta" .}}</div><div class="list">{{range .FeatureList}}<div class="li"><div class="ic">{{icon .Icon}}</div><div><h3>{{.Title}}</h3><p>{{.Desc}}</p></div></div>{{end}}</div></div></section>{{template "notice" .}}{{template "steps" .}}{{end}}

{{define "layout-index7"}}<section class="hero"><div class="wrap"><p class="eyebrow">极速响应 · 安全可靠 · 方便快捷</p><h1>我们拥有比同行更优质的服务</h1><p class="lead">轻松实现手机付款、在线付款，为你的业务提供多种支付解决方案。</p>{{template "cta" .}}{{template "notice" .}}</div></section><section><h2>多种解决方案</h2><p class="sub">按业务场景组合使用</p><div class="wrap"><div class="scn">{{range .Scenarios}}<div class="card"><div class="ic">{{icon .Icon}}</div><h3>{{.Title}}</h3><p>{{.Desc}}</p></div>{{end}}</div></div></section>{{template "stats" .}}<section class="band"><div class="wrap"><h2>那么，你下一步准备好了吗？</h2><p class="sub">赶紧加入我们吧</p>{{template "cta" .}}</div></section>{{end}}

{{define "layout-index8"}}<section class="hero"><div class="wrap"><h1>{{.SiteName}} · 为创业者而生</h1><p class="lead">专注于提供安全、高效、严谨、便捷的订单数据服务。</p>{{template "cta" .}}{{template "notice" .}}</div></section><section><div class="wrap"><div class="cards cols-4">{{range .Assurances}}<div class="card"><div class="ic">{{icon .Icon}}</div><h3>{{.Title}}</h3><p>{{.Desc}}</p></div>{{end}}</div></div></section><section><h2>收款场景</h2><p class="sub">适配不同业务的收款方式</p><div class="wrap"><div class="scn">{{range .Scenarios}}<div class="card"><div class="ic">{{icon .Icon}}</div><div><h3>{{.Title}}</h3><p>{{.Desc}}</p></div></div>{{end}}</div></div></section>{{template "partners" .}}{{end}}

{{define "layout-index9"}}<section class="hero"><div class="wrap"><h1>每个梦想，都值得灌溉</h1><p class="lead">正规、安全、稳定、可靠的支付接口服务，让创造价值的人体现价值。</p>{{template "cta" .}}{{template "notice" .}}</div></section>{{template "steps" .}}{{template "grid4" .}}{{end}}

{{define "layout-index10"}}<section class="hero"><div class="wrap"><h1>全新支付体验</h1><p class="lead">资金记录、订单记录、收益统计、渠道分析，全响应式界面，安全便捷。</p>{{template "cta" .}}</div></section>{{template "notice" .}}{{template "steps6" .}}{{template "cards3" .}}{{end}}

{{define "layout-minimal"}}<div class="mcard"><h1>{{.SiteName}}</h1><p class="lead">聚合支付技术服务</p>{{if .Notice}}<div class="notice"><p>{{.Notice}}</p></div>{{end}}{{template "cta" .}}</div>{{end}}

{{define "doc"}}<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">{{if .Keywords}}<meta name="keywords" content="{{.Keywords}}">{{end}}{{if .Description}}<meta name="description" content="{{.Description}}">{{end}}<title>{{.Title}}</title><style>{{.CSS}}</style></head><body data-home-style="{{.Style}}" data-density="{{.Density}}"><div class="home home-{{.Style}}">{{template "sandbox" .}}{{template "nav" .}}<main class="home-main">{{if eq .Style "index1"}}{{template "layout-index1" .}}{{else if eq .Style "index2"}}{{template "layout-index2" .}}{{else if eq .Style "index3"}}{{template "layout-index3" .}}{{else if eq .Style "index4"}}{{template "layout-index4" .}}{{else if eq .Style "index5"}}{{template "layout-index5" .}}{{else if eq .Style "index6"}}{{template "layout-index6" .}}{{else if eq .Style "index7"}}{{template "layout-index7" .}}{{else if eq .Style "index8"}}{{template "layout-index8" .}}{{else if eq .Style "index9"}}{{template "layout-index9" .}}{{else if eq .Style "index10"}}{{template "layout-index10" .}}{{else if eq .Style "minimal"}}{{template "layout-minimal" .}}{{else}}{{template "layout-default" .}}{{end}}</main>{{template "footer" .}}</div></body></html>{{end}}`

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
	palette := DefaultConfig("home", style.Name)
	accent, background := r.Config.AccentColor, r.Config.BackgroundColor
	if !colorPattern.MatchString(accent) {
		accent = palette.AccentColor
	}
	if !colorPattern.MatchString(background) {
		background = palette.BackgroundColor
	}
	foreground := readable(background, "#e6edf5", "#163047")
	onAccent := readable(accent, "#ffffff", "#0b1220")
	css := homeBaseCSS + homeSkins[style.Name]
	css = strings.NewReplacer("$ONACCENT", onAccent, "$ACCENT", accent, "$BG", background, "$FG", foreground).Replace(css)
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
		Advantages:   homeAdvantages,
		Assurances:   homeAssurances,
		Reasons:      homeReasons,
		Steps:        homeSteps,
		Steps6:       homeSteps6,
		Stats:        homeStats,
		FAQ:          homeFAQ,
		Channels:     homeChannels,
		Partners:     homePartners,
	}
	return homeTmpl.ExecuteTemplate(w, "doc", f)
}
