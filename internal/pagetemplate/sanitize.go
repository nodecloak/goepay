package pagetemplate

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

// The HTML of an uploaded pay or qrpay template is untrusted markup. It is
// tokenized and written back from scratch: only allowlisted elements and
// attributes are emitted, every text and attribute value is escaped, every
// attribute is quoted and every element that was opened is closed. The
// browser therefore only ever parses this package's own, unambiguous output,
// whatever the input looked like. Images and style sheets come from the
// package only, and the page CSP (no template script source at all; styles,
// fonts and images from this site only) stays the second line of defence.

const maxCustomDepth = 128

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
	// Not reported: the wrapper of a full document, and <main>, which the
	// system writes itself around the checkout block.
	customQuiet = map[string]bool{"html": true, "head": true, "body": true, "main": true}
	// The elements {{content}} may sit in.
	contentParents = map[string]bool{"div": true, "section": true, "article": true, "aside": true, "header": true, "footer": true}
)

var (
	customID       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_:.-]{0,63}$`)
	customClass    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	customNumber   = regexp.MustCompile(`^[0-9]{1,4}$`)
	customInteger  = regexp.MustCompile(`^-?[0-9]{1,6}$`)
	customLang     = regexp.MustCompile(`^[A-Za-z]{1,8}(-[A-Za-z0-9]{1,8}){0,4}$`)
	customAttrName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	customARIA     = regexp.MustCompile(`^aria-[a-z]{2,32}$`)
	customRole     = regexp.MustCompile(`^[a-z]{2,32}$`)
)

// payContext is what sanitizing a pay or qrpay index.html needs and finds.
type payContext struct {
	kind, key string
	files     map[string]bool
	warn      *warnList
	links     []string
	linked    map[string]bool
	css       strings.Builder
	bodyClass string
	bodySeen  bool
	contents  int
	misplaced string
	err       error
}

func (c *payContext) fail(e error) {
	if c.err == nil {
		c.err = e
	}
}

type customSanitizer struct {
	src      string
	pos      int
	out      strings.Builder
	stack    []string
	skipName string
	skip     int
	removed  map[string]int
	pkg      *payContext
}

// sanitizeMarkup returns the markup that is served for src, and what was
// dropped from it.
func sanitizeMarkup(src string, c *payContext) (string, map[string]int) {
	if c.linked == nil {
		c.linked = map[string]bool{}
	}
	p := &customSanitizer{src: stripSentinels(src), removed: map[string]int{}, pkg: c}
	p.run()
	return p.out.String(), p.removed
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
			p.pos = commentEnd(s, p.pos)
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

func (p *customSanitizer) text(raw string) {
	if p.skip > 0 || raw == "" {
		return
	}
	escaped := html.EscapeString(stripSentinels(html.UnescapeString(raw)))
	if !strings.Contains(escaped, "{{") {
		p.out.WriteString(escaped)
		return
	}
	p.out.WriteString(tokenPattern.ReplaceAllStringFunc(escaped, func(m string) string {
		name := tokenPattern.FindStringSubmatch(m)[1]
		if name == "content" {
			p.pkg.contents++
			for _, open := range p.stack {
				if !contentParents[open] && p.pkg.misplaced == "" {
					p.pkg.misplaced = "{{content}} 不能放在 <" + open + "> 里，请放在 body、div、section、article、aside、header 或 footer 里"
				}
			}
			return "\x01#\x02"
		}
		return p.token(name, m)
	}))
}

// token is the sentinel of a known placeholder, or the text as written.
func (p *customSanitizer) token(name, written string) string {
	if known, _ := tokenKnown(p.pkg.kind, name); !known {
		p.pkg.warn.add("不认识的占位符 {{%s}}，会原样显示", name)
		return written
	}
	return "\x01" + name + "\x02"
}

// attributeTokens replaces placeholders in a plain attribute value.
func (p *customSanitizer) attributeTokens(v string) string {
	if !strings.Contains(v, "{{") {
		return v
	}
	return tokenPattern.ReplaceAllStringFunc(v, func(m string) string {
		name := tokenPattern.FindStringSubmatch(m)[1]
		if name == "content" {
			if p.pkg.misplaced == "" {
				p.pkg.misplaced = "{{content}} 只能写在文字里，不能写在属性里"
			}
			return m
		}
		return p.token(name, m)
	})
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
		attr.value = stripSentinels(decodeAttribute(attr.value))
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
	case name == "style":
		p.style(p.skipRawText(name))
	case name == "title":
		p.skipRawText(name) // the system writes the page title
	case name == "link":
		p.link(attrs)
	case name == "meta":
	case name == "body":
		p.body(attrs)
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

// skipRawText moves past the raw text of element name and returns it.
func (p *customSanitizer) skipRawText(name string) string {
	start := p.pos
	contentEnd, next := rawTextEnd(p.src, p.pos, name)
	p.pos = next
	return p.src[start:contentEnd]
}

// style moves a <style> block of the template into the page head.
func (p *customSanitizer) style(css string) {
	clean, e := checkPackageCSS(css, "", "index.html 的 <style>", p.pkg.key, p.pkg.files, true, p.pkg.warn)
	if e != nil {
		p.pkg.fail(e)
		return
	}
	p.pkg.css.WriteString(clean)
	p.pkg.css.WriteString("\n")
}

// link keeps a style sheet of the package, written in the page head.
func (p *customSanitizer) link(attrs []customAttr) {
	var rel, href string
	seen := map[string]bool{}
	for _, a := range attrs {
		if seen[a.name] {
			continue
		}
		seen[a.name] = true
		switch a.name {
		case "rel":
			rel = strings.ToLower(a.value)
		case "href":
			href = strings.TrimSpace(a.value)
		}
	}
	if !strings.Contains(" "+strings.Join(strings.Fields(rel), " ")+" ", " stylesheet ") {
		return // icons, preloads and the like do nothing on a checkout page
	}
	if externalRef(href) {
		p.pkg.fail(refuse("index.html 引用了外部样式 %s；支付页和收款页模板只能用模板包里的文件", clip(shown(href), 60)))
		return
	}
	file, _, ok := resolvePackagePath("", href)
	switch {
	case !ok || file == "" || !p.pkg.files[file]:
		p.pkg.warn.add("index.html 找不到样式文件：%s", clip(shown(href), 60))
	case extension(file) != "css":
		p.pkg.warn.add("index.html 的样式不是 .css 文件，没有使用：%s", clip(shown(href), 60))
	case !p.pkg.linked[file]:
		p.pkg.linked[file] = true
		p.pkg.links = append(p.pkg.links, fileURL(p.pkg.key, file))
	}
}

// body keeps the classes of the template's <body> for the system's own.
func (p *customSanitizer) body(attrs []customAttr) {
	if p.pkg.bodySeen {
		return
	}
	p.pkg.bodySeen = true
	for _, a := range attrs {
		if a.name != "class" {
			continue
		}
		var tokens []string
		for _, t := range strings.Fields(a.value) {
			if customClass.MatchString(t) && len(tokens) < 32 {
				tokens = append(tokens, t)
			}
		}
		p.pkg.bodyClass = strings.Join(tokens, " ")
		return
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
		p.drop("图片（只能用模板包里的图片）")
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
	refuseValue := func() (string, bool) {
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
			return refuseValue()
		}
		return v, true
	case name == "title" || name == "alt" && element == "img" || name == "abbr" && (element == "th" || element == "td") || customARIA.MatchString(name):
		return p.attributeTokens(clip(v, 512)), true
	case name == "role":
		if !customRole.MatchString(v) {
			return refuseValue()
		}
		return v, true
	case name == "lang":
		if !customLang.MatchString(v) {
			return refuseValue()
		}
		return v, true
	case name == "dir":
		if v != "ltr" && v != "rtl" && v != "auto" {
			return refuseValue()
		}
		return v, true
	case name == "hidden" || name == "open" && element == "details" || name == "reversed" && element == "ol":
		return "\x00bool", true
	case name == "style":
		if strings.ContainsAny(v, "{}") {
			p.drop("style（含不允许的样式）")
			return "", false
		}
		css, e := checkPackageCSS(v, "", "index.html 的 style 属性", p.pkg.key, p.pkg.files, true, p.pkg.warn)
		if e != nil {
			p.pkg.fail(e)
			return "", false
		}
		return css, true
	case name == "href" && element == "a" || name == "cite" && (element == "blockquote" || element == "q" || element == "del" || element == "ins"):
		if token, ok := wholeURLToken(p.pkg.kind, v); ok && name == "href" {
			return "\x01@" + token + "\x02", true
		}
		if !customURL(v) {
			p.drop(name + "（不安全的地址）")
			return "", false
		}
		return strings.TrimSpace(v), true
	case name == "target" && element == "a":
		if v != "_blank" && v != "_self" {
			return refuseValue()
		}
		return v, true
	case name == "rel" && element == "a":
		return "", true
	case name == "src" && element == "img":
		if token, ok := wholeURLToken(p.pkg.kind, v); ok {
			return "\x01@" + token + "\x02", true
		}
		file, rest, ok := resolvePackagePath("", v)
		if !ok {
			return "", false
		}
		if file == "" || !p.pkg.files[file] || !isImage(file) {
			p.pkg.warn.add("index.html 找不到图片：%s", clip(shown(v), 60))
			return "", false
		}
		return fileURL(p.pkg.key, file) + rest, true
	case (name == "width" || name == "height") && element == "img", (name == "colspan" || name == "rowspan") && (element == "td" || element == "th"), name == "span" && (element == "col" || element == "colgroup"):
		if !customNumber.MatchString(v) {
			return refuseValue()
		}
		return v, true
	case name == "loading" && element == "img":
		if v != "lazy" && v != "eager" {
			return refuseValue()
		}
		return v, true
	case name == "decoding" && element == "img":
		if v != "async" && v != "sync" && v != "auto" {
			return refuseValue()
		}
		return v, true
	case name == "scope" && (element == "th" || element == "td"):
		if v != "row" && v != "col" && v != "rowgroup" && v != "colgroup" {
			return refuseValue()
		}
		return v, true
	case name == "headers" && (element == "th" || element == "td"):
		return clip(v, 256), true
	case name == "start" && element == "ol", name == "value" && element == "li":
		if !customInteger.MatchString(v) {
			return refuseValue()
		}
		return v, true
	case name == "type" && element == "ol":
		if v != "1" && v != "a" && v != "A" && v != "i" && v != "I" {
			return refuseValue()
		}
		return v, true
	case name == "datetime" && (element == "time" || element == "del" || element == "ins"), name == "value" && element == "data":
		return clip(v, 128), true
	case name == "translate":
		if v != "yes" && v != "no" {
			return refuseValue()
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
