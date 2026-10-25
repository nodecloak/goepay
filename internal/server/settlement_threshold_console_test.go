package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettlementThreshold74ConsoleAcceptsExactBusinessModeWithoutPaymentAliases(t *testing.T) {
	base := strings.Replace(settlementPlanReviewDefinition, `"mode":"generate_only"`, `"mode":"balance_threshold"`, 1)
	base = strings.Replace(base, `"frequency":"day"`, `"frequency":"minute"`, 1)
	r := httptest.NewRequest("POST", "/console/v1/admin/settlement-plans", strings.NewReader(base))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	in, _, ok := decodeSettlementPlanDefinition(w, r, false)
	if !ok || in.Mode != "balance_threshold" || in.Schedule.Frequency != "minute" {
		t.Fatal("explicit threshold definition rejected", w.Code, w.Body.String(), in)
	}
	for _, body := range []string{strings.Replace(base, "balance_threshold", "threshold", 1), strings.Replace(base, "balance_threshold", "auto_payout", 1), strings.Replace(base, `"mode":"balance_threshold"`, `"Mode":"balance_threshold"`, 1), strings.Replace(base, `"merchant_ids":["901"]`, `"merchant_ids":["901"],"threshold":"1.00"`, 1)} {
		r := httptest.NewRequest("POST", "/console/v1/admin/settlement-plans", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		if _, _, ok := decodeSettlementPlanDefinition(w, r, false); ok {
			t.Fatal("ambiguous threshold scope accepted", body)
		}
	}
}
