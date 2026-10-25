package backoffice

import (
	"errors"
	"testing"
)

func TestLegacyTemplateRecordsCannotBypassTypedConfiguration(t *testing.T) {
	p := Principal{Role: "admin", AdminClass: "super", SubjectID: 1, Username: "local"}
	if _, err := definitionFor(p, "template-configs", true); !errors.Is(err, ErrForbidden) {
		t.Fatal("legacy generic template write bypass", err)
	}
	schema, err := New(nil, nil).Schema(p, "template-configs")
	if err != nil || schema.Permissions.Create || schema.Permissions.Update || schema.Permissions.Delete {
		t.Fatal("legacy template schema still permits writes", schema.Permissions, err)
	}
}
