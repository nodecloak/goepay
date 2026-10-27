package contract_test

import (
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOriginalPrivateSearchRejectsMalformedInputAndForeignScopeBeforeQuery(t *testing.T) {
	s, _, _ := fixture(t)
	token, _, e := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	call := func(path, q string) int {
		r := httptest.NewRequest("GET", "/console/v1/merchant/"+path+"?"+q, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	for _, path := range []string{"workflows/settlements", "workflows/settlements/statistics"} {
		for _, q := range []string{"value=a&value=b", "value=%00", "value=%FF", "value=" + url.QueryEscape(strings.Repeat("中", 86)), "value=private&uid=2"} {
			want := 422
			if strings.Contains(q, "uid=2") {
				want = 403
			}
			if got := call(path, q); got != want {
				t.Fatalf("private settlement query %s %q got %d want %d", path, q, got, want)
			}
		}
	}
	for _, field := range []string{"account", "username", "desc"} {
		for _, q := range []string{"q=a&q=b", "q=%00", "q=%FF", "q=" + url.QueryEscape(strings.Repeat("中", 86)), "q=private&merchant_id=2"} {
			want := 400
			if strings.Contains(q, "merchant_id=2") {
				want = 403
			}
			if got := call("transfers", "search_field="+field+"&"+q); got != want {
				t.Fatalf("private transfer query %s %q got %d want %d", field, q, got, want)
			}
		}
	}
}
