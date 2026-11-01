package backoffice

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestDomainsMerchantFilter(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, raw := range []string{
		`{"merchant_id":"101","domain":"one.example.invalid","approved":true,"enabled":true}`,
		`{"merchant_id":"102","domain":"two.example.invalid","approved":false,"enabled":true}`,
	} {
		if _, err := s.Create(ctx, admin, "domains", input(raw)); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.List(ctx, admin, "domains", Query{Filter: map[string]string{"merchant_id": "102"}})
	if err != nil || page.Total != 1 || page.Items[0].Fields["domain"] != "two.example.invalid" {
		t.Fatalf("admin filter: %+v %v", page, err)
	}
	// An ID is an exact decimal int64 string, never a floating point value.
	for _, value := range []string{"0", "-1", "01", "1.0", "1e2", " 101", "9223372036854775808", "101' OR true--"} {
		if _, err := s.List(ctx, admin, "domains", Query{Filter: map[string]string{"merchant_id": value}}); err != ErrInvalid {
			t.Errorf("%q: %v", value, err)
		}
	}
	page, err = s.List(ctx, admin, "domains", Query{Filter: map[string]string{"merchant_id": "9007199254740995"}})
	if err != nil || page.Total != 0 {
		t.Fatalf("large exact ID: %+v %v", page, err)
	}
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "merchant"}
	if _, err = s.List(ctx, merchant, "domains", Query{Filter: map[string]string{"merchant_id": "102"}}); err != ErrInvalid {
		t.Fatalf("merchant filter bypass: %v", err)
	}
	own, err := s.List(ctx, merchant, "domains", Query{})
	if err != nil || own.Total != 1 || own.Items[0].Fields["domain"] != "one.example.invalid" {
		t.Fatalf("merchant own scope: %+v %v", own, err)
	}
	for _, tc := range []struct {
		p     Principal
		query string
		code  int
	}{
		{admin, "merchant_id=102", 200}, {admin, "merchant_id=101&merchant_id=102", 400},
		{merchant, "merchant_id=102", 400},
	} {
		w := httptest.NewRecorder()
		s.Serve(w, httptest.NewRequest("GET", "/resources/domains?"+tc.query, nil), tc.p, []string{"domains"})
		if w.Code != tc.code {
			t.Errorf("%s %s: status=%d body=%s", tc.p.Role, tc.query, w.Code, w.Body.String())
		}
	}
}

func TestDomainsMerchantFilterSchema(t *testing.T) {
	s := New(nil, nil)
	for _, tc := range []struct {
		p    Principal
		want bool
	}{
		{admin, true}, {Principal{Role: "merchant", SubjectID: 101}, false},
	} {
		sc, err := s.Schema(tc.p, "domains")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, f := range sc.Filters {
			if f.Param == "merchant_id" {
				found = true
				if f.Type != "positive_id" || f.Options == nil {
					t.Fatal("merchant filter must expose a typed input and an empty options array")
				}
			}
		}
		if found != tc.want {
			t.Errorf("%s merchant filter: %v", tc.p.Role, found)
		}
	}
}
