package pagetemplate

import (
	"bytes"
	"strings"
	"testing"
)

// The 二次元 home styles draw an original inline-SVG mascot that screen
// readers skip, and only those pages carry home/anime.css.
func TestAnimeHomeStylesDrawTheirMascotAndOnlyTheyLoadAnimeCSS(t *testing.T) {
	anime := 0
	for _, style := range homeStyleList {
		var b strings.Builder
		if err := RenderHome(&b, Resolved{Type: "home", Name: style.Name, Config: DefaultConfig("home", style.Name)}, HomeData{SiteName: "站点"}); err != nil {
			t.Fatal(style.Name, err)
		}
		out := b.String()
		art := `<svg class="anime-art art-` + style.Name + `"`
		if !homeAnimeStyles[style.Name] {
			if strings.Contains(out, "anime-art") || strings.Contains(out, ".hero-anime") || strings.Contains(out, "home-anime") {
				t.Errorf("%s carries the anime art or styles", style.Name)
			}
			continue
		}
		anime++
		start := strings.Index(out, art)
		if start < 0 || !strings.Contains(out, `class="home home-`+style.Name+` home-anime"`) || !strings.Contains(out, ".hero-anime-grid{") {
			t.Errorf("%s: mascot, page class or anime styles missing", style.Name)
			continue
		}
		svg := out[start : start+strings.Index(out[start:], "</svg>")]
		if !strings.Contains(svg, `aria-hidden="true"`) || !strings.Contains(svg, `focusable="false"`) {
			t.Errorf("%s: the mascot must be hidden from assistive technology", style.Name)
		}
		assertDrawnFromShapes(t, style.Name, svg)
	}
	if anime != 5 {
		t.Fatalf("%d anime home styles, want 5", anime)
	}
}

// The anime pay page keeps the checkout content of every pay page (the
// handlers write it into <main>) and adds the kitten and its own styles.
func TestAnimePayPageKeepsTheCheckoutAndAddsItsMascot(t *testing.T) {
	render := func(name string) string {
		t.Helper()
		var b bytes.Buffer
		cfg := DefaultConfig("pay", name)
		cfg.BrandName = `<script>alert(1)</script>`
		tail, err := Open(&b, Resolved{Type: "pay", Name: name, Config: cfg}, Page{Title: "支付收银台", TradeNo: "LOCAL1", Amount: "1.00", OrderName: "<img src=x onerror=1>"})
		if err != nil {
			t.Fatal(name, err)
		}
		b.WriteString(`<p>请扫码</p><img alt="支付二维码" src="/qr.png"><a href="/pay">打开支付</a>` + tail)
		return b.String()
	}
	out := render("anime")
	for _, want := range []string{`data-template="anime"`, `<main class="template-anime">`, "template-band", `<span class="template-mascot" aria-hidden="true">`, "&lt;script&gt;", "LOCAL1", "1.00", "main>img,main>svg{", ".template-anime>img,.template-anime>svg{", "max-width:520px"} {
		if !strings.Contains(out, want) {
			t.Errorf("anime pay page missing %q", want)
		}
	}
	if strings.Contains(out, "<script>alert(1)</script>") || strings.Contains(out, "<img src=x") {
		t.Fatal("anime pay page did not escape operator text")
	}
	svg := out[strings.Index(out, `<span class="template-mascot"`):]
	assertDrawnFromShapes(t, "pay/anime", svg[:strings.Index(svg, "</svg>")])
	for _, name := range []string{"default", "compact"} {
		if other := render(name); strings.Contains(other, "template-mascot") || strings.Contains(other, ".template-anime") {
			t.Errorf("pay/%s carries the anime mascot or styles", name)
		}
	}
	c := DefaultConfig("pay", "anime")
	if on := readable(c.AccentColor, "#ffffff", "#0b1220"); contrast(on, c.AccentColor) < 4.5 || contrast("#163047", c.BackgroundColor) < 4.5 {
		t.Fatalf("anime pay colours are not readable: %+v", c)
	}
}

// Every animation of the anime templates runs only when the visitor allows motion.
func TestAnimeMotionRespectsReducedMotion(t *testing.T) {
	for name, css := range map[string]string{"home": homeAnimeCSS, "pay": frameAnimeCSS} {
		const guard = "@media(prefers-reduced-motion:no-preference){"
		outside, rest := "", css
		for {
			i := strings.Index(rest, guard)
			if i < 0 {
				outside += rest
				break
			}
			outside += rest[:i]
			depth, j := 1, i+len(guard)
			for ; j < len(rest) && depth > 0; j++ {
				switch rest[j] {
				case '{':
					depth++
				case '}':
					depth--
				}
			}
			rest = rest[j:]
		}
		if !strings.Contains(css, guard) || strings.Contains(outside, "animation:") || strings.Contains(outside, "animation-name") {
			t.Errorf("%s: an anime animation runs without the reduced-motion guard", name)
		}
		if strings.Contains(css, "url(") {
			t.Errorf("%s: anime styles must not load images", name)
		}
	}
}

// Drawn from shapes only: no pictures, external references, script or handlers.
func assertDrawnFromShapes(t *testing.T, name, svg string) {
	t.Helper()
	for _, unwanted := range []string{"<image", "<script", "<foreignObject", "http:", "https:", "javascript:", " on"} {
		if strings.Contains(svg, unwanted) {
			t.Errorf("%s: mascot contains %q", name, unwanted)
		}
	}
}
