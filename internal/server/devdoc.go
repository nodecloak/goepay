package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"goepay/internal/devdoc"
	"goepay/internal/sitebrand"
	"goepay/internal/siteinfo"
)

// devDocs serves the Go service's public developer documentation (/doc/).
// It needs no session; the pages show the site name, the
// merchant API address and the enabled 支付方式.
func (s *Server) devDocs(w http.ResponseWriter, r *http.Request) bool {
	if !devdoc.Match(r.URL.Path) {
		return false
	}
	devdoc.Serve(w, r, s.devDocSite)
	return true
}

// testPayAlias sends the original epay_pro address of the public test-pay
// page (/user/test) to GOEPAY's /test page.
func (s *Server) testPayAlias(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/user/test" && r.URL.Path != "/user/test/" {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/test", http.StatusFound)
	return true
}

func (s *Server) devDocSite(r *http.Request) (devdoc.Site, error) {
	if s.Pool == nil {
		return devdoc.Site{}, errors.New("database unavailable")
	}
	cfg, err := siteinfo.Load(r.Context(), s.Pool)
	if err != nil {
		return devdoc.Site{}, err
	}
	site := devdoc.Site{SiteName: cfg.Name, API: docsAPIBase(s.DocsAPIOrigin)}
	logo, err := sitebrand.New(s.Pool, s.CheckoutKey).Public(r.Context(), "logo")
	if err != nil && !errors.Is(err, sitebrand.ErrNotFound) {
		return devdoc.Site{}, err
	}
	if logo != nil {
		site.LogoURL = "/static/img/logo.png"
	}
	site.PayTypes, err = s.docPayTypes(r.Context())
	return site, err
}

var docsPathPattern = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)*$`)

// docsAPIBase turns the configured public origin (GOEPAY_PUBLIC_ORIGIN, where
// merchants send /api/pay/* requests) into the address the pages print. An
// unset or unusable value yields "/", so the pages show plain paths rather
// than a guessed host.
func docsAPIBase(origin string) string {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(origin), "/"))
	if origin == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || !docsPathPattern.MatchString(u.Path) {
		return "/"
	}
	return u.Scheme + "://" + u.Host + u.Path + "/"
}

// docPayTypes lists the 支付方式 a merchant can send as type, in the same way
// the payment router decides (payTypeCatalogOpen): a type is open while any of
// its records is enabled, and a type without records follows the original
// default (alipay and wxpay open). The internal sandbox type is not listed.
func (s *Server) docPayTypes(ctx context.Context) ([]devdoc.PayType, error) {
	rows, err := s.Pool.Query(ctx, `SELECT coalesce(body->>'type',''), coalesce(body->>'name',''), coalesce(body->'enabled'='true'::jsonb,false) FROM backoffice_records WHERE resource='pay-types' ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type state struct {
		records, enabled int
		name             string
	}
	byType := map[string]*state{}
	var order []string
	for rows.Next() {
		var kind, name string
		var enabled bool
		if err = rows.Scan(&kind, &name, &enabled); err != nil {
			return nil, err
		}
		st := byType[kind]
		if st == nil {
			st = &state{}
			byType[kind] = st
			order = append(order, kind)
		}
		st.records++
		if enabled {
			st.enabled++
			if st.name == "" {
				st.name = strings.TrimSpace(name)
			}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out := []devdoc.PayType{}
	catalog := map[string]bool{}
	for _, t := range originalPayTypes {
		catalog[t.Name] = true
		st := byType[t.Name]
		switch {
		case st == nil && t.Open:
			out = append(out, devdoc.PayType{Type: t.Name, Name: t.ShowName})
		case st != nil && st.enabled > 0:
			name := st.name
			if name == "" {
				name = t.ShowName
			}
			out = append(out, devdoc.PayType{Type: t.Name, Name: name})
		}
	}
	for _, kind := range order {
		st := byType[kind]
		if catalog[kind] || kind == "" || kind == "sandbox" || st.enabled == 0 || st.name == "" {
			continue
		}
		out = append(out, devdoc.PayType{Type: kind, Name: st.name})
	}
	return out, nil
}
