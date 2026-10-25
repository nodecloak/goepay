package payment

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Independently enforce the approved official URLLink response grammar. This
// only validates a returned shortlink; it never follows or fetches the URL.
func profileIdentityLinkURL(raw string) bool {
	if len(raw) == 0 || len(raw) > 2048 || !utf8.ValidString(raw) || !strings.HasPrefix(raw, "https://") || strings.ContainsAny(raw, "\\#") {
		return false
	}
	for _, c := range raw {
		if unicode.IsSpace(c) || unicode.IsControl(c) {
			return false
		}
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.ForceQuery || (u.Host != "wxaurl.cn" && u.Host != "wxmpurl.cn") {
		return false
	}
	if len(u.Path) < 2 || len(u.Path) > 1024 || !strings.HasPrefix(u.Path, "/") || strings.Trim(u.Path, "/") == "" {
		return false
	}
	for _, c := range u.Path {
		if unicode.IsSpace(c) || unicode.IsControl(c) || c == '\\' {
			return false
		}
	}
	for _, p := range strings.Split(u.Path, "/") {
		if p == "." || p == ".." {
			return false
		}
	}
	if u.RawQuery != "" {
		q, e := url.ParseQuery(u.RawQuery)
		if e != nil || len(q) != 1 || len(q["cq"]) != 1 {
			return false
		}
		v := q.Get("cq")
		if len(v) < 1 || len(v) > 256 {
			return false
		}
		for _, c := range v {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$&'()*+,/:;=?@-._~%`", c)) {
				return false
			}
		}
	}
	return true
}
