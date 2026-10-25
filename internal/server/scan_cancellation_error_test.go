package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"goepay/internal/payment"
)

func TestScanRecovery78KnownCancellationConflictsAreNotInternalFailures(t *testing.T) {
	for _, err := range []error{payment.ErrScanCancellationPending, payment.ErrScanCancelled} {
		w := httptest.NewRecorder()
		(&Server{}).fail(w, err)
		var body struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
			t.Fatal(e)
		}
		if w.Code != http.StatusConflict || body.Code != -1 || body.Message == "internal error" {
			t.Fatal("known original cancellation was treated as an internal failure", w.Code, body.Message)
		}
	}
}
