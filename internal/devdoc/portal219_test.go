package devdoc

import (
	"net/http"
	"strings"
	"testing"
)

func TestPortal219APIWorkbenchAndProgressiveGuide(t *testing.T) {
	api := render(t, "pay_query", testSite)
	for _, want := range []string{"data-api-console", `data-endpoint-slug="pay_query"`, `data-param-name="out_trade_no"`, "文档沙箱", "doc.js?v="} {
		if !strings.Contains(api, want) {
			t.Errorf("API workbench missing %q", want)
		}
	}
	guide := render(t, "sign_note", testSite)
	if strings.Contains(guide, "data-api-console") || !strings.Contains(guide, "签名规则") {
		t.Error("guide must remain readable without a request console")
	}
}

func TestPortal219ExternalModuleAndSandboxAwareCSP(t *testing.T) {
	out := render(t, "index", testSite)
	if strings.Count(out, "<script") != 1 || !strings.Contains(out, `type="module"`) || !strings.Contains(out, `src="/doc/doc.js?v=`) {
		t.Error("one versioned same-origin external module is required")
	}
	if strings.Contains(out, "<form") || strings.Contains(out, "onclick=") || strings.Contains(out, "javascript:") {
		t.Error("documentation must not add form navigation or inline code")
	}
	w := serve(t, http.MethodGet, "/doc/doc.js", okSite)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "javascript") || len(w.Body.Bytes()) < 100 {
		t.Error("documentation module must be served as a real JavaScript asset")
	}
	head := serve(t, http.MethodHead, "/doc/doc.js", okSite)
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Error("HEAD JavaScript must not write a body")
	}
	page := serve(t, http.MethodGet, "/doc/index.html", okSite)
	csp := page.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "connect-src 'self'") || strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
		t.Errorf("unexpected documentation CSP: %s", csp)
	}
}
