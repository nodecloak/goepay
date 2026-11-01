package merchantpolicy

import (
	"encoding/json"
	"testing"
)

func TestCombinePolicyExactInheritanceZeroAndInvalidGroup(t *testing.T) {
	global := map[string]string{"wxcombine_open": "1", "wxcombine_minmoney": "100", "wxcombine_submoney": "50"}
	for _, tc := range []struct {
		body string
		on   bool
		min  int64
		sub  int64
	}{
		{`{}`, true, 10000, 5000},
		{`{"wxcombine_open":"_","wxcombine_minmoney":"","wxcombine_submoney":"_"}`, true, 10000, 5000},
		{`{"wxcombine_open":"0","wxcombine_minmoney":"0","wxcombine_submoney":"0"}`, false, 0, 0},
		{`{"wxcombine_open":1,"wxcombine_minmoney":0.29,"wxcombine_submoney":"92233720368547758.07"}`, true, 29, 9223372036854775807},
	} {
		policy, e := ResolveCombinePolicy(global, json.RawMessage(tc.body))
		if e != nil || policy.Enabled != tc.on || policy.MinCents != tc.min || policy.SubCents != tc.sub {
			t.Errorf("policy=%+v err=%v body=%s", policy, e, tc.body)
		}
	}
	for _, body := range []string{`null`, `[]`, `{"wxcombine_open":null}`, `{"wxcombine_open":true}`, `{"wxcombine_open":"2"}`, `{"wxcombine_minmoney":true}`, `{"wxcombine_minmoney":"-1"}`, `{"wxcombine_submoney":"0.001"}`, `{"wxcombine_minmoney":"92233720368547758.08"}`} {
		if _, e := ResolveCombinePolicy(global, json.RawMessage(body)); e == nil {
			t.Error("invalid group combined payment policy accepted", body)
		}
	}
	legacy, e := ResolveCombinePolicy(map[string]string{"wxcombine_open": "old", "wxcombine_minmoney": "bad", "wxcombine_submoney": "bad"}, json.RawMessage(`{}`))
	if e != nil || legacy.Enabled || legacy.MinCents != 0 || legacy.SubCents != 0 {
		t.Fatal("legacy global malformed defaults changed", legacy, e)
	}
	missing, _ := ResolveCombinePolicy(nil, json.RawMessage(`{}`))
	explicit, _ := ResolveCombinePolicy(map[string]string{"wxcombine_submoney": "0"}, json.RawMessage(`{}`))
	if missing.globalHash == explicit.globalHash {
		t.Error("missing and explicit global zero snapshots were merged")
	}
}
