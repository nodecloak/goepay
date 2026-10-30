package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocsAPIBase(t *testing.T) {
	for origin, want := range map[string]string{
		"":                             "/",
		"https://pay.example.com":      "https://pay.example.com/",
		"https://pay.example.com/":     "https://pay.example.com/",
		"http://127.0.0.1:8186":        "http://127.0.0.1:8186/",
		"https://pay.example.com/epay": "https://pay.example.com/epay/",
		" https://pay.example.com ":    "https://pay.example.com/",
		"ftp://pay.example.com":        "/",
		"https://user@pay.example.com": "/",
		"https://pay.example.com/?x=1": "/",
		"https://pay.example.com/#x":   "/",
		"https://pay.example.com/a b":  "/",
		"https://pay.example.com/<x>":  "/",
		"pay.example.com":              "/",
	} {
		if got := docsAPIBase(origin); got != want {
			t.Errorf("docsAPIBase(%q) = %q, want %q", origin, got, want)
		}
	}
}

// The documentation and the /user/test alias are answered before the payment
// API catch-all, so they never reach paymentAPI's merchant parsing.
func TestDocsAndTestPayAliasRouteBeforePaymentAPI(t *testing.T) {
	s := &Server{}
	h := s.Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/doc/", nil))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/doc/index.html" {
		t.Fatalf("/doc/: %d %q", w.Code, w.Header().Get("Location"))
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/doc/doc.css", nil))
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("/doc/doc.css: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/doc/index.html", nil))
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "merchant unavailable") {
		t.Fatalf("/doc/index.html without a database: %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{"/user/test", "/user/test/"} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusFound || w.Header().Get("Location") != "/test" {
			t.Fatalf("%s: %d %q", path, w.Code, w.Header().Get("Location"))
		}
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/user/test", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /user/test: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/docs", nil))
	if w.Code == http.StatusFound || strings.Contains(w.Body.String(), "开发文档") {
		t.Fatalf("/docs must not be served by the documentation: %d", w.Code)
	}
}
