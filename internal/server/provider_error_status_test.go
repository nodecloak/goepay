package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	provider "goepay/internal/adapters/payment"
)

// Adapter validation and capability errors describe the request or the
// channel, not an outage: they answer 422 with their own message instead of
// "internal error". Unclassified errors stay internal.
func TestProviderRequestErrorsAreNotInternalFailures(t *testing.T) {
	for _, tc := range []struct {
		err     error
		status  int
		message string
	}{
		{fmt.Errorf("build refund: %w", provider.ErrInvalid), 422, "invalid provider request"},
		{fmt.Errorf("close order: %w", provider.ErrUnsupported), 422, "operation not supported by this channel"},
		{errors.New("database unavailable"), 500, "internal error"},
	} {
		w := httptest.NewRecorder()
		(&Server{}).fail(w, tc.err)
		var body struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
			t.Fatal(e)
		}
		if w.Code != tc.status || body.Code != -1 || body.Message != tc.message {
			t.Errorf("%v: got %d %q, want %d %q", tc.err, w.Code, body.Message, tc.status, tc.message)
		}
	}
}
