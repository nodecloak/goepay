package server

import (
	"bytes"
	"goepay/internal/pagetemplate"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

func writeTemplateAsset(w http.ResponseWriter, r *http.Request, a pagetemplate.Asset) {
	w.Header().Set("Content-Type", a.MIME)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Length", strconv.Itoa(len(a.Bytes)))
	if r.Method != "HEAD" {
		w.Write(a.Bytes)
	}
}
func (s *Server) templatePublic(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/template-assets/") {
		return false
	}
	// Negative asset responses must not outlive a later explicit publication.
	w.Header().Set("Cache-Control", "no-store")
	if s.Pool == nil {
		http.Error(w, "page appearance unavailable", http.StatusServiceUnavailable)
		return true
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "query parameters not accepted", 400)
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/template-assets/") {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "method not allowed", 405)
			return true
		}
		a, e := pagetemplate.New(s.Pool).PublishedAsset(r.Context(), strings.TrimPrefix(r.URL.Path, "/template-assets/"))
		if e != nil {
			http.NotFound(w, r)
			return true
		}
		writeTemplateAsset(w, r, a)
		return true
	}
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return true
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; form-action 'none'; base-uri 'none'; frame-ancestors 'none'")
	if !s.pageTemplateOpen(w, r, "home", pagetemplate.Page{Title: "在线支付服务", Sandbox: s.Sandbox}, nil) {
		return true
	}
	io.WriteString(w, "<p>请通过商户提供的付款链接发起支付，并核对订单金额和系统确认状态。</p></main></body></html>")
	return true
}

type templateColors struct{ Background, Foreground string }

func (s *Server) pageTemplateOpen(w http.ResponseWriter, r *http.Request, kind string, p pagetemplate.Page, colors *templateColors) bool {
	appearance, e := pagetemplate.New(s.Pool).Resolve(r.Context(), kind)
	if e != nil {
		http.Error(w, "page appearance unavailable", 503)
		return false
	}
	if colors != nil {
		if colors.Background != "" && validCollectionColor(colors.Background) {
			appearance.Config.BackgroundColor = colors.Background
		}
		if colors.Foreground != "" && validCollectionColor(colors.Foreground) {
			p.ForegroundColor = colors.Foreground
		}
	}
	var b bytes.Buffer
	if e = pagetemplate.Open(&b, appearance, p); e != nil {
		http.Error(w, "page appearance unavailable", 503)
		return false
	}
	if appearance.Fallback != "" {
		w.Header().Set("X-Goepay-Template-Fallback", appearance.Fallback)
		slog.Warn("page template appearance fallback", "type", kind, "reason", appearance.Fallback)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b.Bytes())
	return true
}
