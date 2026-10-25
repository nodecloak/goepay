package server

import "testing"

func TestIncomeHoldAdoptionRequiresExactBodyAndMandatoryGrant(t *testing.T) {
	d, ok := fundsTarget("/console/v1/admin/orders/T/adopt-income-hold")
	if !ok || d.guard != "income-hold-adopt" {
		t.Fatal("historical funding adoption is not protected", d, ok)
	}
	if !mandatoryFundsAuthorization(d) {
		t.Fatal("adoption must require a grant even when optional financial protection is off")
	}
	for _, raw := range []string{
		`{}`, `{"hold_id":"1"}`, `{"external_no":"adopt"}`,
		`{"hold_id":1,"external_no":"adopt"}`, `{"hold_id":"01","external_no":"adopt"}`,
		`{"hold_id":"0","external_no":"adopt"}`, `{"hold_id":"1","external_no":null}`,
		`{"hold_id":"1","external_no":""}`, `{"hold_id":"1","external_no":"adopt","amount":"1"}`,
		`{"hold_id":"1","hold_id":"2","external_no":"adopt"}`,
	} {
		if _, _, err := normalizeFundsBody([]byte(raw), d); err == nil {
			t.Errorf("ambiguous historical funding adoption accepted: %s", raw)
		}
	}
	if _, _, err := normalizeFundsBody([]byte(`{"hold_id":"1","external_no":"adopt-original"}`), d); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/console/v1/admin/orders/T/refund", "/console/v1/admin/orders/T/freeze-income", "/console/v1/admin/orders/T/unfreeze-income"} {
		definition, ok := fundsTarget(target)
		if !ok || mandatoryFundsAuthorization(definition) {
			t.Fatal("ordinary operation policy changed", target)
		}
	}
}
