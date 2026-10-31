package pagetemplate

import (
	"strings"
	"testing"
)

func TestEveryHomeTemplateOffersUsefulDocumentationAndCompleteSections(t *testing.T) {
	for _, style := range homeStyleList {
		t.Run(style.Name, func(t *testing.T) {
			var out strings.Builder
			if err := RenderHome(&out, Resolved{Type: "home", Name: style.Name, Config: DefaultConfig("home", style.Name)}, HomeData{SiteName: "支付技术服务"}); err != nil {
				t.Fatal(err)
			}
			html := out.String()
			if strings.Count(html, `href="/doc/"`) < 3 {
				t.Error("documentation must be reachable from navigation, main content and footer")
			}
			for _, link := range []string{"/doc/sign_note.html", "/doc/pay_create.html", "/doc/pay_notify.html", "/doc/sdk.html"} {
				if !strings.Contains(html, `href="`+link+`"`) {
					t.Errorf("missing developer resource %s", link)
				}
			}
			for _, section := range []string{"features", "integration", "developers", "faq"} {
				if !strings.Contains(html, `id="`+section+`"`) {
					t.Errorf("missing useful homepage section %s", section)
				}
			}
			for _, unsupported := range []string{"免签约", "费率超低", "满额自动结算到账", "平台合作伙伴", "比同行更优质"} {
				if strings.Contains(html, unsupported) {
					t.Errorf("homepage makes an unverified promise: %s", unsupported)
				}
			}
			if strings.Contains(html, "<script") || strings.Contains(html, "<form") {
				t.Error("public homepage must remain script-free navigation without payment forms")
			}
		})
	}
}

func TestEveryHomeTemplateUsesARealRegistrationDestination(t *testing.T) {
	for _, style := range homeStyleList {
		t.Run(style.Name, func(t *testing.T) {
			for _, open := range []bool{false, true} {
				var out strings.Builder
				if err := RenderHome(&out, Resolved{Type: "home", Name: style.Name, Config: DefaultConfig("home", style.Name)}, HomeData{SiteName: "站点", RegistrationOpen: open}); err != nil {
					t.Fatal(err)
				}
				linked := strings.Contains(out.String(), `href="/merchant/login?enroll=register"`)
				if linked != open {
					t.Errorf("registration link must target its form exactly when enabled; open=%v", open)
				}
				if strings.Contains(out.String(), `href="/test"`) {
					t.Error("disabled test payment must not be advertised")
				}
			}
		})
	}
}

func TestHomePagesIncludeKeyboardAndNativeFAQInteractions(t *testing.T) {
	for _, style := range homeStyleList {
		t.Run(style.Name, func(t *testing.T) {
			var out strings.Builder
			if err := RenderHome(&out, Resolved{Type: "home", Name: style.Name, Config: DefaultConfig("home", style.Name)}, HomeData{SiteName: "站点"}); err != nil {
				t.Fatal(err)
			}
			for _, markup := range []string{":focus-visible", "prefers-reduced-motion", "<details", "<summary", `href="#main-content"`, `id="main-content"`} {
				if !strings.Contains(out.String(), markup) {
					t.Errorf("missing keyboard or script-free interaction: %s", markup)
				}
			}
		})
	}
}
