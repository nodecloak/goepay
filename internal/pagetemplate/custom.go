package pagetemplate

import (
	_ "embed"
	"html/template"
	"io"
	"strings"
)

// The custom home template (自定义 HTML) shows the newest version the super
// administrator saved. It is presentation only, for the home page only: the
// pay and collection pages never take administrator HTML.

//go:embed home/custom.html
var homeCustomText string

//go:embed home/custom.css
var homeCustomCSS string

//go:embed home/custom-sample.html
var customSampleHTML string

//go:embed home/custom-sample.css
var customSampleCSS string

// customFullCSS replaces the system styles when the custom HTML is the whole
// page body: the colour variables, a skip link and the preview bars only.
const customFullCSS = `:root{--bg:$BG;--fg:$FG;--accent:$ACCENT;--on-accent:$ONACCENT;--link:$LINK;--line:color-mix(in srgb,$FG 20%,transparent)}
*,*::before,*::after{box-sizing:border-box}
body{margin:0;background:$BG;color:$FG;overflow-wrap:anywhere}
img{max-width:100%;height:auto}
.skip-link{position:absolute;left:12px;top:-64px;z-index:100;padding:8px 14px;border-radius:8px;background:#fff;color:#163047}
.skip-link:focus{top:12px}
.sandbox-bar,.preview-bar{margin:0;padding:8px 16px;font:13px/1.6 system-ui,sans-serif;text-align:center;background:#fff4e5;color:#7a4b00}
:focus-visible{outline:3px solid $ACCENT;outline-offset:2px}
@media(prefers-reduced-motion:reduce){*,*::before,*::after{animation-duration:.01ms!important;animation-iteration-count:1!important;transition-duration:.01ms!important;scroll-behavior:auto!important}}
`

// CustomContent is one saved version of the custom home page: the
// administrator's source text. Assets lists the template images it shows.
type CustomContent struct {
	Version int
	HTML    string
	CSS     string
	Frame   bool
	Assets  []string
	clean   *SanitizedHTML // set by the store once checked
}

// CustomSample is the starter page offered in the console and shown before a
// version is saved.
func CustomSample() CustomContent {
	return CustomContent{HTML: customSampleHTML, CSS: customSampleCSS, Frame: true}
}

// checkCustom sanitizes the HTML and checks the CSS of a custom page.
func checkCustom(c CustomContent) (SanitizedHTML, error) {
	clean, e := SanitizeCustomHTML(c.HTML)
	if e != nil {
		return clean, e
	}
	if e = CheckCustomCSS(c.CSS); e != nil {
		return clean, e
	}
	return clean, nil
}

func renderCustomHome(w io.Writer, frame homeFrame, c *CustomContent, replacer *strings.Replacer) error {
	content := CustomSample()
	if c != nil {
		content = *c
	}
	var clean SanitizedHTML
	if content.clean != nil {
		clean = *content.clean
	} else {
		var e error
		if clean, e = checkCustom(content); e != nil {
			return e
		}
	}
	markup := clean.HTML
	if frame.Preview {
		// The only way this text occurs in sanitized output is an <img src>
		// written by the sanitizer: text and other values are escaped.
		markup = strings.ReplaceAll(markup, ` src="/template-assets/`, ` src="/console/v1/admin/templates/images/`)
	}
	if !content.Frame {
		frame.CSS = template.CSS(replacer.Replace(customFullCSS))
	}
	frame.CustomHTML = template.HTML(markup)
	frame.CustomCSS = template.CSS(content.CSS)
	frame.CustomFrame = content.Frame
	frame.CustomVersion = content.Version
	return homeTmpl.ExecuteTemplate(w, "custom-doc", frame)
}
