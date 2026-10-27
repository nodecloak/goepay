package server

import (
	"fmt"
	"goepay/internal/siteinfo"
	"html"
	"html/template"
	"net/http"
	"net/url"
)

func (s *Server) publicSite191(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" {
		w.Header().Set("Allow", "GET")
		response(w, 405, -1, "method not allowed", nil)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "site query parameters not accepted", nil)
		return
	}
	if s.Pool == nil {
		response(w, 503, -1, "website settings unavailable", nil)
		return
	}
	c, err := siteinfo.Load(r.Context(), s.Pool)
	if err != nil {
		response(w, 503, -1, "website settings unavailable", nil)
		return
	}
	response(w, 200, 0, "", c)
}

func siteHomepage191(w http.ResponseWriter, r *http.Request, c siteinfo.Config) bool {
	switch c.Homepage {
	case "1":
		http.Redirect(w, r, "/merchant/dashboard", http.StatusFound)
		return true
	case "2":
		u, _ := url.Parse(c.HomepageURL)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-src "+u.Scheme+"://"+u.Host+"; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
		fmt.Fprintf(w, `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title><style>html,body,iframe{width:100%%;height:100%%;margin:0;border:0;display:block}</style><iframe title="网站首页" src="%s" sandbox="allow-scripts allow-forms" referrerpolicy="no-referrer"></iframe></html>`, html.EscapeString(c.Title), html.EscapeString(c.HomepageURL))
		return true
	case "3":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(200)
		return true
	}
	return false
}

var siteContacts191 = template.Must(template.New("site-contacts").Parse(`{{if .Organisation}}<p>{{.Organisation}}</p>{{end}}<nav aria-label="联系网站">{{if .Email}}<a href="mailto:{{.Email}}">{{.Email}}</a> {{end}}{{if .QQURL}}<a href="{{.QQURL}}" target="_blank" rel="noopener noreferrer">客服QQ{{if .QQ}}：{{.QQ}}{{end}}</a> {{else}}{{if .QQ}}<span>客服QQ：{{.QQ}}</span>{{end}}{{end}}{{if .WeWorkURL}}<a href="{{.WeWorkURL}}" target="_blank" rel="noopener noreferrer">企业微信</a> {{end}}{{if .QQGroupURL}}<a href="{{.QQGroupURL}}" target="_blank" rel="noopener noreferrer">加入QQ群</a>{{end}}</nav>`))
