package main

import (
	"encoding/json"
	"testing"

	"goepay/internal/businessnotice"
	"goepay/internal/merchantnotice"
)

func TestReconciliationCLIIncludesBothIndependentNoticeIntegrityNamespaces(t *testing.T) {
	report := reconciliationReport{IntegrityCheck: merchantnotice.IntegrityCheck{InvalidProfiles: 1, InvalidEvents: 2, InvalidDeliveries: 3, UnknownDeliveries: 4}, BusinessNoticeIntegrity: businessnotice.IntegrityCheck{InvalidEvents: 5, InvalidDeliveries: 6, UnknownDeliveries: 7}}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]int64
	if err = json.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 32 {
		t.Fatalf("original CLI counter contract changed: fields=%d", len(values))
	}
	for key, want := range map[string]int64{"invalid_merchant_notice_profiles": 1, "invalid_merchant_notice_events": 2, "invalid_merchant_notice_deliveries": 3, "unknown_merchant_notice_deliveries": 4, "invalid_admin_business_notice_events": 5, "invalid_admin_business_notice_deliveries": 6, "unknown_admin_business_notice_deliveries": 7} {
		if values[key] != want {
			t.Fatalf("counter omitted or overwritten: %s got%d want%d", key, values[key], want)
		}
	}
}
