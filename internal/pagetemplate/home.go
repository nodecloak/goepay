package pagetemplate

import (
	_ "embed"
	"html/template"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// Home styles share safe, server-rendered content while retaining their own
// layout and palette. Only typed colours enter CSS; all operator text passes
// through html/template. No homepage executes scripts or submits payments.
type HomeStyleInfo struct{ Name, Title, Description, CTA string }

var homeStyleList = []HomeStyleInfo{
	{"default", "默认首页模板", "品牌主张 + 支付流程示意 + 完整接入指引", "注册商户"},
	{"index1", "简约通用风首页模板", "分栏工作台 + 订单流转 + 开发者资源", "注册使用"},
	{"index2", "清新扁平风首页模板", "清新服务概览 + 接入步骤 + 原生问答", "注册商户"},
	{"index3", "粒子特效首页模板", "点阵横幅 + 场景说明 + 安全接入路径", "成为商户"},
	{"index4", "Bootstrap企业风首页模板", "企业服务结构 + 能力清单 + 业务接入指南", "立即入驻"},
	{"index5", "蓝色科技风首页模板", "蓝色整幅主视觉 + 支付链路 + 接口资源", "立即开启"},
	{"index6", "紫色科技风首页模板", "紫色分栏 + 接口示意 + 完整开发指引", "申请接入"},
	{"index7", "深色科技风首页模板", "深色工作台 + 结果核对 + 场景与文档", "马上加入"},
	{"index8", "经典商务风首页模板", "经典商务排版 + 业务路径 + 服务说明", "注册商户"},
	{"index9", "简洁大气风首页模板", "大字主张 + 清晰步骤 + 文档阅读路径", "开始使用"},
	{"index10", "动效展示风首页模板", "轻动态主视觉 + 完整接入流程 + 开发资源", "立即注册"},
	{"minimal", "单卡极简首页模板", "紧凑编辑式布局，保留能力、指引、文档与问答", "注册商户"},
	{"sakura", "樱花物语二次元首页模板", "樱花看板娘主视觉 + 飘落花瓣 + 完整接入指引", "注册商户"},
	{"starry", "星空魔法二次元首页模板", "星空魔法兔主视觉 + 闪烁星光 + 开发者资源", "立即注册"},
	{"manga", "热血漫画二次元首页模板", "三格漫画讲解接入流程 + 网点纸背景 + 原生问答", "马上加入"},
	{"mecha", "机甲未来二次元首页模板", "机甲 HUD 主视觉 + 全息网格 + 支付链路说明", "启动接入"},
	{"candy", "糖果萌系二次元首页模板", "糖果小熊主视觉 + 波点背景 + 清晰接入步骤", "注册使用"},
	// custom shows the super administrator's own HTML and CSS (custom.go).
	{"custom", "自定义 HTML 首页模板", "粘贴自己的 HTML 和 CSS；不运行脚本，图片需先在这里上传", "注册商户"},
}

// Anime (二次元) styles draw an original inline-SVG mascot in the hero and
// add home/anime.css; the page content stays the same as in other styles.
var homeAnimeStyles = map[string]bool{"sakura": true, "starry": true, "manga": true, "mecha": true, "candy": true}

// Existing style palettes and saved operator colours remain authoritative.
var homePalettes = map[string][2]string{
	"index2": {"#047857", "#f3faf6"}, "index3": {"#2b6cb0", "#f4f7fb"},
	"index4": {"#0b5ed7", "#f8f9fa"}, "index5": {"#1e4fd8", "#f3f6ff"},
	"index6": {"#7c3aed", "#f7f4ff"}, "index7": {"#38bdf8", "#0b1220"},
	"index8": {"#1f3a5f", "#f6f7f9"}, "index9": {"#006bb8", "#f7fbff"},
	"index10": {"#c2410c", "#fff8f3"},
	"sakura":  {"#be185d", "#fff7fb"}, "starry": {"#a78bfa", "#0f0b2e"},
	"manga": {"#b91c1c", "#fffdf5"}, "mecha": {"#22d3ee", "#070b1a"},
	"candy": {"#c2185b", "#fff6fb"},
}

func homeStyleNames() []string {
	out := make([]string, 0, len(homeStyleList))
	for _, style := range homeStyleList {
		out = append(out, style.Name)
	}
	return out
}
func homeStyle(name string) (HomeStyleInfo, bool) {
	for _, style := range homeStyleList {
		if style.Name == name {
			return style, true
		}
	}
	return HomeStyleInfo{}, false
}
func HomeTemplates() []Template {
	out := make([]Template, 0, len(homeStyleList))
	for _, style := range homeStyleList {
		// Version 3: every style has its own background and an agreement
		// page; the console reloads the preview pictures by this version.
		out = append(out, Template{Type: "home", Name: style.Name, Title: style.Title, Author: "GOEPAY", Version: "3", Description: style.Description, PreviewURL: "/console/v1/admin/templates/home/" + style.Name + "/preview", HasConfig: true})
	}
	return out
}

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
	return .2126*channel(v>>16&0xff) + .7152*channel(v>>8&0xff) + .0722*channel(v&0xff)
}
func contrast(a, b string) float64 {
	x, y := luminance(a), luminance(b)
	if x < y {
		x, y = y, x
	}
	return (x + .05) / (y + .05)
}
func readable(bg, light, dark string) string {
	if contrast(bg, light) >= contrast(bg, dark) {
		return light
	}
	return dark
}

