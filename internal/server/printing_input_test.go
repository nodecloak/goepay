package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrintingBodyRejectsImplicitOrAmbiguousSettings(t *testing.T) {
	const valid = `{"expected_revision":"0","mode":0,"copies":1,"voice":false,"device_action":"keep","device_id":""}`
	for _, body := range []string{
		strings.Replace(valid, `"expected_revision":"0"`, `"expected_revision":0`, 1),
		strings.Replace(valid, `"expected_revision":"0"`, `"expected_revision":"00"`, 1),
		strings.Replace(valid, `"expected_revision":"0"`, `"expected_revision":"9223372036854775808"`, 1),
		strings.Replace(valid, `"mode":0`, `"mode":null`, 1),
		strings.Replace(valid, `"voice":false`, `"voice":null`, 1),
		strings.Replace(valid, `"device_id":""`, `"device_id":"","merchant_id":"2"`, 1),
		strings.Replace(valid, `"copies":1`, `"copies":1,"copies":5`, 1),
		strings.Replace(valid, `,"device_id":""`, "", 1),
	} {
		r := httptest.NewRequest("PUT", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		var out map[string]any
		if printingBody(w, r, "merchant", &out) {
			t.Fatalf("ambiguous printing settings accepted: %s", body)
		}
	}
	r := httptest.NewRequest("PUT", "/", strings.NewReader(valid))
	r.Header.Set("Content-Type", "application/json")
	if !printingBody(httptest.NewRecorder(), r, "merchant", &map[string]any{}) {
		t.Fatal("explicit zero mode rejected")
	}
}

func TestPrintingManualBodyRequiresExplicitOriginalRetryChoice(t *testing.T) {
	const valid = `{"external_no":"local-print-1","previous_job_id":"0","acknowledge_duplicate":false}`
	for _, body := range []string{
		`{"external_no":"local-print-1"}`,
		strings.Replace(valid, `"previous_job_id":"0"`, `"previous_job_id":null`, 1),
		strings.Replace(valid, `"acknowledge_duplicate":false`, `"acknowledge_duplicate":"false"`, 1),
		strings.Replace(valid, `"external_no":"local-print-1"`, `"external_no":"local-print-1","order_id":"2"`, 1),
	} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if printingBody(httptest.NewRecorder(), r, "manual", &map[string]any{}) {
			t.Fatalf("ambiguous manual printing accepted: %s", body)
		}
	}
}
