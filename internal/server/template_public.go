package server

import (
	"bytes"
	"context"
	"errors"
	"goepay/internal/pagetemplate"
	"goepay/internal/sitebrand193"
	"goepay/internal/siteinfo"
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
	cfg, err := siteinfo.Load(r.Context(), s.Pool)
	if err != nil {
		http.Error(w, "website settings unavailable", 503)
		return true
	}
	if siteHomepage191(w, r, cfg) {
		return true
	}
	s.renderHome(w, r, cfg, false)
	return true
}

// agreementPublic serves the service agreement at the original epay_pro
// address /agreement.html (/agreement redirects there). It is rendered in the
// selected home template, with the contact details from the website settings.
// It is shown whatever the homepage mode, like the original page.
func (s *Server) agreementPublic(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/agreement.html" && r.URL.Path != "/agreement" {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}
	if r.URL.Path == "/agreement" {
		http.Redirect(w, r, "/agreement.html", http.StatusFound)
		return true
	}
	if s.Pool == nil {
		http.Error(w, "page appearance unavailable", http.StatusServiceUnavailable)
		return true
	}
	cfg, err := siteinfo.Load(r.Context(), s.Pool)
	if err != nil {
		http.Error(w, "website settings unavailable", 503)
		return true
	}
	s.renderHome(w, r, cfg, true)
	return true
}

// homeFlags reads whether the public test-pay entry and self-registration are
// enabled, so the landing page offers only the links that work. Read-only; a
// read error simply hides the optional links.
func (s *Server) homeFlags(ctx context.Context) (testOpen, registrationOpen bool) {
	rows, e := s.Pool.Query(ctx, `SELECT body->>'key',body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1)`, []string{"test_open", "registration_open"})
	if e != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) != nil {
			return
		}
		switch k {
		case "test_open":
			testOpen = v == "1"
		case "registration_open":
			registrationOpen = v == "true"
		}
	}
	return
}

// renderHome serves the selected built-in home landing page, or with agreement
// set the service agreement page in the same template. Operator text is
// escaped by the template, the page carries no script or form, and the
// appearance colours come from the selected template's typed configuration.
func (s *Server) renderHome(w http.ResponseWriter, r *http.Request, cfg siteinfo.Config, agreement bool) {
	appearance, e := pagetemplate.New(s.Pool).Resolve(r.Context(), "home")
	if e != nil {
		http.Error(w, "page appearance unavailable", 503)
		return
	}
	logo, e := sitebrand193.New(s.Pool, s.CheckoutKey).Public(r.Context(), "logo")
	if e != nil && !errors.Is(e, sitebrand193.ErrNotFound) {
		http.Error(w, "website branding unavailable", 503)
		return
	}
	testOpen, registrationOpen := s.homeFlags(r.Context())
	d := pagetemplate.HomeData{
		SiteName:         cfg.Name,
		Title:            cfg.Title,
		Keywords:         cfg.Keywords,
		Description:      cfg.Description,
		Org:              cfg.Organisation,
		Email:            cfg.Email,
		QQ:               cfg.QQ,
		QQURL:            cfg.QQURL,
		QQGroup:          cfg.QQGroupURL,
		WeWork:           cfg.WeWorkURL,
		TestOpen:         testOpen,
		RegistrationOpen: registrationOpen,
		Sandbox:          s.Sandbox,
	}
	if logo != nil {
		d.LogoURL = "/static/img/logo.png"
	}
	var b bytes.Buffer
	render := pagetemplate.RenderHome
	if agreement {
		render = pagetemplate.RenderAgreement
	}
	if e = render(&b, appearance, d); e != nil {
		http.Error(w, "page appearance unavailable", 503)
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; form-action 'none'; base-uri 'none'; frame-ancestors 'none'")
	if appearance.Fallback != "" {
		w.Header().Set("X-Goepay-Template-Fallback", appearance.Fallback)
		slog.Warn("page template appearance fallback", "type", "home", "reason", appearance.Fallback)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b.Bytes())
}

type templateColors struct{ Background, Foreground string }

func (s *Server) pageTemplateOpen(w http.ResponseWriter, r *http.Request, kind string, p pagetemplate.Page, colors *templateColors) bool {
	if kind == "home" && !p.Preview {
		logo, e := sitebrand193.New(s.Pool, s.CheckoutKey).Public(r.Context(), "logo")
		if e != nil && !errors.Is(e, sitebrand193.ErrNotFound) {
			http.Error(w, "website branding unavailable", 503)
			return false
		}
		p.SiteLogo = logo != nil
	}
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
