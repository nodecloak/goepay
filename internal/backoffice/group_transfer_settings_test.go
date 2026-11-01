package backoffice

import (
	"errors"
	"fmt"
	"testing"
)

func TestGroupTransferSettingsSchemaCRUDAndPreciseValidation(t *testing.T) {
	s := setup(t)
	schema, err := s.Schema(admin, "groups")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"transfer_rate", "user_transfer_red"} {
		found := false
		for _, field := range schema.Fields {
			if field.Name == name && !field.Required {
				found = true
			}
		}
		if !found {
			t.Fatal("missing optional group transfer field", name)
		}
	}
	for i, value := range []string{"", "_", "0", "0.00", "1.25", "100.00"} {
		item, err := s.Create(t.Context(), admin, "groups", input(fmt.Sprintf(`{"name":"transfer-fee-%d","transfer_rate":%q,"user_transfer_red":"1"}`, i, value)))
		if err != nil || item.Fields["transfer_rate"] != value || item.Fields["user_transfer_red"] != "1" {
			t.Fatal("group fee/source was normalized incorrectly", value, item, err)
		}
		if _, err := s.Update(t.Context(), admin, "groups", item.ID, input(`{"user_transfer_red":"0"}`)); err != nil {
			t.Fatal("could not close group redpack creation", err)
		}
	}
	for _, raw := range []string{
		`"transfer_rate":"100.01"`, `"transfer_rate":"1.001"`, `"transfer_rate":"-1"`, `"transfer_rate":0`, `"transfer_rate":null`,
		`"user_transfer_red":"true"`, `"user_transfer_red":true`, `"user_transfer_red":null`,
	} {
		if _, err := s.Create(t.Context(), admin, "groups", input(`{"name":"invalid group fee",`+raw+`}`)); !errors.Is(err, ErrInvalid) {
			t.Fatal("malformed group transfer setting persisted", raw, err)
		}
	}
}
