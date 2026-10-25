package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

const settlementPlanReviewDefinition = `{"name":"local midnight plan","mode":"generate_only","schedule":{"frequency":"day","interval":"1","anchor_at":"2026-10-23T00:00:00+08:00","execute_hour":0,"execute_minute":0,"execute_second":0},"merchant_ids":["901"]}`

func TestSettlementPlanInputReviewExplicitMidnightAndAmbiguousAliases(t *testing.T) {
	decode := func(raw string) (bool, int) {
		t.Helper()
		r := httptest.NewRequest("POST", "/console/v1/admin/settlement-plans", strings.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		in, _, ok := decodeSettlementPlanDefinition(w, r, false)
		if ok && (in.Schedule.ExecuteHour != 0 || in.Schedule.Interval != 1 || len(in.MerchantIDs) != 1 || in.MerchantIDs[0] != 901) {
			t.Fatalf("explicit midnight mutated %+v", in)
		}
		return ok, w.Code
	}
	if ok, status := decode(settlementPlanReviewDefinition); !ok {
		t.Fatalf("explicit hour zero rejected: status%d", status)
	}
	for _, raw := range []string{
		strings.Replace(settlementPlanReviewDefinition, `"execute_hour":0`, `"execute_hour":null`, 1),
		strings.Replace(settlementPlanReviewDefinition, `"execute_minute":0`, `"execute_minute":null`, 1),
		strings.Replace(settlementPlanReviewDefinition, `"execute_second":0`, `"execute_second":null`, 1),
		strings.Replace(settlementPlanReviewDefinition, `"interval":"1"`, `"interval":1`, 1),
		strings.Replace(settlementPlanReviewDefinition, `"interval":"1"`, `"interval":"01"`, 1),
		strings.Replace(settlementPlanReviewDefinition, `"interval":"1"`, `"interval":"1","interval":"2"`, 1),
		strings.Replace(settlementPlanReviewDefinition, `"execute_hour":0`, `"execute_hour":0,"ExecuteHour":1`, 1),
		strings.Replace(settlementPlanReviewDefinition, `"execute_hour":0`, `"execute_hour":0,"maturity_hour":8`, 1),
		strings.Replace(settlementPlanReviewDefinition, `"merchant_ids":["901"]`, `"merchant_ids":["0901"]`, 1),
		strings.Replace(settlementPlanReviewDefinition, `"merchant_ids":["901"]`, `"merchant_ids":["901","901"]`, 1),
		strings.Replace(settlementPlanReviewDefinition, `"merchant_ids":["901"]`, `"merchant_ids":[901]`, 1),
		strings.Replace(settlementPlanReviewDefinition, `"mode":"generate_only"`, `"mode":"auto_payout"`, 1),
		strings.Replace(settlementPlanReviewDefinition, `"name":"local midnight plan"`, `"name":"local midnight plan","enabled":true`, 1),
	} {
		if ok, status := decode(raw); ok || status < 400 {
			t.Fatalf("ambiguous financial definition accepted status%d body%s", status, raw)
		}
	}
}

func TestSettlementPlanInputReviewAuthorityDatesNormalizeButScopeDoesNotAlias(t *testing.T) {
	d := fundsTargetDefinition{category: "settlement-plan"}
	_, original, err := normalizeSettlementPlanFundsBody([]byte(validSettlementPlanAuthorizationBody), d)
	if err != nil {
		t.Fatal(err)
	}
	local := strings.ReplaceAll(validSettlementPlanAuthorizationBody, "T00:00:00Z", "T08:00:00+08:00")
	_, normalized, err := normalizeSettlementPlanFundsBody([]byte(local), d)
	if err != nil || string(normalized) != string(original) {
		t.Fatalf("same authorization instant produced another action: %s %s %v", original, normalized, err)
	}
	for _, raw := range []string{
		strings.Replace(validSettlementPlanAuthorizationBody, `"max_day_items":"10"`, `"max_day_items":null`, 1),
		strings.Replace(validSettlementPlanAuthorizationBody, `"expected_revision":"1"`, `"Expected_Revision":"1"`, 1),
		strings.Replace(validSettlementPlanAuthorizationBody, `"max_day_gross":"200000"`, `"max_day_gross":"9223372036854775808"`, 1),
		strings.Replace(validSettlementPlanAuthorizationBody, `"max_day_items":"10"`, `"max_day_items":"10","merchant_ids":["902"]`, 1),
		strings.Replace(validSettlementPlanAuthorizationBody, `"expected_authorization_id":"0"`, `"expected_authorization_id":"-0"`, 1),
	} {
		if _, _, err = normalizeSettlementPlanFundsBody([]byte(raw), d); err == nil {
			t.Fatalf("authority input hid or aliased scope %s", raw)
		}
	}
	var decoded map[string]any
	if err = json.Unmarshal(original, &decoded); err != nil || decoded["valid_from"] != "2026-10-23T00:00:00Z" {
		t.Fatal("UTC normalization failed", decoded, err)
	}
}
