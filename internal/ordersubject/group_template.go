package ordersubject

import "encoding/json"

// GroupTemplate preserves the reference's null/empty inheritance. Nonempty
// values, including "0" and "_", are literal templates, never PHP-falsy flags.
func GroupTemplate(body json.RawMessage) (string, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return "", ErrInvalid
	}
	raw, exists := fields["ordername"]
	if !exists || string(raw) == "null" {
		return "", nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", ErrInvalid
	}
	if (Policy{Scope: "global", Template: value}).Validate() != nil {
		return "", ErrInvalid
	}
	return value, nil
}
