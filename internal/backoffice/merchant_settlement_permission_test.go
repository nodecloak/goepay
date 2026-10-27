package backoffice

import (
	"context"
	"errors"
	"net/url"
	"testing"
)

func TestOriginalMerchantSettlementPermissionSchemaProjectionAndDirectory(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p := Principal{Role: "admin", AdminClass: "super", SubjectID: 1, Username: "admin"}
	schema, e := s.Schema(p, "merchants")
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, f := range schema.Fields {
		if f.Name == "settle" {
			found = f.Label == "结算权限" && f.Type == "select" && len(f.Options) == 2
		}
	}
	if !found {
		t.Fatal("original settle field missing from administrator editor")
	}
	if _, e = s.Update(ctx, p, "merchants", 101, input(`{"settle":"0"}`)); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		state string
		id    int64
		value string
	}{{"settle_0", 101, "0"}, {"settle_1", 102, "1"}} {
		filter, _, e := ParseMerchantDirectoryQuery(url.Values{"dstatus": {tc.state}})
		if e != nil {
			t.Fatal(e)
		}
		page, e := s.List(ctx, p, "merchants", Query{Merchant: filter, Limit: 20})
		if e != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != tc.id || page.Items[0].Fields["settle"] != tc.value {
			t.Fatal("original settlement permission directory ignored", tc.state, page, e)
		}
	}
	for _, raw := range []string{`{"settle":"2"}`, `{"settle":true}`, `{"settle":null}`, `{"settlement_permission_revision":"1"}`} {
		if _, e = s.Update(ctx, p, "merchants", 101, input(raw)); e == nil {
			t.Fatal("invalid or readonly permission state accepted", raw)
		}
	}
	if _, e = s.Update(ctx, Principal{Role: "merchant", SubjectID: 101, Username: "local"}, "merchants", 101, input(`{"settle":"1"}`)); !errors.Is(e, ErrForbidden) {
		t.Fatal("merchant changed own payout gate", e)
	}
}
