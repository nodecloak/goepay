package backoffice

import (
	"context"
	"testing"
)

func TestEnrollmentSettingIsRegisteredAndStrictBoolean(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, value := range []string{"1", "False", "yes", ""} {
		if _, e := s.Create(ctx, admin, "settings", input(`{"key":"registration_invite_required","value":"`+value+`"}`)); e == nil {
			t.Fatal("invalid boolean accepted", value)
		}
	}
	if _, e := s.Create(ctx, admin, "settings", input(`{"key":"registration_invite_required","value":"true"}`)); e != nil {
		t.Fatal(e)
	}
	sch, e := s.Schema(admin, "settings")
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, f := range sch.Fields {
		if f.Name == "key" {
			for _, o := range f.Options {
				found = found || o.Value == "registration_invite_required"
			}
		}
	}
	if !found {
		t.Fatal("invite requirement unavailable to administrator settings UI")
	}
}
