package paymentdefend206

import (
	"math/big"
	"regexp"
	"strings"
)

// Settings retains the original administrator text. Parsing does not rewrite
// leading zeros, decimal precision, UID order, or duplicate UID entries.
type Settings map[string]string

var fields = []string{"pay_verify", "pay_verify_type", "pay_verify_check_second", "pay_verify_check_count", "pay_verify_check_rate", "pay_verify_check_ip", "pay_verify_check_uid"}
var digits = regexp.MustCompile(`^[0-9]+$`)
var decimal = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

func Defaults() Settings {
	return Settings{"pay_verify": "0", "pay_verify_type": "0", "pay_verify_check_second": "", "pay_verify_check_count": "", "pay_verify_check_rate": "", "pay_verify_check_ip": "", "pay_verify_check_uid": ""}
}

// ParseRate uses an exact rational and preserves no execution assumptions about
// Pro's encrypted consumer. The local form specifies a decimal percentage but
// supplies no maximum or precision limit; 128 bytes is an input resource bound.
// An empty field means no configured decimal value and returns nil, nil.
func ParseRate(raw string) (*big.Rat, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 128 || !decimal.MatchString(raw) {
		return nil, ErrInvalid
	}
	parts := strings.SplitN(raw, ".", 2)
	numeratorText, scale := parts[0], 0
	if len(parts) == 2 {
		numeratorText += parts[1]
		scale = len(parts[1])
	}
	numerator, ok := new(big.Int).SetString(numeratorText, 10)
	if !ok || numerator.Sign() < 0 {
		return nil, ErrInvalid
	}
	denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	return new(big.Rat).SetFrac(numerator, denominator), nil
}

func Validate(s Settings) (Settings, error) {
	if len(s) != len(fields) {
		return nil, ErrInvalid
	}
	out := Defaults()
	for _, field := range fields {
		value, ok := s[field]
		if !ok || len(value) > 8192 || strings.TrimSpace(value) != value {
			return nil, ErrInvalid
		}
		out[field] = value
	}
	for _, field := range []string{"pay_verify", "pay_verify_type"} {
		if out[field] != "0" && out[field] != "1" && out[field] != "2" && out[field] != "3" {
			return nil, ErrInvalid
		}
	}
	for _, field := range []string{"pay_verify_check_second", "pay_verify_check_count", "pay_verify_check_ip"} {
		// These are numeric conditions, not IP addresses. The original text
		// form supplies no numeric maximum; the 128-byte bound is defensive.
		if raw := out[field]; raw != "" && (len(raw) > 128 || !digits.MatchString(raw)) {
			return nil, ErrInvalid
		}
	}
	if _, err := ParseRate(out["pay_verify_check_rate"]); err != nil {
		return nil, err
	}
	if raw := out["pay_verify_check_uid"]; raw != "" {
		for _, id := range strings.Split(raw, "|") {
			if !digits.MatchString(id) {
				return nil, ErrInvalid
			}
			number, ok := new(big.Int).SetString(id, 10)
			if !ok || number.Sign() <= 0 || !number.IsInt64() {
				return nil, ErrInvalid
			}
		}
	}
	return out, nil
}
