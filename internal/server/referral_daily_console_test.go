package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestReferralDailyPolicyExplicitModeStaysInFundsCanonicalBody(t *testing.T) {
	d, ok := referralPolicyFundsTarget([]string{"referral-policies", "global", "0"})
	if !ok {
		t.Fatal("canonical target unavailable")
	}
	raw := []byte(`{"expected":"0","enabled":true,"rate_bps":5000,"mode":"daily","basis":"gross","refund_policy":"any_successful_refund_voids_reward"}`)
	fields, canonical, err := normalizeReferralPolicyFundsBody(raw, d)
	if err != nil {
		t.Fatalf("explicit daily policy rejected: %v", err)
	}
	if string(fields["mode"]) != `"daily"` || string(fields["basis"]) != `"gross"` {
		t.Fatal("original daily/gross action changed")
	}
	var normalized map[string]json.RawMessage
	if json.Unmarshal(canonical, &normalized) != nil || string(normalized["mode"]) != `"daily"` {
		t.Fatal("funds grant does not bind exact mode")
	}
	for _, invalid := range []string{
		`{"expected":"0","enabled":true,"rate_bps":5000,"mode":"weekly","basis":"gross","refund_policy":"any_successful_refund_voids_reward"}`,
		`{"expected":"0","enabled":true,"rate_bps":5000,"basis":"gross","refund_policy":"any_successful_refund_voids_reward"}`,
		`{"expected":"0","enabled":true,"rate_bps":5000,"mode":"daily","mode":"immediate","basis":"gross","refund_policy":"any_successful_refund_voids_reward"}`,
		`{"expected":"0","enabled":true,"rate_bps":5000,"mode":"daily","basis":"unproven","refund_policy":"any_successful_refund_voids_reward"}`,
	} {
		if _, _, err = normalizeReferralPolicyFundsBody([]byte(invalid), d); err == nil {
			t.Fatal("ambiguous unsupported original policy accepted", invalid)
		}
	}
}

func TestReferralPagingRejectsMalformedOwnerSelectors(t *testing.T) {
	for _, query := range []string{"merchant_id=%zz", "after_id=0&limit=20&merchant_id=%zz", "after_id=0&limit=20&promoter_id=%zz", "after_id=%zz", "limit=20;merchant_id=1"} {
		request := httptest.NewRequest("GET", "/console/v1/merchant/referral/daily-queue?"+query, nil)
		response := httptest.NewRecorder()
		if _, _, err := referralPaging(response, request, false); err == nil || response.Code != 400 {
			t.Fatalf("malformed selector silently ignored: query=%q status=%d err=%v", query, response.Code, err)
		}
	}
}
