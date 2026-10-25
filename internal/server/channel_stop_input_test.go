package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChannelStop76ResumeRequiresExactIndependentConfirmation(t *testing.T) {
	d, ok := fundsTarget("/console/v1/admin/channel-stops/7/resume")
	if !ok || d.guard != "channel-resume" || !mandatoryFundsAuthorization(d) {
		t.Fatal("channel resume missing mandatory independent confirmation", d, ok)
	}
	good := `{"expected_event_id":"31","expected_revision":"1"}`
	if _, _, e := normalizeFundsBody(json.RawMessage(good), d); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{`{}`, `{"expected_event_id":"31"}`, strings.Replace(good, `"31"`, `31`, 1), strings.Replace(good, `"1"`, `"01"`, 1), strings.Replace(good, `"1"`, `"4096"`, 1), strings.Replace(good, `"31"`, `"0"`, 1), strings.TrimSuffix(good, "}") + `,"enabled":true}`, `{"expected_event_id":"31","expected_event_id":"32","expected_revision":"1"}`} {
		if _, _, e := normalizeFundsBody(json.RawMessage(bad), d); e == nil {
			t.Fatal("invalid resume confirmation accepted", bad)
		}
	}
	for _, bad := range []string{"/console/v1/merchant/channel-stops/7/resume", "/console/v1/admin/channel-stops/07/resume", "/console/v1/admin/channel-stops/7/resume?enabled=true", "/console/v1/admin/channel-stops/7"} {
		if _, ok := fundsTarget(bad); ok {
			t.Fatal("invalid resume target", bad)
		}
	}
}
