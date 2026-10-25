package server

import (
	"goepay/internal/identity"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIdentityProfile64RequiresOriginalSessionAndUnavailableIsClosed(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	if !s.IdentityProfilePublic(w, httptest.NewRequest("POST", "/identity/profile/mini/wxpay/1/"+"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/", nil)) || w.Code != 503 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	if !s.IdentityProfileConsole(w, httptest.NewRequest("GET", "/console/v1/merchant/identity-profile-lookups/capabilities", nil), "merchant", identity.Session{}, []string{"merchant", "identity-profile-lookups", "capabilities"}, "") || w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestIdentityProfileJSONRejectsNullAndDuplicateOptionalMember(t *testing.T) {
	for _, body := range []string{`{"merchant_id":null}`, `{"merchant_id":"1","merchant_id":"1"}`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		if _, ok := identityJSON(w, r, "merchant_id"); ok {
			t.Fatalf("ambiguous optional member accepted: %s", body)
		}
	}
}
