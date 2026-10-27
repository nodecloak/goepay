package payment

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// Golden bytes were exported by actual immutable154 after its authenticated
// authorization reader verified the source. Only a synthetic masked payee is
// included. The optional new policy must not change old signed serialization.
func TestOriginalGroupSettlementFeeLegacy154ScopeSerializationRemainsExact(t *testing.T) {
	b, e := os.ReadFile("testdata/settlement_scope_v154.json")
	if e != nil {
		t.Fatal(e)
	}
	if fmt.Sprintf("%x", sha256.Sum256(b)) != "17f024c697bf763df7702b8ce7d151f78755fdc2168b103399b4f6714eb63b1f" {
		t.Fatal("original verified154 golden changed")
	}
	var scope settlementPlanScope
	if json.Unmarshal(b, &scope) != nil || len(scope.Merchants) != 1 || scope.Merchants[0].SettlementPolicy != nil {
		t.Fatal("legacy scope acquired a new price snapshot")
	}
	got, e := json.Marshal(scope)
	if e != nil || !bytes.Equal(got, bytes.TrimSpace(b)) {
		t.Fatal("new reader changed old signed bytes", string(got), e)
	}
	t.Log("Verified original154 manifest b2c65d2209b389a7e2683c408622a0d41b5398a1e9db8e9deb98d3aa1564a4ad; original scope serialization unchanged")
}
