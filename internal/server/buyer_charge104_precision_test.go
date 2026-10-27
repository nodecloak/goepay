package server

import (
	"encoding/json"
	"testing"
)

func TestBuyerCharge104OriginalProDecimalBuyerRatesRemainExactText(t *testing.T) {
	f := buyer104MerchantLocal(t)
	for _, rate := range []string{"0.125", "0.0001", "0.000001", "3.141592", "0.333333333333", "0.000000000001", "100"} {
		state := channel168Data(t, f.buyer("GET", "", ""))
		body, _ := json.Marshal(map[string]any{"mode": 1, "mode_feerate": rate, "expected_fingerprint": state["fingerprint"]})
		w := f.buyer("PATCH", "", string(body))
		if w.Code != 200 {
			t.Fatal("original calculator supports buyer percentage precision", rate, w.Code, w.Body.String())
		}
		saved := channel168Data(t, w)
		if saved["mode_feerate"] != rate || saved["partial_available"] != true {
			t.Fatal("rate rounded or capability concealed", rate, saved)
		}
	}
}

func TestBuyerCharge104LostHeadAndAuditCannotRestoreDefault(t *testing.T) {
	f := buyer104MerchantLocal(t)
	v := channel168Data(t, f.buyer("GET", "", ""))
	body, _ := json.Marshal(map[string]any{"mode": 1, "mode_feerate": "3.125", "expected_fingerprint": v["fingerprint"]})
	channel168Data(t, f.buyer("PATCH", "", string(body)))
	if _, e := f.p.Exec(t.Context(), `ALTER TABLE customer_charge_policies104 DISABLE TRIGGER USER;DELETE FROM customer_charge_policies104;ALTER TABLE customer_charge_policies104 ENABLE TRIGGER USER;ALTER TABLE backoffice_audit DISABLE TRIGGER USER;DELETE FROM backoffice_audit WHERE resource='customer-charge-policies104';ALTER TABLE backoffice_audit ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	w := f.buyer("GET", "", "")
	if w.Code != 409 {
		t.Fatal("lost financial policy became a never-configured default", w.Code, w.Body.String())
	}
}
