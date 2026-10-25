package payment

import (
	"encoding/hex"
	"testing"
	"time"
)

// Golden captured before extending settlementPlanScope for threshold mode.
// An omitted new field must preserve actual existing signed authority bytes.
func TestSettlementThreshold74KeepsExistingGenerationProofBytes(t *testing.T) {
	s := &Store{indexKey: make([]byte, 32)}
	at := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	r := settlementPlanAuthorizationRecord{ActorID: 1, ActorName: "synthetic-original-admin"}
	r.View = SettlementPlanAuthorizationView{ID: 8, PlanID: 4, PlanRevision: 3, CreatedAt: at, SettlementPlanAuthorizationInput: SettlementPlanAuthorizationInput{ValidFrom: at, ExpiresAt: at.Add(time.Hour), MaxItemGross: 1000, MaxRunGross: 2000, MaxDayGross: 3000, MaxDayItems: 5}}
	r.Scope = settlementPlanScope{DefinitionID: 7, DefinitionHash: make([]byte, 32), Policy: SettlementPolicy{MinimumAmount: 100, FeeRateBPS: 300}, SettleType: "0", Merchants: []settlementScopeMerchant{{MerchantID: 2, PreferenceID: 3, PreferenceRevision: 1, PreferenceHash: make([]byte, 32), RecipientConfigID: 4, RecipientHash: make([]byte, 32), Recipient: SettlementRecipientView{State: "configured", Type: "alipay", AccountMasked: "***test"}, Enabled: true}}}
	proof, e := s.settlementPlanAuthorizationProof(r)
	if e != nil {
		t.Fatal(e)
	}
	const original = "fb2a39232ef520489963c2269ae13c31ac8058c9299c78cf67478905318817b3"
	if got := hex.EncodeToString(proof); got != original {
		t.Fatalf("existing signed generation bytes changed: %s", got)
	}
}
