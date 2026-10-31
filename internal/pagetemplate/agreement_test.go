package pagetemplate

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func renderAgreement(t *testing.T, style string, d HomeData) string {
	t.Helper()
	var b strings.Builder
	if err := RenderAgreement(&b, Resolved{Type: "home", Name: style, Config: DefaultConfig("home", style)}, d); err != nil {
		t.Fatal(style, err)
	}
	return b.String()
}

// /agreement.html belongs to every home template: it keeps the style's
// navigation, backdrop and footer, and carries the original agreement's
// sections with the site name filled in.
func TestAgreementRendersInEveryHomeStyle(t *testing.T) {
	for _, style := range homeStyleList {
		t.Run(style.Name, func(t *testing.T) {
			out := renderAgreement(t, style.Name, HomeData{SiteName: "示例支付"})
			for _, want := range []string{
				`data-home-style="` + style.Name + `"`,
				`class="home home-` + style.Name + `"`,
				"<title>示例支付服务协议</title>",
				"<h1>示例支付服务协议</h1>",
				"本协议由示例支付系统和你共同签订",
				"16.4本协议一式貳份",
				`href="/#features"`, `href="/#faq"`,
				`href="/agreement.html"`, `href="/doc/"`, `href="/merchant/login"`,
			} {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q", want)
				}
			}
			for i := 0; i <= 16; i++ {
				if !strings.Contains(out, `<h2 id="a`+strconv.Itoa(i)+`">`) || !strings.Contains(out, `href="#a`+strconv.Itoa(i)+`"`) {
					t.Errorf("section a%d or its table-of-contents link is missing", i)
				}
			}
			// Copy errors of the original (another provider's names) are not kept.
			for _, unwanted := range []string{"腾讯", "微信公众平台", "财付将", "公众账号", "码上付", "config_get", "<script", "<form", `id="features"`, `id="faq"`} {
				if strings.Contains(out, unwanted) {
					t.Errorf("agreement page contains %q", unwanted)
				}
			}
			if strings.Contains(out, `id="contact"`) || strings.Contains(out, "联系我们") {
				t.Error("contact details shown although none are set")
			}
		})
	}
}

func TestAgreementShowsTheContactDetailsThatAreSet(t *testing.T) {
	d := HomeData{SiteName: "站点", Org: "示例<b>科技</b>", Email: "support@example.com", QQ: "10001", QQURL: "https://wpa.qq.com/msgrd?v=3&uin=10001", WeWork: "https://work.weixin.qq.com/kfid/kfc1", QQGroup: "https://qm.qq.com/q/abc"}
	out := renderAgreement(t, "index7", d)
	for _, want := range []string{
		`<section class="agreement-contact" id="contact"`, `href="#contact"`, ">客服</a>咨询",
		`href="mailto:support@example.com"`, ">10001</a>",
		`href="https://wpa.qq.com/msgrd?v=3&amp;uin=10001"`,
		`href="https://work.weixin.qq.com/kfid/kfc1"`, `href="https://qm.qq.com/q/abc"`,
		"示例&lt;b&gt;科技&lt;/b&gt;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "<b>科技") {
		t.Fatal("organisation name was not escaped")
	}
	// Each detail appears on its own: a QQ number without a link stays text,
	// and nothing is shown for details that are not set.
	only := renderAgreement(t, "default", HomeData{SiteName: "站点", QQ: "10002"})
	if !strings.Contains(only, "<dt>客服QQ</dt><dd>10002</dd>") || strings.Contains(only, "mailto:") || strings.Contains(only, "<dt>企业微信</dt>") || strings.Contains(only, "<dt>QQ群</dt>") {
		t.Fatal("contact list must show exactly the QQ number")
	}
	// Settings validation already refuses such links; the template still
	// neutralises them.
	if evil := renderAgreement(t, "default", HomeData{SiteName: "站点", QQGroup: "javascript:alert(1)"}); strings.Contains(evil, "javascript:") {
		t.Fatal("unsafe link was rendered")
	}
}

// The home page footer links the agreement; previews keep cross-page links inert.
func TestHomeFooterLinksTheAgreement(t *testing.T) {
	for _, style := range homeStyleList {
		var live, preview strings.Builder
		r := Resolved{Type: "home", Name: style.Name, Config: DefaultConfig("home", style.Name)}
		if err := RenderHome(&live, r, HomeData{SiteName: "站点"}); err != nil {
			t.Fatal(err)
		}
		if err := RenderHome(&preview, r, HomeData{SiteName: "站点", Preview: true}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(live.String(), `href="/agreement.html">服务协议</a>`) {
			t.Fatalf("%s: footer does not link the agreement", style.Name)
		}
		if strings.Contains(preview.String(), `href="/agreement.html"`) || !strings.Contains(preview.String(), `tabindex="-1">服务协议</a>`) {
			t.Fatalf("%s: preview must keep the agreement link inert", style.Name)
		}
		if strings.Contains(live.String(), `href="/#`) {
			t.Fatalf("%s: home page section links must stay on the page", style.Name)
		}
	}
}

// Every style draws its own backdrop, from CSS gradients only: the public
// page allows images from its own origin only, and previews are sandboxed.
func TestEveryHomeStyleHasItsOwnBackground(t *testing.T) {
	css := homeBaseCSS + homeThemeCSS
	for _, style := range homeStyleList {
		rule := regexp.MustCompile(regexp.QuoteMeta(".home-"+style.Name) + `( \.hero)?(::before|::after)?\{[^}]*background`)
		if !rule.MatchString(css) {
			t.Errorf("%s has no background of its own", style.Name)
		}
	}
	if strings.Contains(css, "url(") {
		t.Fatal("home backgrounds must not load images")
	}
}
