package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type rejectedOCRBody233 struct{ read bool }

func (b *rejectedOCRBody233) Read([]byte) (int, error) {
	b.read = true
	return 0, http.ErrBodyReadAfterClose
}
func (*rejectedOCRBody233) Close() error { return nil }

func TestOCR233AdmissionRejectsBeforeReadingBodyAndReleasesInvalidRequests(t *testing.T) {
	f := newKYC233(t)
	for i := 0; i < cap(ocrAdmission233); i++ {
		ocrAdmission233 <- struct{}{}
	}
	defer func() {
		for len(ocrAdmission233) > 0 {
			<-ocrAdmission233
		}
	}()
	body := &rejectedOCRBody233{}
	r := httptest.NewRequest("POST", "/console/v1/admin/account/ocr", body)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.Header.Set("Content-Type", "multipart/form-data; boundary=synthetic233")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || body.read {
		t.Fatal("busy OCR request read image or lost bounded admission", w.Code, body.read)
	}
	for len(ocrAdmission233) > 0 {
		<-ocrAdmission233
	}
	for i := 0; i < 3; i++ {
		r = httptest.NewRequest("POST", "/console/v1/admin/account/ocr", strings.NewReader("invalid-local-image"))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.Header.Set("Content-Type", "multipart/form-data; boundary=synthetic233")
		w = httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != 422 || len(ocrAdmission233) != 0 {
			t.Fatal("failed OCR input leaked its concurrency slot", w.Code, len(ocrAdmission233))
		}
	}
	if f.provider.ocrCalls.Load() != 0 {
		t.Fatal("busy/invalid input reached provider")
	}
}
