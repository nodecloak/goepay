package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocs219PlaygroundDoesNotRequireAccountOrDatabase(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodPost, "https://docs.example/doc/playground/run", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://docs.example")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var body struct {
		Environment string `json:"environment"`
		Error       string `json:"error"`
	}
	if w.Code != http.StatusBadRequest || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Environment != "documentation-sandbox" || body.Error == "" {
		t.Fatalf("documentation sandbox was not routed independently: status=%d", w.Code)
	}
	if strings.Contains(w.Body.String(), "merchant unavailable") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("documentation sandbox reached the merchant API or became cacheable")
	}
}

func TestDocs219ModuleIsPublicWithoutDatabase(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := httptest.NewRecorder()
		(&Server{}).Handler().ServeHTTP(w, httptest.NewRequest(method, "/doc/doc.js", nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "javascript") {
			t.Fatalf("documentation module is not independently served: %s HTTP%d", method, w.Code)
		}
		if method == http.MethodHead && w.Body.Len() != 0 {
			t.Fatal("module HEAD returned a body")
		}
	}
}
