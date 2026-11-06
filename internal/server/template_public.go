package server

import (
	"bytes"
	"context"
	"errors"
	"goepay/internal/pagetemplate"
	"goepay/internal/sitebrand"
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
	if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/template-assets/") && !strings.HasPrefix(r.URL.Path, "/template-files/") {
		return false
	}
	// Negative asset responses must not outlive a later explicit publication.
	w.Header().Set("Cache-Control", "no-store")
	if s.Pool == nil {
		http.Error(w, "page appearance unavailable", http.StatusServiceUnavailable)
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/template-files/") {
		s.templateFile(w, r)
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

// templateFile serves a file of an uploaded template version at
// /template-files/<key>/<path>. Versions never change, so files are cached
// for good; the sandboxed home page has no origin of its own, so any origin
// may read them (fonts need that), and a file opened directly never runs.
// Query strings (cache busters in templates) are ignored.
func (s *Server) templateFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	key, p, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/template-files/"), "/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, e := pagetemplate.New(s.Pool).PublicFile(r.Context(), key, p)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	// Template scripts run only in the sandboxed home page, which has no
	// origin of its own. A page of this site (the console, say) never gets
	// to run them, so an uploaded script can never act with its session.
	if strings.HasPrefix(f.MIME, "text/javascript") {
		switch r.Header.Get("Sec-Fetch-Site") {
		case "same-origin", "same-site":
			switch r.Header.Get("Sec-Fetch-Dest") {
			case "script", "worker", "sharedworker", "serviceworker", "audioworklet", "paintworklet":
				http.Error(w, "template scripts run only in the template page", http.StatusForbidden)
				return
			}
		}
	}
	etag := `"` + f.Digest + `"`
	h := w.Header()
	h.Set("Content-Type", f.MIME)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("ETag", etag)
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", strconv.Itoa(len(f.Content)))
	if r.Method != http.MethodHead {
		w.Write(f.Content)
	}
}

// widenTemplateCSP lets an uploaded pay or collection template load its own
// style sheets, fonts and data: images. No script source is ever added.
func widenTemplateCSP(csp string) string {
	add := func(fields []string, sources ...string) []string {
		out := fields[:1]
		for _, f := range fields[1:] {
			if f != "'none'" {
				out = append(out, f)
			}
		}
		for _, s := range sources {
			found := false
			for _, f := range out[1:] {
				found = found || f == s
			}
			if !found {
				out = append(out, s)
			}
		}
		return out
	}
	var parts []string
	seen := map[string]bool{}
	for _, d := range strings.Split(csp, ";") {
		fields := strings.Fields(d)
		if len(fields) == 0 {
			continue
		}
		name := strings.ToLower(fields[0])
		seen[name] = true
		switch name {
		case "style-src":
			fields = add(fields, "'self'")
		case "img-src":
			fields = add(fields, "data:")
		case "font-src":
			fields = add(fields, "'self'", "data:")
		}
		parts = append(parts, strings.Join(fields, " "))
	}
	if !seen["style-src"] {
		parts = append(parts, "style-src 'self' 'unsafe-inline'")
	}
	if !seen["img-src"] {
		parts = append(parts, "img-src 'self' data:")
	}
	if !seen["font-src"] {
		parts = append(parts, "font-src 'self' data:")
	}
	return strings.Join(parts, "; ")
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

// renderHome serves the selected home landing page, or with agreement set the
// service agreement page (in the built-in style of the selected template, or
// the default style for an uploaded one). Operator text is escaped by the
// template and the appearance colours come from the selected template's
// typed configuration. Built-in pages carry no script or form; an uploaded
// page runs its own scripts inside a CSP sandbox without the site's origin.
func (s *Server) renderHome(w http.ResponseWriter, r *http.Request, cfg siteinfo.Config, agreement bool) {
	appearance, e := pagetemplate.New(s.Pool).Resolve(r.Context(), "home")
	if e != nil {
		http.Error(w, "page appearance unavailable", 503)
		return
	}
	logo, e := sitebrand.New(s.Pool, s.CheckoutKey).Public(r.Context(), "logo")
	if e != nil && !errors.Is(e, sitebrand.ErrNotFound) {
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
	if e = render(&b, appearance, d); e != nil && appearance.Upload != nil {
		// An uploaded page that cannot be written falls back to the default.
		slog.Warn("uploaded home template render failed", "name", appearance.Name)
		appearance = pagetemplate.Resolved{Type: "home", Name: "default", Revision: appearance.Revision, Config: pagetemplate.DefaultConfig("home", "default"), Fallback: "invalid_upload"}
		b.Reset()
		e = render(&b, appearance, d)
	}
	if e != nil {
		http.Error(w, "page appearance unavailable", 503)
		return
	}
	csp := "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; form-action 'none'; base-uri 'none'; frame-ancestors 'none'"
	if appearance.Upload != nil && !agreement {
		// The administrator's uploaded page runs its own scripts, sandboxed.
		csp = homeUploadCSP("'none'")
	}
	w.Header().Set("Content-Security-Policy", csp)
	if appearance.Fallback != "" {
		w.Header().Set("X-Goepay-Template-Fallback", appearance.Fallback)
		slog.Warn("page template appearance fallback", "type", "home", "reason", appearance.Fallback)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b.Bytes())
}

type templateColors struct{ Background, Foreground string }

// pageTemplateOpen writes a pay or qrpay page up to its content and returns
// the closing the handler writes after the content; ok is false when an
// error page was written instead. With an uploaded template it widens the
// handler's CSP for the template's own style sheets, fonts and images.
func (s *Server) pageTemplateOpen(w http.ResponseWriter, r *http.Request, kind string, p pagetemplate.Page, colors *templateColors) (string, bool) {
	if kind == "home" && !p.Preview {
		logo, e := sitebrand.New(s.Pool, s.CheckoutKey).Public(r.Context(), "logo")
		if e != nil && !errors.Is(e, sitebrand.ErrNotFound) {
			http.Error(w, "website branding unavailable", 503)
			return "", false
		}
		p.SiteLogo = logo != nil
	}
	appearance, e := pagetemplate.New(s.Pool).Resolve(r.Context(), kind)
	if e != nil {
		http.Error(w, "page appearance unavailable", 503)
		return "", false
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
	tail, e := pagetemplate.Open(&b, appearance, p)
	if e != nil && appearance.Upload != nil {
		// An uploaded page that cannot be written falls back to the default.
		slog.Warn("uploaded page template render failed", "type", kind, "name", appearance.Name)
		appearance = pagetemplate.Resolved{Type: kind, Name: "default", Revision: appearance.Revision, Config: pagetemplate.DefaultConfig(kind, "default"), Fallback: "invalid_upload"}
		b.Reset()
		tail, e = pagetemplate.Open(&b, appearance, p)
	}
	if e != nil {
		http.Error(w, "page appearance unavailable", 503)
		return "", false
	}
	if appearance.Upload != nil {
		w.Header().Set("Content-Security-Policy", widenTemplateCSP(w.Header().Get("Content-Security-Policy")))
	}
	if appearance.Fallback != "" {
		w.Header().Set("X-Goepay-Template-Fallback", appearance.Fallback)
		slog.Warn("page template appearance fallback", "type", kind, "reason", appearance.Fallback)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b.Bytes())
	return tail, true
}
