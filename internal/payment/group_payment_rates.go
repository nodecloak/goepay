package payment

import (
	"bytes"
	"encoding/json"
	"goepay/internal/ledger"
	"io"
)

// ParseGroupRetainedRates validates the supported two-level original rate
// shape. Keys are stable Go payment types, never unrelated directory IDs.
// Zero and empty mean inheritance, matching the original channel rate rule.
func ParseGroupRetainedRates(raw []byte) (map[string]int64, error) {
	if len(raw) > 4096 {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	start, e := d.Token()
	if e != nil || start != json.Delim('{') {
		return nil, ErrInvalid
	}
	out, seen := map[string]int64{}, map[string]bool{}
	for d.More() {
		token, e := d.Token()
		kind, ok := token.(string)
		if e != nil || !ok || seen[kind] || (kind != "alipay" && kind != "wxpay" && kind != "sandbox") {
			return nil, ErrInvalid
		}
		seen[kind] = true
		start, e = d.Token()
		if e != nil || start != json.Delim('{') {
			return nil, ErrInvalid
		}
		hasRate := false
		for d.More() {
			key, e := d.Token()
			if e != nil || key != "rate" || hasRate {
				return nil, ErrInvalid
			}
			hasRate = true
			token, e := d.Token()
			value, ok := token.(string)
			if e != nil || !ok {
				return nil, ErrInvalid
			}
			if value == "" {
				continue
			}
			n, e := ledger.Parse(value)
			if e != nil || n > 10000 {
				return nil, ErrInvalid
			}
			if n > 0 {
				out[kind] = n
			}
		}
		end, e := d.Token()
		if e != nil || end != json.Delim('}') {
			return nil, ErrInvalid
		}
	}
	end, e := d.Token()
	if e != nil || end != json.Delim('}') {
		return nil, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrInvalid
	}
	return out, nil
}
