package grouppolicy

import (
	"encoding/json"
	"testing"
)

func TestSettlementTypeSwitchLegacyValuesAndUnsupportedQQ(t *testing.T) {
	for _, tc := range []struct {
		raw               string
		override, enabled bool
	}{{"", false, false}, {`""`, false, false}, {`"_"`, false, false}, {`"0"`, true, false}, {`"1"`, true, true}, {`0`, true, false}, {`1`, true, true}} {
		got, err := ParseSettlementTypeSwitch(json.RawMessage(tc.raw))
		if err != nil || got.Override != tc.override || got.Enabled != tc.enabled {
			t.Fatal(tc, got, err)
		}
	}
	for _, raw := range []string{`null`, ` null `, `true`, `false`, `2`, `1.0`, `"true"`, `"01"`, `{}`} {
		if _, err := ParseSettlementTypeSwitch(json.RawMessage(raw)); err == nil {
			t.Fatal("malformed rail setting enabled", raw)
		}
	}
	if _, ok := SettlementTypeKey("qqpay"); ok {
		t.Fatal("QQ must not become an executable recipient rail")
	}
}
