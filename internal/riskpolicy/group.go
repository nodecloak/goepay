package riskpolicy

// Underscore is the original UI sentinel; persisted empty means inherit.
func NormalizeGroupConfig(in map[string]string) (map[string]string, error) {
	if len(in) != 2 {
		return nil, ErrInvalid
	}
	if _, ok := in["pay_block_oversea"]; !ok {
		return nil, ErrInvalid
	}
	if _, ok := in["check_pay_regoin"]; !ok {
		return nil, ErrInvalid
	}
	out := map[string]string{"pay_block_oversea": "", "check_pay_regoin": ""}
	for key, value := range in {
		if _, ok := out[key]; !ok {
			return nil, ErrInvalid
		}
		if value == "_" {
			value = ""
		}
		if value != "" && value != "0" && value != "1" && !(key == "check_pay_regoin" && value == "2") {
			return nil, ErrInvalid
		}
		out[key] = value
	}
	return out, nil
}
