package backoffice

import (
	"encoding/json"
	"errors"
	"testing"

	"goepay/internal/payment"
)

func TestTransferRouting228GenericWritersCannotBypassVersionedPolicy(t *testing.T) {
	key := payment.TransferRoutingKey228
	if !protectedPlatformSetting179(key) {
		t.Fatal("versioned routing record is exposed to generic mutation")
	}
	for _, in := range []map[string]json.RawMessage{
		{"value": json.RawMessage(`"0"`)},
		{"key": json.RawMessage(`"sitename"`)},
	} {
		if err := rejectPlatformSetting179(in, map[string]any{"key": key}); !errors.Is(err, ErrForbidden) {
			t.Fatal("existing policy could be changed/renamed", err)
		}
	}
	raw, _ := json.Marshal(key)
	if err := rejectPlatformSetting179(map[string]json.RawMessage{"key": raw}, map[string]any{}); !errors.Is(err, ErrForbidden) {
		t.Fatal("generic route creation accepted", err)
	}
}
