package pagetemplate

import (
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// Compiling an uploaded version turns its index.html into the markup that is
// served, with placeholders replaced by sentinels that are filled in, escaped,
// at render time:
//
//	\x01name\x02   a placeholder in text or a plain attribute (HTML-escaped)
//	\x01@name\x02  a *_url placeholder that is a whole URL attribute (checked)
//	\x01#\x02      {{content}}, where pay and qrpay pages put the checkout block
//	\x01^\x02      the address of the page itself (home in-page links)
//	\x01!\x02      the sandbox or preview notice bar (home)
//	\x01$\x02      the --goepay-* colour variables (home)
//
// Source text never contains \x00, \x01 or \x02: they are removed first.

var tokenPattern = regexp.MustCompile(`\{\{\s*([a-z_]+)\s*\}\}`)

var (
	homeTokens = map[string]bool{"site_name": true, "title": true, "keywords": true, "description": true, "org": true, "email": true, "qq": true, "notice": true, "brand_name": true, "year": true, "accent_color": true, "background_color": true, "registration_open": true, "test_open": true}
	payTokens  = map[string]bool{"title": true, "amount": true, "trade_no": true, "order_name": true, "merchant_name": true, "brand_name": true, "year": true}
	homeURLs   = map[string]bool{"logo_url": true, "qq_url": true, "qq_group_url": true, "wework_url": true, "email_url": true, "login_url": true, "register_url": true, "test_url": true, "doc_url": true, "agreement_url": true, "home_url": true}
	payURLs    = map[string]bool{"logo_url": true}
)

func tokenKnown(kind, name string) (known, isURL bool) {
	if kind == "home" {
		return homeTokens[name] || homeURLs[name], homeURLs[name]
	}
	return payTokens[name] || payURLs[name] || name == "content", payURLs[name]
}

func stripSentinels(s string) string {
	return strings.NewReplacer("\x00", "", "\x01", "", "\x02", "").Replace(s)
}

// warnList collects at most 20 warnings and counts the rest.
type warnList struct {
	items []string
	more  int
	seen  map[string]bool
}

func (w *warnList) add(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	if w.seen == nil {
		w.seen = map[string]bool{}
	}
	if w.seen[text] {
		return
	}
	w.seen[text] = true
	if len(w.items) >= 20 {
		w.more++
		return
	}
	w.items = append(w.items, text)
}

func (w *warnList) list() []string {
	out := append([]string{}, w.items...)
	if w.more > 0 {
		out = append(out, fmt.Sprintf("还有 %d 条提示未列出", w.more))
	}
	return out
}

// replaceTokens turns the known placeholders of kind in text into sentinels;
// unknown ones stay as they are, with a warning.
func replaceTokens(kind, text string, warn *warnList) string {
	if !strings.Contains(text, "{{") {
		return text
	}
	return tokenPattern.ReplaceAllStringFunc(text, func(m string) string {
		name := tokenPattern.FindStringSubmatch(m)[1]
		if known, _ := tokenKnown(kind, name); !known || name == "content" {
			if !known {
				warn.add("不认识的占位符 {{%s}}，会原样显示", name)
			}
			return m
		}
		return "\x01" + name + "\x02"
	})
}

// wholeURLToken returns the name when value is exactly one *_url placeholder.
func wholeURLToken(kind, value string) (string, bool) {
	m := tokenPattern.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil || m[0] != strings.TrimSpace(value) {
		return "", false
	}
	known, isURL := tokenKnown(kind, m[1])
	return m[1], known && isURL
}

// safeURL is the value written for a URL placeholder: site paths, http(s),
// mailto and template image data; anything else becomes a link to the page.
func safeURL(v, page string) string {
	v = strings.TrimSpace(v)
	for _, r := range v {
		if r < 0x20 || r == 0x7f || r == ' ' {
			return page + "#"
		}
	}
	lower := strings.ToLower(v)
	switch {
	case strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "//"),
		strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "mailto:"),
		strings.HasPrefix(lower, "data:image/png;base64,"), strings.HasPrefix(lower, "data:image/jpeg;base64,"):
		return v
	}
	return page + "#"
}

