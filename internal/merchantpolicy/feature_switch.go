package merchantpolicy

import (
	"encoding/json"
	"errors"
)

var ErrFeatureSwitch = errors.New("invalid merchant group feature switch")

// GroupFeatureKeys deliberately excludes authentication, upstream execution and
// money-moving permissions. These controls select ordinary merchant features.
var GroupFeatureKeys = []string{"recharge", "onecode", "subchannel_open", "user_profitsharing", "user_transfer_batch"}

type FeatureOverride struct {
	Configured bool
	Enabled    bool
}

func GroupFeatureKey(key string) bool {
	for _, known := range GroupFeatureKeys {
		if key == known {
			return true
		}
	}
	return false
}

// GroupFeatureSwitch accepts the inherited empty/_ values and exact 0/1,
// including legacy numeric 0/1. Booleans, nulls and unknown values fail closed.
func GroupFeatureSwitch(body json.RawMessage, key string) (FeatureOverride, error) {
	var out FeatureOverride
	if !GroupFeatureKey(key) {
		return out, ErrFeatureSwitch
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return out, ErrFeatureSwitch
	}
	raw, exists := fields[key]
	if !exists {
		return out, nil
	}
	if string(raw) == "null" {
		return out, ErrFeatureSwitch
	}
	value, err := scalar(raw, true)
	if err != nil {
		return out, ErrFeatureSwitch
	}
	switch value {
	case "", "_":
		return out, nil
	case "0", "1":
		out.Configured, out.Enabled = true, value == "1"
		return out, nil
	default:
		return out, ErrFeatureSwitch
	}
}
