package merchantpolicy

import (
	"encoding/json"
	"testing"
)

func TestOriginalMerchantSettlementPermissionCanonicalPrecedenceAndStrictValues(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		allowed bool
	}{{`{}`, true}, {`{"settle":1}`, true}, {`{"settle":"1"}`, true}, {`{"settle":0}`, false}, {`{"settle":"0"}`, false}, {`{"settle_status":0}`, false}, {`{"settle_status":"1"}`, true}, {`{"settle":0,"settle_status":1}`, false}, {`{"settle":1,"settle_status":0}`, true}} {
		got, e := ResolveSettlementPermission(json.RawMessage(tc.raw))
		if e != nil || got != tc.allowed {
			t.Fatal(tc.raw, got, e)
		}
	}
	for _, raw := range []string{`null`, `[]`, `{"settle":null}`, `{"settle":true}`, `{"settle":2}`, `{"settle":"01"}`, `{"settle":""}`, `{"settle":"_"}`, `{"settle_status":false}`} {
		if _, e := ResolveSettlementPermission(json.RawMessage(raw)); e == nil {
			t.Fatal("malformed original merchant permission accepted", raw)
		}
	}
}