// expand fills in the sentinels of compiled markup.
func expand(s string, values map[string]string, page, bar, vars string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '\x01')
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		j := strings.IndexByte(s[i:], '\x02')
		if j < 0 {
			return b.String()
		}
		name := s[i+1 : i+j]
		s = s[i+j+1:]
		switch {
		case name == "^":
			b.WriteString(html.EscapeString(page))
		case name == "!":
			b.WriteString(bar)
		case name == "$":
			b.WriteString(vars)
		case name == "#":
		case strings.HasPrefix(name, "@"):
			b.WriteString(html.EscapeString(safeURL(values[name[1:]], page)))
		default:
			b.WriteString(html.EscapeString(values[name]))
		}
	}
}

// resolvePackagePath resolves a relative reference from a package file in dir
// to a package path; ok is false for references that are not relative paths.
// rest is the query and fragment, kept as written.
func resolvePackagePath(dir, ref string) (p, rest string, ok bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "#") || strings.HasPrefix(ref, "?") || strings.ContainsAny(ref, "\x01\\") {
		return "", "", false
	}
	if colon := strings.IndexByte(ref, ':'); colon >= 0 {
		if cut := strings.IndexAny(ref, "/?#"); cut < 0 || cut > colon {
			return "", "", false // a scheme
		}
	}
	if cut := strings.IndexAny(ref, "?#"); cut >= 0 {
		ref, rest = ref[:cut], ref[cut:]
	}
	decoded, e := url.PathUnescape(ref)
	if e != nil {
		return "", rest, true
	}
	// A reference that climbs out of the package names no package file.
	p = path.Join(dir, decoded)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") {
		return "", rest, true
	}
	return p, rest, true
}

func fileURL(key, p string) string { return "/template-files/" + key + "/" + p }

// External reports whether a CSS or HTML reference leaves the package: a
// scheme other than data:, a protocol-relative or a site path.
func externalRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	lower := strings.ToLower(ref)
	if ref == "" || strings.HasPrefix(ref, "#") || strings.HasPrefix(lower, "data:") {
		return false
	}
	if strings.HasPrefix(ref, "/") {
		return true
	}
	if colon := strings.IndexByte(ref, ':'); colon >= 0 {
		if cut := strings.IndexAny(ref, "/?#"); cut < 0 || cut > colon {
			return true
		}
	}
	return false
}

// cssRef is one url() or @import in a style sheet; start and end delimit the
// address itself.
type cssRef struct {
	start, end int
	value      string
	escaped    bool
	isImport   bool
}

func cssIdentChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c >= 0x80 || c == '\\'
}

