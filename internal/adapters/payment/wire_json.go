package payment

import (
	"bytes"
	"encoding/json"
	"io"
)

// Reject ambiguous JSON before provider-specific interpretation.
func providerUniqueJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 32 {
			return ErrAuthentication
		}
		token, e := d.Token()
		if e != nil {
			return ErrAuthentication
		}
		delim, complex := token.(json.Delim)
		if !complex {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, e := d.Token()
				name, ok := key.(string)
				if e != nil || !ok || seen[name] {
					return ErrAuthentication
				}
				seen[name] = true
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
			end, e := d.Token()
			if e != nil || end != json.Delim('}') {
				return ErrAuthentication
			}
		case '[':
			for d.More() {
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
			end, e := d.Token()
			if e != nil || end != json.Delim(']') {
				return ErrAuthentication
			}
		default:
			return ErrAuthentication
		}
		return nil
	}
	if e := walk(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrAuthentication
	}
	return nil
}
