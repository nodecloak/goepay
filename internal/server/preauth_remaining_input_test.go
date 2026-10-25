package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPreauthRemaining75TargetRequiresIndependentExactConfirmation(t *testing.T) {
	d, ok := fundsTarget("/console/v1/admin/orders/T75/release-remaining-authorization")
	if !ok || d.guard != "authorization-release-remaining" || !mandatoryFundsAuthorization(d) {
		t.Fatal("remaining release lacks mandatory separate guard", d, ok)
	}
	good := `{"expected_observation_id":"` + strings.Repeat("a", 64) + `","expected_remaining_cents":"450","expected_source_revision":"0"}`
	if _, _, e := normalizeFundsBody(json.RawMessage(good), d); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{`{}`, strings.Replace(good, `"450"`, `450`, 1), strings.Replace(good, `"450"`, `"0"`, 1), strings.Replace(good, `"0"`, `"100"`, 1), strings.Replace(good, `"0"`, `"01"`, 1), strings.Replace(good, strings.Repeat("a", 64), strings.Repeat("A", 64), 1), strings.TrimSuffix(good, "}") + `,"amount":"1.00"}`} {
		if _, _, e := normalizeFundsBody(json.RawMessage(bad), d); e == nil {
			t.Fatal("invalid confirmation accepted", bad)
		}
	}
	old, _ := fundsTarget("/console/v1/admin/orders/T75/release-authorization")
	if old.guard == d.guard {
		t.Fatal("old full grant reused")
	}
}
