package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserSubmitErrorsRenderHTML(t *testing.T) {
	for _, route := range []string{"/api/pay/submit", "/submit.php"} {
		for _, method := range []string{"GET", "POST", "PUT"} {
			t.Run(route+method, func(t *testing.T) {
				r := httptest.NewRequest(method, route, nil)
				r.Header.Set("Accept", "text/html,application/xhtml+xml")
				w := httptest.NewRecorder()
				(&Server{}).paymentAPI(w, r)
				if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "暂时无法发起支付") || strings.Contains(w.Body.String(), `"code":`) {
					t.Fatalf("not a friendly page: %d %s", w.Code, w.Body.String())
				}
				if w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'none'") {
					t.Fatal("missing protective headers")
				}
			})
		}
	}
}

func TestBrowserSubmitErrorBoundaryPreservesOtherResponses(t *testing.T) {
	for _, tc := range []struct {
		name, accept, fetch, path string
		html                      bool
	}{
		{"browser", "text/html", "", "/api/pay/submit", true},
		{"navigate", "*/*", "navigate", "/api/pay/submit", true},
		{"machine", "application/json", "", "/api/pay/submit", false},
		{"api-create", "text/html", "navigate", "/api/pay/create", false},
		{"refused-html", "text/html;q=0,application/json", "", "/api/pay/submit", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tc.path, nil)
			r.Header.Set("Accept", tc.accept)
			r.Header.Set("Sec-Fetch-Mode", tc.fetch)
			out := httptest.NewRecorder()
			w := submitBrowserResponse(out, r)
			(&Server{}).wire(w, false, nil, "payment channel not permitted")
			if tc.html {
				if !strings.Contains(out.Body.String(), "该商户暂未开通当前支付通道") || out.Code != 400 {
					t.Fatalf("channel error missing: %s", out.Body.String())
				}
			} else if !strings.Contains(out.Body.String(), `"msg":"payment channel not permitted"`) {
				t.Fatalf("machine response changed: %s", out.Body.String())
			}
		})
	}
	r := httptest.NewRequest("GET", "/api/pay/submit", nil)
	r.Header.Set("Accept", "text/html")
	for _, status := range []int{200, 302} {
		out := httptest.NewRecorder()
		w := submitBrowserResponse(out, r)
		if status == 302 {
			http.Redirect(w, r, "/checkout?opaque=1", status)
		} else {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>payment verification</html>"))
		}
		if out.Code != status || (status == 302 && out.Header().Get("Location") != "/checkout?opaque=1") || (status == 200 && out.Body.String() != "<html>payment verification</html>") {
			t.Fatal("successful flow changed")
		}
	}
	out := httptest.NewRecorder()
	w := submitBrowserResponse(out, r)
	(&Server{}).wire(w, false, nil, `<script>alert(1)</script> PRIVATE_KEY`)
	if strings.Contains(out.Body.String(), "PRIVATE_KEY") || strings.Contains(out.Body.String(), "<script>") {
		t.Fatal("untrusted error reflected")
	}
}
