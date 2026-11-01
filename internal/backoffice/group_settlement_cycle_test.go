package backoffice

import (
	"errors"
	"fmt"
	"testing"
)

func TestOriginalGroupSettlementCycleSchemaAndNormalizedCRUD(t *testing.T) {
	s := setup(t)
	schema, err := s.Schema(admin, "groups")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, field := range schema.Fields {
		if field.Name == "settle_type" && field.Type == "select" && !field.Required && field.Default == "_" {
			found = true
		}
	}
	if !found {
		t.Fatal("group cycle must expose normalized D+0/D+1 with inheritance")
	}
	for i, value := range []string{"", "_", "0", "1"} {
		item, err := s.Create(t.Context(), admin, "groups", input(fmt.Sprintf(`{"name":"cycle-%d","settle_type":%q}`, i, value)))
		if err != nil || item.Fields["settle_type"] != value {
			t.Fatal("normalized group cycle was not preserved", value, item, err)
		}
		if _, err = s.Update(t.Context(), admin, "groups", item.ID, input(`{"settle_type":"2"}`)); !errors.Is(err, ErrInvalid) {
			t.Fatal("raw PHP group code must not become a normalized cycle", err)
		}
	}
	for _, value := range []string{`"2"`, `"-1"`, `"01"`, `0`, `null`, `true`} {
		if _, err := s.Create(t.Context(), admin, "groups", input(`{"name":"invalid cycle","settle_type":`+value+`}`)); !errors.Is(err, ErrInvalid) {
			t.Fatal("unsupported group cycle accepted", value, err)
		}
	}
}
