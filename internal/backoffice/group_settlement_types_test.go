package backoffice

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"testing"

	"goepay/internal/payment"
)

func TestGroupSettlementTypesSchemaValidationAndAllowedPreview(t *testing.T) {
	s := setup(t)
	schema, err := s.Schema(admin, "groups")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"settle_alipay", "settle_wxpay", "settle_bank"} {
		found := false
		for _, field := range schema.Fields {
			if field.Name == key && field.Type == "select" && !field.Required {
				found = true
			}
		}
		if !found {
			t.Errorf("supported group settlement switch missing: %s", key)
		}
	}
	for _, field := range schema.Fields {
		if field.Name == "settle_qqpay" && !field.ReadOnly {
			t.Fatal("unsupported QQ settlement exposed executable switch")
		}
	}
	group, err := s.Create(t.Context(), admin, "groups", input(`{"name":"restricted rails","settle_alipay":"0","settle_wxpay":"_","settle_bank":"1"}`))
	if err != nil {
		t.Fatal("group settlement fields not accepted", err)
	}
	for _, key := range []string{"settle_alipay", "settle_wxpay", "settle_bank"} {
		if _, err = s.Update(t.Context(), admin, "groups", group.ID, input(fmt.Sprintf(`{%q:"bad"}`, key))); !errors.Is(err, ErrInvalid) {
			t.Fatal("bad group settlement switch accepted", key, err)
		}
	}
	request := func(p Principal, query string) (int, []string) {
		w := httptest.NewRecorder()
		s.ServeWorkflows(w, httptest.NewRequest("GET", "/settlement-recipient/allowed-types"+query, nil), p, []string{"settlement-recipient", "allowed-types"})
		var response struct {
			Data struct {
				Settlement []string `json:"settlement"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return w.Code, response.Data.Settlement
	}
	if code, types := request(admin, fmt.Sprintf("?group_id=%d", group.ID)); code != 200 || !reflect.DeepEqual(types, []string{"wxpay", "bank"}) {
		t.Fatal("admin preview did not restrict selected group", code, types)
	}
	if code, types := request(admin, ""); code != 200 || !reflect.DeepEqual(types, []string{"alipay", "wxpay", "bank"}) {
		t.Fatal("global supported preview", code, types)
	}
	if _, err := s.Create(t.Context(), createSetupSuper, "merchants", input(fmt.Sprintf(`{"id":"11993","name":"synthetic group creation","group_id":%q,"settlement_type":"alipay","settlement_account":"local@example.test","settlement_name":"Synthetic"}`, fmt.Sprint(group.ID)))); !errors.Is(err, payment.ErrState) {
		t.Fatal("merchant creation bypassed selected group rail", err)
	}
	var exists bool
	if err := s.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM merchants WHERE id=11993)`).Scan(&exists); err != nil || exists {
		t.Fatal("refused creation was not atomic", exists, err)
	}
	for _, query := range []string{"?group_id=00", "?group_id=-1", "?group_id=1&group_id=2", "?extra=1", "?merchant_id=0", "?merchant_id=01", "?merchant_id=101&merchant_id=102", "?group_id=0&merchant_id=101"} {
		if code, _ := request(admin, query); code != 422 {
			t.Fatal("ambiguous/unknown preview query accepted", query, code)
		}
	}
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "m1"}
	if _, err = s.pool.Exec(t.Context(), `UPDATE merchants SET base_group_id=$1 WHERE id=101`, group.ID); err != nil {
		t.Fatal(err)
	}
	if code, types := request(merchant, ""); code != 200 || !reflect.DeepEqual(types, []string{"wxpay", "bank"}) {
		t.Fatal("merchant chooser ignored its effective group", code, types)
	}
	if code, types := request(admin, "?merchant_id=101"); code != 200 || !reflect.DeepEqual(types, []string{"wxpay", "bank"}) {
		t.Fatal("administrator merchant preview ignored effective group", code, types)
	}
	if code, _ := request(merchant, "?group_id=0"); code != 422 {
		t.Fatal("merchant previewed another group", code)
	}
}
