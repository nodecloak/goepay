package backoffice

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestOriginalGroupSettlementMinimumSchemaAndCRUD(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	schema, err := s.Schema(admin, "groups")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, field := range schema.Fields {
		if field.Name == "settle_minmoney" && field.Type == "string" && !field.Required && !field.ReadOnly {
			found = true
		}
	}
	if !found {
		t.Fatal("editable original group settlement minimum missing")
	}
	for i, value := range []string{"", "_", "0", "0.00", "1.23", "1000000000.00"} {
		item, err := s.Create(ctx, admin, "groups", input(fmt.Sprintf(`{"name":"minimum-%d","settle_minmoney":%q}`, i, value)))
		if err != nil || item.Fields["settle_minmoney"] != value {
			t.Fatal("valid group minimum was not stored exactly", value, item, err)
		}
		updated, err := s.Update(ctx, admin, "groups", item.ID, input(`{"settle_minmoney":"_"}`))
		if err != nil || updated.Fields["settle_minmoney"] != "_" {
			t.Fatal("inherit update lost", updated, err)
		}
	}
	for _, raw := range []string{`"-1"`, `"1.001"`, `"1000000000.01"`, `"1e2"`, `"NaN"`, `true`, `1.23`, `null`} {
		if _, err := s.Create(ctx, admin, "groups", input(`{"name":"invalid minimum","settle_minmoney":`+raw+`}`)); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid group minimum accepted", raw, err)
		}
	}
}
