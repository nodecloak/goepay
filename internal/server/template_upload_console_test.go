package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWidenTemplateCSPAddsStylesFontsImagesButNeverScripts(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{
			"default-src 'none'; base-uri 'none'; frame-ancestors 'none'; style-src 'unsafe-inline'; img-src 'self'; connect-src 'self'; form-action http: https:; script-src 'nonce-abc'",
			"default-src 'none'; base-uri 'none'; frame-ancestors 'none'; style-src 'unsafe-inline' 'self'; img-src 'self' data:; connect-src 'self'; form-action http: https:; script-src 'nonce-abc'; font-src 'self' data:",
		},
		{
			"default-src 'none'; style-src 'none'; img-src 'self' data:; font-src 'none'",
			"default-src 'none'; style-src 'self'; img-src 'self' data:; font-src 'self' data:",
		},
		{"", "style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:"},
	} {
		if got := widenTemplateCSP(c.in); got != c.want {
			t.Fatalf("\n%s\n%s", got, c.want)
		}
	}
	for _, csp := range []string{"script-src 'none'", "default-src 'none'; script-src 'nonce-x'"} {
		if got := widenTemplateCSP(csp); strings.Contains(got, "script-src 'self'") || strings.Count(got, "script-src") != 1 {
			t.Fatal(got)
		}
	}
	if csp := homeUploadCSP("'none'"); !strings.Contains(csp, "sandbox allow-scripts") || strings.Contains(csp, "allow-same-origin") || !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "form-action 'none'") || strings.Contains(csp, "unsafe-eval") {
		t.Fatal(csp)
	}
}

func TestTemplateUploadRoutesAloneGetTheLargerBody(t *testing.T) {
	for path, want := range map[string]bool{
		"/console/v1/admin/templates/home/uploads":        true,
		"/console/v1/admin/templates/qrpay/uploads/check": true,
		"/console/v1/admin/templates/pay/uploads/x":       false,
		"/console/v1/admin/templates/images":              false,
		"/console/v1/admin/templates/other/uploads":       false,
		"/console/v1/admin/templates/home/uploads/check/": false,
	} {
		if templateUploadPath.MatchString(path) != want {
			t.Fatal(path)
		}
	}
	// Without a database the public template routes answer 503, files too.
	w := httptest.NewRecorder()
	if !(&Server{}).templatePublic(w, httptest.NewRequest("GET", "/template-files/0123456789abcdef0123456789abcdef/a.css", nil)) || w.Code != 503 {
		t.Fatal(w.Code)
	}
}
