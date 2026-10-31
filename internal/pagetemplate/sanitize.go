package pagetemplate

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Custom HTML for the home page is untrusted markup typed in the console. It
// is tokenized and written back from scratch: only allowlisted elements and
// attributes are emitted, every text and attribute value is escaped, every
// attribute is quoted and every element that was opened is closed. The
// browser therefore only ever parses this package's own, unambiguous output,
// whatever the input looked like. The home page CSP (no script source at
// all, images from this site only) stays the second line of defence.

const (
	MaxCustomHTML   = 64 << 10
	MaxCustomCSS    = 32 << 10
	maxCustomAssets = 64
	maxCustomDepth  = 128
)

// CustomError is a refused custom page with a reason for the administrator.
type CustomError struct{ Reason string }

func (e *CustomError) Error() string { return e.Reason }
func (e *CustomError) Unwrap() error { return ErrInvalid }

// SanitizedHTML is the custom HTML as it is served.
type SanitizedHTML struct {
	HTML    string
	Assets  []string       // template image IDs referenced by <img src>
	Removed map[string]int // what was dropped, reported in the console
}

var (
	customVoid = map[string]bool{"br": true, "col": true, "hr": true, "img": true, "wbr": true}
	customKeep = map[string]bool{
		"a": true, "abbr": true, "address": true, "article": true, "aside": true, "b": true, "bdi": true, "bdo": true,
		"blockquote": true, "br": true, "caption": true, "cite": true, "code": true, "col": true, "colgroup": true,
		"data": true, "dd": true, "del": true, "details": true, "dfn": true, "div": true, "dl": true, "dt": true,
		"em": true, "figcaption": true, "figure": true, "footer": true, "h1": true, "h2": true, "h3": true, "h4": true,
		"h5": true, "h6": true, "header": true, "hgroup": true, "hr": true, "i": true, "img": true, "ins": true,
		"kbd": true, "li": true, "mark": true, "nav": true, "ol": true, "p": true, "pre": true, "q": true, "rp": true,
		"rt": true, "ruby": true, "s": true, "samp": true, "section": true, "small": true, "span": true,
		"strong": true, "sub": true, "summary": true, "sup": true, "table": true, "tbody": true, "td": true,
		"tfoot": true, "th": true, "thead": true, "time": true, "tr": true, "u": true, "ul": true, "var": true,
		"wbr": true,
	}
	// Elements whose content is raw text in the HTML parser: skipped up to
	// their end tag, content included.
	customRawText = map[string]bool{"script": true, "style": true, "textarea": true, "title": true, "xmp": true, "iframe": true, "noembed": true, "noframes": true, "noscript": true}
	// Elements dropped together with everything inside them.
	customDropTree = map[string]bool{"template": true, "object": true, "applet": true, "svg": true, "math": true, "select": true, "datalist": true, "canvas": true, "video": true, "audio": true, "picture": true, "map": true, "frameset": true, "dialog": true, "portal": true}
	// Void elements dropped (they can load, submit or redirect).
	customDropVoid = map[string]bool{"input": true, "meta": true, "link": true, "base": true, "embed": true, "source": true, "track": true, "area": true, "param": true, "keygen": true, "frame": true, "image": true, "isindex": true}
	// Not reported: the wrapper of a pasted full document.
	customQuiet = map[string]bool{"html": true, "head": true, "body": true}
)

