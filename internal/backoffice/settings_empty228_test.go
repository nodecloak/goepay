package backoffice

import (
	"encoding/json"
	"testing"
)

func TestSettings228ClearOptionalValuesWithoutClearingRequiredSwitches(t *testing.T) {
	for _, key := range []string{"blockname", "blockalert", "blackmsg", "pay_force_cashier_pids", "user_deposit_min", "user_deposit_day", "wxnotice_tpl_order_no", "wxnotice_tpl_order"} {
		t.Run(key, func(t *testing.T) {
			raw := map[string]json.RawMessage{"value": json.RawMessage(`""`)}
			current := map[string]any{"key": key, "value": "existing"}
			out, err := validate(Principal{Role: "admin"}, definitions["settings"], raw, current, false)
			if err != nil || out["value"] != "" {
				t.Fatal("clear optional value", err)
			}
			if err = validateSpecific("settings", out); err != nil {
				t.Fatal("consumer rejected clear", err)
			}
		})
	}
	for _, key := range []string{"user_deposit", "registration_open"} {
		if _, err := validate(Principal{Role: "admin"}, definitions["settings"], map[string]json.RawMessage{"value": json.RawMessage(`""`)}, map[string]any{"key": key, "value": "1"}, false); err == nil {
			t.Fatal("required setting accepted empty", key)
		}
	}
}
