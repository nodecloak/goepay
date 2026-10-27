package server

import (
	"encoding/json"
	"testing"
)

func TestSubchannel168BAccountEditsRejectDuplicateNestedAuthorityFields(t *testing.T) {
	for _, raw := range []string{`{"app":{"operation":"keep","operation":"replace","value":"new"}}`, `{"app":{"operation":"replace","value":"one","value":"two"}}`, `{"app":{"operation":"keep"},"app":{"operation":"replace","value":"two"}}`} {
		_, e := childConsolePatch(map[string]json.RawMessage{"expected_fingerprint": json.RawMessage(`"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`), "values": json.RawMessage(raw)}, true)
		if e == nil {
			t.Fatal("duplicate private account operation accepted", raw)
		}
	}
}
