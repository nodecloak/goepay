package server

import "testing"

func TestManualRefundUXFundsTargetKeepsOfflineConfirmationAndReason(t *testing.T) {
	d, ok := fundsTarget("/console/v1/admin/orders/T123/manual-refund")
	if !ok || d.guard != "refund" || !mandatoryFundsAuthorization(d) {
		t.Fatal("manual refund escaped mandatory authorization", d, ok)
	}
	fields, _, e := normalizeFundsBody([]byte(`{"amount":"0.40","external_no":"manual-one","reason":"线下退款已完成","offline_confirmed":true}`), d)
	if e != nil || fundsText(fields, "reason") != "线下退款已完成" {
		t.Fatal(fields, e)
	}
	for _, body := range []string{`{"amount":"0.40","external_no":"manual-one","reason":"reason","offline_confirmed":false}`, `{"amount":"0.40","external_no":"manual-one","reason":"reason","offline_confirmed":"true"}`, `{"amount":"0.40","external_no":"manual-one","reason":"reason"}`} {
		if _, _, e := normalizeFundsBody([]byte(body), d); e == nil {
			t.Fatal("unconfirmed offline refund accepted")
		}
	}
}