// HomeData contains only operator text and presentation flags. No financial
// capability is enabled by rendering a link or a template preview.
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
	Year                int
	CTA, Density        string
	DefaultLogoURL      string
	CSS                 template.CSS
	Features, Scenarios []homeFeat
	Steps               []homeStep
	FAQ                 []homeQA
	Resources           []homeResource
	// Agreement renders the service agreement (/agreement.html) inside the
	// style's navigation and footer. Anchor prefixes the home-page section
	// links (#features, #faq): "" on the home page, "/" on other pages.
	Agreement bool
	Anchor    string
	// Anime marks the 二次元 styles, which share home/anime.css.
	Anime bool
	// The custom style (custom.go): sanitized HTML, checked CSS, whether the
	// system navigation and footer stay, and the version shown.
	CustomHTML    template.HTML
	CustomCSS     template.CSS
	CustomFrame   bool
	CustomVersion int
	replacer      *strings.Replacer
}

//go:embed home/page.html
var homeTemplateText string

//go:embed home/agreement.html
var homeAgreementText string

//go:embed home/anime-art.html
var homeAnimeArtText string

//go:embed home/base.css
var homeBaseCSS string

//go:embed home/themes.css
var homeThemeCSS string

//go:embed home/anime.css
var homeAnimeCSS string

var homeTmpl = template.Must(template.Must(template.Must(template.Must(template.New("home").Funcs(template.FuncMap{
	"icon": homeIcon, "inc": func(i int) int { return i + 1 },
}).Parse(homeTemplateText)).Parse(homeAgreementText)).Parse(homeAnimeArtText)).Parse(homeCustomText))

func RenderHome(w io.Writer, r Resolved, d HomeData) error {
	frame, err := buildHomeFrame(r, d)
	if err != nil {
		return err
	}
	if frame.Style == "custom" {
		return renderCustomHome(w, frame, r.Custom, frame.replacer)
	}
	return homeTmpl.ExecuteTemplate(w, "doc", frame)
}

// RenderAgreement renders the service agreement page (/agreement.html) with
// the selected home style, so it shares that style's navigation, background
// and footer, including the contact details from the website settings.
func RenderAgreement(w io.Writer, r Resolved, d HomeData) error {
	frame, err := buildHomeFrame(r, d)
	if err != nil {
		return err
	}
	frame.Agreement, frame.Anchor = true, "/"
	frame.Title = frame.SiteName + "服务协议"
	return homeTmpl.ExecuteTemplate(w, "doc", frame)
}

func buildHomeFrame(r Resolved, d HomeData) (homeFrame, error) {
	if r.Type != "home" {
		return homeFrame{}, ErrInvalid
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
	if contrast(foreground, background) < 4.5 {
		foreground = readable(background, "#ffffff", "#000000")
	}
	if contrast(onAccent, accent) < 4.5 {
		onAccent = readable(accent, "#ffffff", "#000000")
	}
	link := accent
	if contrast(link, background) < 4.5 {
		link = foreground
	}
	styleCSS := homeBaseCSS + homeThemeCSS
	anime := homeAnimeStyles[style.Name]
	if anime {
		styleCSS += homeAnimeCSS
	}
	if style.Name == "custom" {
		styleCSS += homeCustomCSS
	}
	replacer := strings.NewReplacer("$ONACCENT", onAccent, "$ACCENT", accent, "$LINK", link, "$BG", background, "$FG", foreground)
	css := replacer.Replace(styleCSS)
	if d.Notice == "" {
		d.Notice = r.Config.Notice
	}
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
	defaultLogo := "/static/goepay-wordmark-onlight.svg?v=90cafbd35591"
	if contrast("#ffffff", background) > contrast("#0A0F1F", background) {
		defaultLogo = "/static/goepay-wordmark-ondark.svg?v=306905c49705"
	}
	return homeFrame{HomeData: d, Year: time.Now().Year(), CTA: style.CTA, Density: density, DefaultLogoURL: defaultLogo, CSS: template.CSS(css), Features: homeFeatures, Scenarios: homeScenarios, Steps: homeSteps, FAQ: homeFAQ, Resources: homeResources, Anime: anime, replacer: replacer}, nil
}