var (
	customAssetSrc = regexp.MustCompile(`^/template-assets/([0-9a-f]{64})$`)
	customID       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_:.-]{0,63}$`)
	customClass    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	customNumber   = regexp.MustCompile(`^[0-9]{1,4}$`)
	customInteger  = regexp.MustCompile(`^-?[0-9]{1,6}$`)
	customLang     = regexp.MustCompile(`^[A-Za-z]{1,8}(-[A-Za-z0-9]{1,8}){0,4}$`)
	customAttrName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	customARIA     = regexp.MustCompile(`^aria-[a-z]{2,32}$`)
	customRole     = regexp.MustCompile(`^[a-z]{2,32}$`)
)

type customSanitizer struct {
	src      string
	pos      int
	out      strings.Builder
	stack    []string
	skipName string
	skip     int
	removed  map[string]int
	assets   []string
	seen     map[string]bool
}

// SanitizeCustomHTML checks the size and encoding of the administrator's
// HTML and returns the markup that is served. Always pass the stored source:
// escaping can make the output several times longer than the input, so the
// output itself may exceed MaxCustomHTML.
func SanitizeCustomHTML(src string) (SanitizedHTML, error) {
	if len(src) > MaxCustomHTML {
		return SanitizedHTML{}, &CustomError{"HTML 超过 64 KiB"}
	}
	if !utf8.ValidString(src) {
		return SanitizedHTML{}, &CustomError{"HTML 不是有效的 UTF-8 文本"}
	}
	p := &customSanitizer{src: strings.ReplaceAll(src, "\x00", ""), removed: map[string]int{}, seen: map[string]bool{}}
	p.run()
	if len(p.assets) > maxCustomAssets {
		return SanitizedHTML{}, &CustomError{"HTML 最多引用 64 张模板图片"}
	}
	return SanitizedHTML{HTML: p.out.String(), Assets: p.assets, Removed: p.removed}, nil
}

func (p *customSanitizer) drop(what string) { p.removed[what]++ }

func (p *customSanitizer) run() {
	s := p.src
	for p.pos < len(s) {
		i := strings.IndexByte(s[p.pos:], '<')
		if i < 0 {
			p.text(s[p.pos:])
			break
		}
		if i > 0 {
			p.text(s[p.pos : p.pos+i])
			p.pos += i
		}
		rest := s[p.pos:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			p.comment()
		case len(rest) > 1 && (rest[1] == '!' || rest[1] == '?'):
			// Doctype, CDATA or processing instruction: skipped like a bogus comment.
			if end := strings.IndexByte(rest, '>'); end < 0 {
				p.pos = len(s)
			} else {
				p.pos += end + 1
			}
		case len(rest) > 2 && rest[1] == '/' && asciiAlpha(rest[2]):
			p.endTag()
		case len(rest) > 1 && asciiAlpha(rest[1]):
			p.startTag()
		default:
			p.text("<")
			p.pos++
		}
	}
	for i := len(p.stack) - 1; i >= 0; i-- {
		p.out.WriteString("</" + p.stack[i] + ">")
	}
	p.stack = nil
}

func asciiAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func htmlSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}

func (p *customSanitizer) comment() {
	rest := p.src[p.pos:]
	if strings.HasPrefix(rest, "<!-->") {
		p.pos += 5
		return
	}
	if strings.HasPrefix(rest, "<!--->") {
		p.pos += 6
		return
	}
	end, size := -1, 0
	if i := strings.Index(rest[4:], "-->"); i >= 0 {
		end, size = i, 3
	}
	if i := strings.Index(rest[4:], "--!>"); i >= 0 && (end < 0 || i < end) {
		end, size = i, 4
	}
	if end < 0 {
		p.pos = len(p.src)
		return
	}
	p.pos += 4 + end + size
}

func (p *customSanitizer) text(raw string) {
	if p.skip > 0 || raw == "" {
		return
	}
	p.out.WriteString(html.EscapeString(html.UnescapeString(raw)))
}

type customAttr struct{ name, value string }

// tag reads a tag name and its attributes from p.pos (at '<' or '</'); ok is
// false when the input ends inside the tag, which drops the tag as the HTML
// parser does.
func (p *customSanitizer) tag(skip int) (name string, attrs []customAttr, ok bool) {
	s := p.src
	i := p.pos + skip
	start := i
	for i < len(s) && !htmlSpace(s[i]) && s[i] != '/' && s[i] != '>' {
		i++
	}
	name = strings.ToLower(s[start:i])
	for {
		for i < len(s) && (htmlSpace(s[i]) || s[i] == '/') {
			i++
		}
		if i >= len(s) {
			p.pos = len(s)
			return name, attrs, false
		}
		if s[i] == '>' {
			p.pos = i + 1
			return name, attrs, true
		}
		start = i
		i++ // a leading '=' belongs to the name
		for i < len(s) && !htmlSpace(s[i]) && s[i] != '/' && s[i] != '>' && s[i] != '=' {
			i++
		}
		attr := customAttr{name: strings.ToLower(s[start:i])}
		j := i
		for j < len(s) && htmlSpace(s[j]) {
			j++
		}
		if j < len(s) && s[j] == '=' {
			j++
			for j < len(s) && htmlSpace(s[j]) {
				j++
			}
			if j >= len(s) {
				p.pos = len(s)
				return name, attrs, false
			}
			if q := s[j]; q == '"' || q == '\'' {
				end := strings.IndexByte(s[j+1:], q)
				if end < 0 {
					p.pos = len(s)
					return name, attrs, false
				}
				attr.value = s[j+1 : j+1+end]
				j += end + 2
			} else {
				vs := j
				for j < len(s) && !htmlSpace(s[j]) && s[j] != '>' {
					j++
				}
				attr.value = s[vs:j]
			}
			i = j
		}
		attr.value = decodeAttribute(attr.value)
		attrs = append(attrs, attr)
	}
}

// decodeAttribute decodes character references that the browser decodes in
// an attribute value: numeric references and named references ending in ';'.
// Whatever it yields is what the browser reads back from the escaped output.
func decodeAttribute(v string) string {
	if !strings.Contains(v, "&") {
		return v
	}
	var b strings.Builder
	for i := 0; i < len(v); {
		if v[i] != '&' {
			b.WriteByte(v[i])
			i++
			continue
		}
		j := i + 1
		if j < len(v) && v[j] == '#' {
			j++
			hex := j < len(v) && (v[j] == 'x' || v[j] == 'X')
			if hex {
				j++
			}
			digits := j
			for j < len(v) && (v[j] >= '0' && v[j] <= '9' || hex && (v[j] >= 'a' && v[j] <= 'f' || v[j] >= 'A' && v[j] <= 'F')) {
				j++
			}
			if j > digits {
				if j < len(v) && v[j] == ';' {
					j++
				}
				ref := v[i:j]
				if !strings.HasSuffix(ref, ";") {
					ref += ";"
				}
				b.WriteString(html.UnescapeString(ref))
				i = j
				continue
			}
		} else {
			for j < len(v) && (asciiAlpha(v[j]) || v[j] >= '0' && v[j] <= '9') {
				j++
			}
			if j > i+1 && j < len(v) && v[j] == ';' {
				ref := v[i : j+1]
				if decoded := html.UnescapeString(ref); decoded != ref {
					b.WriteString(decoded)
					i = j + 1
					continue
				}
			}
		}
		b.WriteByte('&')
		i++
	}
	return b.String()
}

func (p *customSanitizer) startTag() {
	name, attrs, ok := p.tag(1)
	if !ok {
		return
	}
	if p.skip > 0 {
		if name == p.skipName {
			p.skip++
		} else if customRawText[name] {
			p.skipRawText(name)
		}
		return
	}
	switch {
	case name == "plaintext":
		p.drop("<plaintext>")
		p.pos = len(p.src)
	case customRawText[name]:
		p.drop("<" + name + ">")
		p.skipRawText(name)
	case customDropTree[name]:
		p.drop("<" + name + ">")
		p.skipName, p.skip = name, 1
	case customDropVoid[name]:
		p.drop("<" + name + ">")
	case !customKeep[name]:
		if !customQuiet[name] {
			p.drop("<" + name + ">")
		}
	case len(p.stack) >= maxCustomDepth:
		p.drop("嵌套过深的元素")
	default:
		p.element(name, attrs)
	}
}

func (p *customSanitizer) skipRawText(name string) {
	s := p.src
	for i := p.pos; ; {
		j := strings.Index(s[i:], "</")
		if j < 0 {
			p.pos = len(s)
			return
		}
		at := i + j + 2
		if len(s)-at >= len(name) && strings.EqualFold(s[at:at+len(name)], name) && (at+len(name) == len(s) || htmlSpace(s[at+len(name)]) || s[at+len(name)] == '/' || s[at+len(name)] == '>') {
			end := strings.IndexByte(s[at:], '>')
			if end < 0 {
				p.pos = len(s)
			} else {
				p.pos = at + end + 1
			}
			return
		}
		i = at
	}
}

func (p *customSanitizer) endTag() {
	name, _, ok := p.tag(2)
	if !ok {
		return
	}
	if p.skip > 0 {
		if name == p.skipName {
			p.skip--
			if p.skip == 0 {
				p.skipName = ""
			}
		}
		return
	}
	for i := len(p.stack) - 1; i >= 0; i-- {
		if p.stack[i] == name {
			for j := len(p.stack) - 1; j >= i; j-- {
				p.out.WriteString("</" + p.stack[j] + ">")
			}
			p.stack = p.stack[:i]
			return
		}
	}
}

func (p *customSanitizer) element(name string, attrs []customAttr) {
	var b strings.Builder
	b.WriteString("<" + name)
	seen := map[string]bool{}
	blank, hasSrc := false, false
	for _, a := range attrs {
		if seen[a.name] {
			continue // the browser keeps the first of duplicated attributes
		}
		seen[a.name] = true
		value, keep := p.attribute(name, a)
		if !keep {
			continue
		}
		switch a.name {
		case "target":
			blank = value == "_blank"
		case "rel":
			continue // written below from target
		case "src":
			hasSrc = true
		}
		if value == "\x00bool" {
			b.WriteString(" " + a.name)
			continue
		}
		b.WriteString(" " + a.name + `="` + html.EscapeString(value) + `"`)
	}
	if name == "img" && !hasSrc {
		p.drop("图片（只能用已上传的模板图片）")
		return
	}
	if name == "a" && blank {
		b.WriteString(` rel="noopener noreferrer"`)
	}
	b.WriteString(">")
	p.out.WriteString(b.String())
	if !customVoid[name] {
		p.stack = append(p.stack, name)
	}
}

// attribute returns the value to write and whether the attribute is kept.
// The value "\x00bool" writes a boolean attribute without a value.
func (p *customSanitizer) attribute(element string, a customAttr) (string, bool) {
	name, v := a.name, a.value
	if !customAttrName.MatchString(name) {
		p.drop("属性 " + clip(name, 32))
		return "", false
	}
	if strings.HasPrefix(name, "on") {
		p.drop(clip(name, 32))
		return "", false
	}
	refuse := func() (string, bool) {
		p.drop(name + "（不允许的值）")
		return "", false
	}
	switch {
	case name == "class":
		var tokens []string
		for _, t := range strings.Fields(v) {
			if customClass.MatchString(t) && len(tokens) < 32 {
				tokens = append(tokens, t)
			}
		}
		if len(tokens) == 0 {
			return "", false
		}
		return strings.Join(tokens, " "), true
	case name == "id":
		if !customID.MatchString(v) {
			return refuse()
		}
		return v, true
	case name == "title" || name == "alt" && element == "img" || name == "abbr" && (element == "th" || element == "td") || customARIA.MatchString(name):
		return clip(v, 512), true
	case name == "role":
		if !customRole.MatchString(v) {
			return refuse()
		}
		return v, true
	case name == "lang":
		if !customLang.MatchString(v) {
			return refuse()
		}
		return v, true
	case name == "dir":
		if v != "ltr" && v != "rtl" && v != "auto" {
			return refuse()
		}
		return v, true
	case name == "hidden" || name == "open" && element == "details" || name == "reversed" && element == "ol":
		return "\x00bool", true
	case name == "style":
		if CheckCustomCSS(v) != nil || strings.ContainsAny(v, "{}") {
			p.drop("style（含不允许的样式）")
			return "", false
		}
		return v, true
	case name == "href" && element == "a" || name == "cite" && (element == "blockquote" || element == "q" || element == "del" || element == "ins"):
		if !customURL(v) {
			p.drop(name + "（不安全的地址）")
			return "", false
		}
		return strings.TrimSpace(v), true
	case name == "target" && element == "a":
		if v != "_blank" && v != "_self" {
			return refuse()
		}
		return v, true
	case name == "rel" && element == "a":
		return "", true
	case name == "src" && element == "img":
		m := customAssetSrc.FindStringSubmatch(strings.TrimSpace(v))
		if m == nil {
			return "", false
		}
		if !p.seen[m[1]] {
			p.seen[m[1]] = true
			p.assets = append(p.assets, m[1])
		}
		return m[0], true
	case (name == "width" || name == "height") && element == "img", (name == "colspan" || name == "rowspan") && (element == "td" || element == "th"), name == "span" && (element == "col" || element == "colgroup"):
		if !customNumber.MatchString(v) {
			return refuse()
		}
		return v, true
	case name == "loading" && element == "img":
		if v != "lazy" && v != "eager" {
			return refuse()
		}
		return v, true
	case name == "decoding" && element == "img":
		if v != "async" && v != "sync" && v != "auto" {
			return refuse()
		}
		return v, true
	case name == "scope" && (element == "th" || element == "td"):
		if v != "row" && v != "col" && v != "rowgroup" && v != "colgroup" {
			return refuse()
		}
		return v, true
	case name == "headers" && (element == "th" || element == "td"):
		return clip(v, 256), true
	case name == "start" && element == "ol", name == "value" && element == "li":
		if !customInteger.MatchString(v) {
			return refuse()
		}
		return v, true
	case name == "type" && element == "ol":
		if v != "1" && v != "a" && v != "A" && v != "i" && v != "I" {
			return refuse()
		}
		return v, true
	case name == "datetime" && (element == "time" || element == "del" || element == "ins"), name == "value" && element == "data":
		return clip(v, 128), true
	case name == "translate":
		if v != "yes" && v != "no" {
			return refuse()
		}
		return v, true
	}
	p.drop("属性 " + clip(name, 32))
	return "", false
}

func clip(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max])
}

// customURL accepts relative addresses and the http, https, mailto and tel
// schemes. Tabs and line breaks are removed first, as the browser's URL
// parser does, so that "java\tscript:" is read as the browser would.
func customURL(v string) bool {
	v = strings.TrimSpace(v)
	v = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, v)
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	colon := strings.IndexByte(v, ':')
	if colon < 0 {
		return true
	}
	if cut := strings.IndexAny(v, "/?#"); cut >= 0 && cut < colon {
		return true // the colon is inside a relative path, query or fragment
	}
	switch strings.ToLower(v[:colon]) {
	case "http", "https", "mailto", "tel":
		return true
	}
	return false
}

var customCSSComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

// CheckCustomCSS refuses CSS that could load from elsewhere, run legacy
// script-like extensions or end the <style> element it is written into.
func CheckCustomCSS(css string) error {
	if len(css) > MaxCustomCSS {
		return &CustomError{"CSS 超过 32 KiB"}
	}
	if !utf8.ValidString(css) || strings.ContainsRune(css, 0) {
		return &CustomError{"CSS 不是有效的 UTF-8 文本"}
	}
	if strings.ContainsAny(css, `<\`) {
		return &CustomError{`CSS 不能包含 < 或 \ 字符`}
	}
	plain := strings.ToLower(customCSSComment.ReplaceAllString(css, ""))
	compact := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			return -1
		}
		return r
	}, plain)
	for _, rule := range []struct{ token, reason string }{
		{"url(", "CSS 不能使用 url()，图片请用 <img> 和已上传的模板图片"},
		{"@import", "CSS 不能使用 @import"},
		{"image(", "CSS 不能使用 image() 或 image-set()"},
		{"image-set(", "CSS 不能使用 image() 或 image-set()"},
		{"cross-fade(", "CSS 不能使用 cross-fade()"},
		{"element(", "CSS 不能使用 element()"},
		{"src(", "CSS 不能使用 src()"},
		{"expression(", "CSS 不能使用 expression()"},
		{"javascript:", "CSS 不能包含 javascript:"},
		{"vbscript:", "CSS 不能包含 vbscript:"},
		{"behavior:", "CSS 不能使用 behavior"},
		{"-moz-binding", "CSS 不能使用 -moz-binding"},
	} {
		if strings.Contains(compact, rule.token) {
			return &CustomError{rule.reason}
		}
	}
	return nil
}
