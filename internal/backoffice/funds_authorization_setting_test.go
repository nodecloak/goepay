package backoffice

import (
	"context"
	"testing"
)

func TestFundsAuthorizationSettingExplicitBooleanAndSchemaOption(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	schema, e := s.Schema(admin, "settings")
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, f := range schema.Fields {
		if f.Name == "key" {
			for _, o := range f.Options {
				if o.Value == "admin_funds_authorization_enabled" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("enforcement setting missing in real admin schema")
	}
	if _, e = s.Create(ctx, admin, "settings", input(`{"key":"admin_funds_authorization_enabled","value":"1"}`)); e == nil {
		t.Fatal("nonboolean enforcement accepted")
	}
	if _, e = s.Create(ctx, admin, "settings", input(`{"key":"admin_funds_authorization_enabled","value":"true"}`)); e != nil {
		t.Fatal(e)
	}
}
