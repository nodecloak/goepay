package backoffice

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// Original admin/set.php mod=proxy 中转代理设置. The async-notify proxy. The
// two secrets proxy_pwd / proxy_apikey are NOT plaintext settings here; they
// live in an encrypted store. These are the non-secret dimensions.
func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options,
				Option{Label: "中转代理开关 proxy（0关闭/1代理服务器/2代理API）", Value: "proxy"},
				Option{Label: "代理IP proxy_server", Value: "proxy_server"},
				Option{Label: "代理端口 proxy_port", Value: "proxy_port"},
				Option{Label: "代理账号 proxy_user", Value: "proxy_user"},
				Option{Label: "代理协议 proxy_type", Value: "proxy_type"},
				Option{Label: "代理API接口地址 proxy_apiurl", Value: "proxy_apiurl"},
			)
		}
	}
	definitions["settings"] = d
}

func validateProxySetting(key, value string) (bool, error) {
	switch key {
	case "proxy":
		if value != "0" && value != "1" && value != "2" {
			return true, ErrInvalid
		}
	case "proxy_type":
		switch value {
		case "http", "https", "sock4", "sock5", "sock5h":
		default:
			return true, ErrInvalid
		}
	case "proxy_port":
		n := 0
		for _, c := range value {
			if c < '0' || c > '9' {
				return true, ErrInvalid
			}
			n = n*10 + int(c-'0')
		}
		if value == "" || n < 1 || n > 65535 {
			return true, ErrInvalid
		}
	case "proxy_server", "proxy_user":
		if value == "" || len(value) > 255 || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n\t ") {
			return true, ErrInvalid
		}
	case "proxy_apiurl":
		u, e := url.Parse(value)
		if e != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.Fragment != "" || len(value) > 2048 {
			return true, ErrInvalid
		}
	default:
		return false, nil
	}
	return true, nil
}
