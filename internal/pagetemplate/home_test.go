package pagetemplate

import (
	"strings"
	"testing"
)

func TestHomeStylesRenderSafelyAndDistinctly(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range homeStyleList {
		var b strings.Builder
		d := HomeData{
			SiteName:    `聚合支付<script>alert(1)</script>`,
			Title:       "首页",
			Keywords:    "支付",
			Description: "聚合支付平台",
			Org:         "示例科技",
			Email:       "ops@example.com",
			QQ:          "10001",
			QQGroup:     "https://qm.qq.com/q/abc",
			Notice:      "本地模拟环境<img src=x onerror=1>",
			TestOpen:    true,
		}
		r := Resolved{Type: "home", Name: s.Name, Config: DefaultConfig("home", s.Name)}
		r.Config.BackgroundColor = "#ddeeff"
		if e := RenderHome(&b, r, d); e != nil {
			t.Fatal(s.Name, e)
		}
		out := b.String()
		// Operator text is escaped; no raw script or event-handler injection.
		if strings.Contains(out, "<script>alert(1)</script>") || strings.Contains(out, "<img src=x") {
			t.Fatal("home style did not escape operator text", s.Name)
		}
		if !strings.Contains(out, "&lt;script&gt;") {
			t.Fatal("expected escaped site name", s.Name)
		}
		for _, want := range []string{
			`data-home-style="` + s.Name + `"`,
			"background:#ddeeff", // the configured background colour is applied
			"/merchant/login",    // login/register link present
			`聚合支付`,               // the site name (brand) is rendered
			"本地模拟环境",             // the notice text is rendered
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("home style %s missing %q", s.Name, want)
			}
		}
		if strings.Contains(out, "<script") {
			t.Fatal("home landing must carry no script", s.Name)
		}
		seen[out] = true
	}
	if len(seen) != len(homeStyleList) {
		t.Fatalf("home styles are not visually distinct: %d unique of %d", len(seen), len(homeStyleList))
	}
}

func TestRenderHomeRejectsNonHomeAndFallsBackToDefaultStyle(t *testing.T) {
	var b strings.Builder
	if e := RenderHome(&b, Resolved{Type: "pay", Name: "default", Config: DefaultConfig("pay", "default")}, HomeData{SiteName: "x"}); e != ErrInvalid {
		t.Fatal("non-home kind must be refused")
	}
	b.Reset()
	r := Resolved{Type: "home", Name: "nonexistent", Config: DefaultConfig("home", "default")}
	if e := RenderHome(&b, r, HomeData{SiteName: "GOEPAY"}); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(b.String(), `data-home-style="default"`) {
		t.Fatal("unknown style must fall back to default")
	}
}

func TestHomeLinksBannerLogoAndDensityFollowSettings(t *testing.T) {
	render := func(name string, cfg Config, d HomeData) string {
		t.Helper()
		var b strings.Builder
		if e := RenderHome(&b, Resolved{Type: "home", Name: name, Config: cfg}, d); e != nil {
			t.Fatal(name, e)
		}
		return b.String()
	}
	for _, s := range homeStyleList {
		cfg := DefaultConfig("home", s.Name)
		closed := render(s.Name, cfg, HomeData{SiteName: "站点"})
		if strings.Contains(closed, ">"+s.CTA+"</a>") || strings.Contains(closed, ">注册商户</a>") || strings.Contains(closed, `href="/test"`) {
			t.Fatal("closed registration or test pay still linked", s.Name)
		}
		if !strings.Contains(closed, ">商户登录</a>") {
			t.Fatal("login link missing", s.Name)
		}
		open := render(s.Name, cfg, HomeData{SiteName: "站点", RegistrationOpen: true, TestOpen: true})
		if !strings.Contains(open, ">"+s.CTA+"</a>") || !strings.Contains(open, `href="/test"`) {
			t.Fatal("open registration or test pay not linked", s.Name)
		}
		if strings.Contains(open, "不涉及真实资金") {
			t.Fatal("sandbox banner shown outside sandbox", s.Name)
		}
		if !strings.Contains(render(s.Name, cfg, HomeData{SiteName: "站点", Sandbox: true}), "本地模拟环境，不涉及真实资金。") {
			t.Fatal("sandbox banner missing", s.Name)
		}
	}
	id := strings.Repeat("ab", 32)
	cfg := DefaultConfig("home", "default")
	cfg.LogoAssetID = id
	if out := render("default", cfg, HomeData{}); !strings.Contains(out, `src="/template-assets/`+id+`"`) {
		t.Fatal("template logo not used on the public page")
	}
	if out := render("default", cfg, HomeData{Preview: true}); !strings.Contains(out, `src="/console/v1/admin/templates/images/`+id+`"`) {
		t.Fatal("template logo not served from the console path in preview")
	}
	if out := render("default", cfg, HomeData{LogoURL: "/static/img/logo.png"}); !strings.Contains(out, `src="/static/img/logo.png"`) || strings.Contains(out, id) {
		t.Fatal("site logo must win over the template image")
	}
	cfg.LogoAssetID = "../../etc"
	if out := render("default", cfg, HomeData{}); strings.Contains(out, "../../etc") || !strings.Contains(out, `/static/goepay-wordmark-onlight.svg?v=90cafbd35591`) {
		t.Fatal("malformed asset id must use the default brand image")
	}
	cfg.Density = "compact"
	if out := render("index1", cfg, HomeData{}); !strings.Contains(out, `data-density="compact"`) {
		t.Fatal("compact spacing not applied")
	}
}

func TestHomeStylesKeepOriginalNamesAndReadableColours(t *testing.T) {
	want := "default,index1,index2,index3,index4,index5,index6,index7,index8,index9,index10,minimal,sakura,starry,manga,mecha,candy"
	if got := strings.Join(homeStyleNames(), ","); got != want {
		t.Fatalf("home styles %s, want %s", got, want)
	}
	for _, s := range homeStyleList {
		c := DefaultConfig("home", s.Name)
		fg := readable(c.BackgroundColor, "#e6edf5", "#163047")
		on := readable(c.AccentColor, "#ffffff", "#0b1220")
		for what, ratio := range map[string]float64{
			"text on background":  contrast(fg, c.BackgroundColor),
			"button text":         contrast(on, c.AccentColor),
			"links on background": contrast(c.AccentColor, c.BackgroundColor),
		} {
			if ratio < 4.5 {
				t.Fatalf("%s: %s contrast %.2f < 4.5 (%+v)", s.Name, what, ratio, c)
			}
		}
	}
	var b strings.Builder
	if e := RenderHome(&b, Resolved{Type: "home", Name: "index7", Config: DefaultConfig("home", "index7")}, HomeData{SiteName: "x"}); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(b.String(), "background:#0b1220;color:#e6edf5") {
		t.Fatal("dark style must switch to light text")
	}
	if readable("#ffffff", "#e6edf5", "#163047") != "#163047" || readable("#000000", "#e6edf5", "#163047") != "#e6edf5" {
		t.Fatal("readable picked the wrong colour")
	}
}