// scanCSS lists the url() addresses and @import rules of css, skipping
// comments and strings.
func scanCSS(css string) []cssRef {
	var refs []cssRef
	n := len(css)
	readString := func(i int) int { // i at the quote; returns the index after it
		q := css[i]
		for i++; i < n; i++ {
			switch css[i] {
			case '\\':
				i++
			case q, '\n':
				return i + 1
			}
		}
		return n
	}
	for i := 0; i < n; {
		c := css[i]
		switch {
		case c == '/' && i+1 < n && css[i+1] == '*':
			end := strings.Index(css[i+2:], "*/")
			if end < 0 {
				return refs
			}
			i += end + 4
		case c == '"' || c == '\'':
			i = readString(i)
		case c == '@' && i+7 <= n && strings.EqualFold(css[i:i+7], "@import") && (i+7 == n || !cssIdentChar(css[i+7])):
			refs = append(refs, cssRef{start: i, end: i + 7, isImport: true})
			i += 7
		case (c == 'u' || c == 'U') && i+4 <= n && strings.EqualFold(css[i:i+4], "url(") && (i == 0 || !cssIdentChar(css[i-1])):
			j := i + 4
			for j < n && htmlSpace(css[j]) {
				j++
			}
			if j < n && (css[j] == '"' || css[j] == '\'') {
				end := readString(j)
				value := css[j+1 : max(j+1, end-1)]
				refs = append(refs, cssRef{start: j + 1, end: max(j+1, end-1), value: value, escaped: strings.Contains(value, `\`)})
				i = end
				continue
			}
			k := j
			for k < n && css[k] != ')' && !htmlSpace(css[k]) {
				k++
			}
			value := css[j:k]
			refs = append(refs, cssRef{start: j, end: k, value: value, escaped: strings.Contains(value, `\`)})
			i = k
		default:
			i++
		}
	}
	return refs
}

var legacyCSSTokens = []string{"expression(", "javascript:", "vbscript:", "behavior:", "-moz-binding"}

// checkPackageCSS applies the rules of pay and qrpay templates to a style
// sheet in dir (inline styles use ""): no @import, no legacy script hooks,
// only package files or data: in url(). With rewrite, package references are
// rewritten to their file addresses (inline styles resolve against the page,
// not the package). Missing files are warnings.
func checkPackageCSS(css, dir, where, key string, files map[string]bool, rewrite bool, warn *warnList) (string, error) {
	plain := strings.ToLower(customCSSComment.ReplaceAllString(css, ""))
	compact := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			return -1
		}
		return r
	}, plain)
	for _, token := range legacyCSSTokens {
		if strings.Contains(compact, token) {
			return "", refuse("%s 的 CSS 不能使用 %s", where, strings.TrimSuffix(token, "("))
		}
	}
	var b strings.Builder
	last := 0
	for _, ref := range scanCSS(css) {
		if ref.isImport {
			return "", refuse("支付页和收款页模板的 CSS 不能用 @import，请在 index.html 里多写一个 <link>（%s）", where)
		}
		if ref.escaped {
			return "", refuse("%s 的 CSS 地址不能含反斜杠：%s", where, clip(shown(ref.value), 60))
		}
		if externalRef(ref.value) {
			return "", refuse("%s 的 CSS 引用了外部地址 %s；支付页和收款页模板只能用模板包里的文件", where, clip(shown(ref.value), 60))
		}
		p, rest, ok := resolvePackagePath(dir, ref.value)
		if !ok {
			continue
		}
		if !files[p] {
			warn.add("%s 找不到文件：%s", where, clip(shown(ref.value), 60))
			continue
		}
		if rewrite {
			b.WriteString(css[last:ref.start])
			b.WriteString(fileURL(key, p) + rest)
			last = ref.end
		}
	}
	b.WriteString(css[last:])
	return b.String(), nil
}

// warnMissingCSS reports url() references of a home style sheet that are
// relative but not in the package.
func warnMissingCSS(css, dir, where string, files map[string]bool, warn *warnList) {
	for _, ref := range scanCSS(css) {
		if ref.isImport || ref.escaped {
			continue
		}
		if p, _, ok := resolvePackagePath(dir, ref.value); ok && !files[p] {
			warn.add("%s 找不到文件：%s", where, clip(shown(ref.value), 60))
		}
	}
}

// attrSpan is one attribute of a start tag, as byte offsets into the source.
type attrSpan struct {
	name                 string
	nameStart, nameEnd   int
	valueStart, valueEnd int // -1 when the attribute has no value
	quote                byte
	end                  int
}

// scanTag reads the tag at s[pos] ('<' or '</'). ok is false when the input
// ends inside the tag.
func scanTag(s string, pos int, endTag bool) (name string, attrs []attrSpan, next int, ok bool) {
	i := pos + 1
	if endTag {
		i++
	}
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
			return name, attrs, len(s), false
		}
		if s[i] == '>' {
			return name, attrs, i + 1, true
		}
		a := attrSpan{nameStart: i, valueStart: -1, valueEnd: -1}
		i++ // a leading '=' belongs to the name
		for i < len(s) && !htmlSpace(s[i]) && s[i] != '/' && s[i] != '>' && s[i] != '=' {
			i++
		}
		a.nameEnd = i
		a.name = strings.ToLower(s[a.nameStart:a.nameEnd])
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
				return name, attrs, len(s), false
			}
			if q := s[j]; q == '"' || q == '\'' {
				end := strings.IndexByte(s[j+1:], q)
				if end < 0 {
					return name, attrs, len(s), false
				}
				a.quote, a.valueStart, a.valueEnd = q, j+1, j+1+end
				j += end + 2
			} else {
				a.valueStart = j
				for j < len(s) && !htmlSpace(s[j]) && s[j] != '>' {
					j++
				}
				a.valueEnd = j
			}
			i = j
		}
		a.end = i
		attrs = append(attrs, a)
	}
}

// rawTextEnd returns where the raw text of element name that starts at pos
// ends (the start of its end tag, or the end of s) and where the end tag ends.
func rawTextEnd(s string, pos int, name string) (contentEnd, next int) {
	for i := pos; ; {
		j := strings.Index(s[i:], "</")
		if j < 0 {
			return len(s), len(s)
		}
		at := i + j + 2
		if len(s)-at >= len(name) && strings.EqualFold(s[at:at+len(name)], name) && (at+len(name) == len(s) || htmlSpace(s[at+len(name)]) || s[at+len(name)] == '/' || s[at+len(name)] == '>') {
			end := strings.IndexByte(s[at:], '>')
			if end < 0 {
				return i + j, len(s)
			}
			return i + j, at + end + 1
		}
		i = at
	}
}

func commentEnd(s string, pos int) int {
	rest := s[pos:]
	if strings.HasPrefix(rest, "<!-->") {
		return pos + 5
	}
	if strings.HasPrefix(rest, "<!--->") {
		return pos + 6
	}
	end, size := -1, 0
	if i := strings.Index(rest[4:], "-->"); i >= 0 {
		end, size = i, 3
	}
	if i := strings.Index(rest[4:], "--!>"); i >= 0 && (end < 0 || i < end) {
		end, size = i, 4
	}
	if end < 0 {
		return len(s)
	}
	return pos + 4 + end + size
}

var (
	homeRawText = map[string]bool{"script": true, "style": true, "xmp": true, "iframe": true, "noembed": true, "noframes": true, "noscript": true}
	homeRCDATA  = map[string]bool{"title": true, "textarea": true}
	urlAttrs    = map[string]bool{"href": true, "src": true, "action": true, "formaction": true, "poster": true, "data": true, "cite": true, "background": true, "xlink:href": true, "srcset": true, "longdesc": true, "manifest": true, "ping": true}
)

type homeCompiler struct {
	src   string
	key   string
	files map[string]bool
	warn  *warnList
	out   strings.Builder
}

// compileHome compiles the index.html of a home package. The page is the
// administrator's own and may run scripts, so it is kept byte for byte except
// for the edits listed in the spec: a <base> for the package files, the
// colour variables, the notice bar, in-page links and placeholders.
func compileHome(key string, index string, files map[string]bool, warn *warnList) string {
	c := &homeCompiler{src: stripSentinels(strings.TrimPrefix(index, "\ufeff")), key: key, files: files, warn: warn}
	insertAt, bodyAt := c.landmarks()
	inserted := false
	s := c.src
	for pos := 0; pos < len(s); {
		if !inserted && pos >= insertAt {
			c.out.WriteString(`<base href="/template-files/` + key + `/">` + "\x01$\x02")
			inserted = true
		}
		i := strings.IndexByte(s[pos:], '<')
		if i < 0 {
			c.text(s[pos:])
			break
		}
		if i > 0 {
			stop := pos + i
			if !inserted && insertAt > pos && insertAt < stop {
				stop = insertAt
			}
			c.text(s[pos:stop])
			pos = stop
			continue
		}
		rest := s[pos:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			end := commentEnd(s, pos)
			c.out.WriteString(s[pos:end])
			pos = end
		case len(rest) > 1 && (rest[1] == '!' || rest[1] == '?'):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				end = len(rest) - 1
			}
			c.out.WriteString(rest[:end+1])
			pos += end + 1
		case len(rest) > 2 && rest[1] == '/' && asciiAlpha(rest[2]):
			_, _, next, _ := scanTag(s, pos, true)
			c.out.WriteString(s[pos:next])
			pos = next
		case len(rest) > 1 && asciiAlpha(rest[1]):
			name, attrs, next, ok := scanTag(s, pos, false)
			if !ok {
				c.out.WriteString(s[pos:])
				pos = len(s)
				continue
			}
			if name == "base" {
				c.warn.add("去掉了模板里的 <base>：系统会自动处理模板文件的相对路径")
			} else {
				c.tag(name, pos, attrs, next)
			}
			pos = next
			if !inserted && pos >= insertAt {
				c.out.WriteString(`<base href="/template-files/` + key + `/">` + "\x01$\x02")
				inserted = true
			}
			if pos == bodyAt {
				c.out.WriteString("\x01!\x02")
			}
			switch {
			case name == "plaintext":
				c.out.WriteString(s[pos:])
				pos = len(s)
			case homeRawText[name]:
				contentEnd, end := rawTextEnd(s, pos, name)
				c.out.WriteString(s[pos:end])
				if name == "style" {
					warnMissingCSS(s[pos:contentEnd], "", "index.html 的 <style>", c.files, c.warn)
				}
				pos = end
			case homeRCDATA[name]:
				contentEnd, end := rawTextEnd(s, pos, name)
				c.text(s[pos:contentEnd])
				c.out.WriteString(s[contentEnd:end])
				pos = end
			}
		default:
			c.out.WriteString("<")
			pos++
		}
	}
	if !inserted {
		c.out.WriteString(`<base href="/template-files/` + key + `/">` + "\x01$\x02")
	}
	return c.out.String()
}

// landmarks finds where the <base> goes (after the first <head> start tag,
// else after <html>, else after a leading doctype) and where the first
// <body> start tag ends (-1 without one).
func (c *homeCompiler) landmarks() (insertAt, bodyAt int) {
	s := c.src
	head, htmlTag, doctype := -1, -1, 0
	bodyAt = -1
	if t := strings.TrimLeft(s, " \t\r\n\f"); len(t) > 9 && strings.EqualFold(t[:9], "<!doctype") {
		if end := strings.IndexByte(t, '>'); end >= 0 {
			doctype = len(s) - len(t) + end + 1
		}
	}
	for pos := 0; pos < len(s); {
		i := strings.IndexByte(s[pos:], '<')
		if i < 0 {
			break
		}
		pos += i
		rest := s[pos:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			pos = commentEnd(s, pos)
		case len(rest) > 1 && asciiAlpha(rest[1]):
			name, _, next, ok := scanTag(s, pos, false)
			if !ok {
				pos = len(s)
				continue
			}
			switch {
			case name == "head" && head < 0:
				head = next
			case name == "html" && htmlTag < 0:
				htmlTag = next
			case name == "body" && bodyAt < 0:
				bodyAt = next
			}
			pos = next
			if homeRawText[name] || homeRCDATA[name] {
				_, pos = rawTextEnd(s, pos, name)
			} else if name == "plaintext" {
				pos = len(s)
			}
		default:
			pos++
		}
	}
	switch {
	case head >= 0:
		return head, bodyAt
	case htmlTag >= 0:
		return htmlTag, bodyAt
	}
	return doctype, bodyAt
}

func (c *homeCompiler) text(raw string) {
	c.out.WriteString(replaceTokens("home", raw, c.warn))
}

// inPageLink returns the fragment of an in-page link of the home page itself
// ("", "#x", "./", "index.html#x"); ok is false for other links.
func inPageLink(v string) (fragment string, ok bool) {
	v = strings.Trim(v, " \t\n\r\f")
	page, fragment, _ := strings.Cut(v, "#")
	if fragment != "" || strings.Contains(v, "#") {
		fragment = "#" + fragment
	}
	switch page {
	case "", ".", "./", "index.html", "./index.html", "index.htm", "./index.htm":
		return fragment, true
	}
	return "", false
}

func (c *homeCompiler) tag(name string, pos int, attrs []attrSpan, next int) {
	s := c.src
	type edit struct {
		i     int
		value string
	}
	var edits []edit
	for i, a := range attrs {
		if a.valueStart < 0 {
			if (name == "a" || name == "area") && a.name == "href" {
				edits = append(edits, edit{i, "\x01^\x02"})
			}
			continue
		}
		raw := s[a.valueStart:a.valueEnd]
		if a.quote != '"' {
			raw = strings.ReplaceAll(raw, `"`, "&quot;")
		}
		hasToken := tokenPattern.MatchString(raw)
		switch {
		case (name == "a" || name == "area") && a.name == "href" && !hasToken:
			if fragment, ok := inPageLink(raw); ok {
				edits = append(edits, edit{i, "\x01^\x02" + fragment})
			}
		case !hasToken:
			if a.name == "src" || a.name == "poster" || name == "link" && a.name == "href" {
				if p, _, ok := resolvePackagePath("", html.UnescapeString(raw)); ok && !c.files[p] {
					c.warn.add("index.html 找不到文件：%s", clip(shown(html.UnescapeString(raw)), 60))
				}
			}
		case strings.HasPrefix(a.name, "on") || a.name == "style":
			c.warn.add("占位符不能用在 %s 属性里，会原样显示", clip(a.name, 32))
		case urlAttrs[a.name]:
			if token, ok := wholeURLToken("home", raw); ok {
				edits = append(edits, edit{i, "\x01@" + token + "\x02"})
			} else {
				c.warn.add("%s 属性里只能单独写一个 *_url 占位符，例如 href=\"{{login_url}}\"", a.name)
			}
		default:
			edits = append(edits, edit{i, replaceTokens("home", raw, c.warn)})
		}
	}
	if len(edits) == 0 {
		c.out.WriteString(s[pos:next])
		return
	}
	at := pos
	for _, e := range edits {
		a := attrs[e.i]
		c.out.WriteString(s[at:a.nameStart])
		c.out.WriteString(s[a.nameStart:a.nameEnd] + `="` + e.value + `"`)
		at = a.end
	}
	c.out.WriteString(s[at:next])
}
