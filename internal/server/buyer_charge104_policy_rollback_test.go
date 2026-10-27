package server

import (
	"encoding/json"
	"testing"
)

func TestBuyerCharge104PolicyHeadAndAuditRollbackCannotRestoreOldValidMAC(t *testing.T) {
	f := buyer104MerchantLocal(t)
	state := channel168Data(t, f.buyer("GET", "", ""))
	save := func(rate string) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"mode": 1, "mode_feerate": rate, "expected_fingerprint": state["fingerprint"]})
		return channel168Data(t, f.buyer("PATCH", "", string(body)))
	}
	state = save("3.125")
	var oldFacts, oldProof []byte
	if e := f.p.QueryRow(t.Context(), `SELECT facts,proof_hash FROM customer_charge_policies104 WHERE kind='merchant' AND owner_id=1`).Scan(&oldFacts, &oldProof); e != nil {
		t.Fatal(e)
	}
	state = save("1.25")
	if state["revision"] != "2" {
		t.Fatal(state)
	}
	if _, e := f.p.Exec(t.Context(), `ALTER TABLE customer_charge_policies104 DISABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.p.Exec(t.Context(), `UPDATE customer_charge_policies104 SET revision=1,facts=$1,proof_hash=$2 WHERE kind='merchant' AND owner_id=1`, oldFacts, oldProof); e != nil {
		t.Fatal(e)
	}
	if _, e := f.p.Exec(t.Context(), `ALTER TABLE customer_charge_policies104 ENABLE TRIGGER USER;ALTER TABLE backoffice_audit DISABLE TRIGGER USER;DELETE FROM backoffice_audit WHERE resource='customer-charge-policies104' AND resource_id=1 AND changes->>'kind'='merchant' AND changes->>'revision'='2';ALTER TABLE backoffice_audit ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	w := f.buyer("GET", "", "")
	if w.Code != 409 {
		t.Fatal("current pricing accepted an old valid policy after head/audit rollback", w.Code, w.Body.String())
	}
}
