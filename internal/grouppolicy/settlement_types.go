package grouppolicy

import (
	"bytes"
	"encoding/json"
	"errors"
)

var ErrSettlementType = errors.New("invalid group settlement type switch")

type SettlementTypeOverride struct{ Override, Enabled bool }

// Only rails supported by the recipient vault can become executable choices.
func SettlementRecipientTypes() []string { return []string{"alipay", "wxpay", "bank"} }

func SettlementTypeKey(typ string) (string, bool) {
	switch typ {
	case "alipay", "wxpay", "bank":
		return "settle_" + typ, true
	}
	return "", false
}

// Missing/empty/_ inherit. Exact legacy numeric 0/1 are accepted on reads;
// nulls, booleans and unknown encodings fail closed.
func ParseSettlementTypeSwitch(raw json.RawMessage) (SettlementTypeOverride, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return SettlementTypeOverride{}, nil
	}
	if string(raw) == "null" {
		return SettlementTypeOverride{}, ErrSettlementType
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		value = string(raw)
	}
	switch value {
	case "", "_":
		return SettlementTypeOverride{}, nil
	case "0", "1":
		return SettlementTypeOverride{Override: true, Enabled: value == "1"}, nil
	default:
		return SettlementTypeOverride{}, ErrSettlementType
	}
}
