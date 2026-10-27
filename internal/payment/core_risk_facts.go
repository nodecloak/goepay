package payment

import (
	"bytes"
	"encoding/json"
	"io"
)

// Private facts accept exactly their writer's shape. encoding/json otherwise
// accepts case aliases and ignores unknown fields, which must not be able to
// hide beside an authenticated field in a persisted JSONB object.
func decodeCoreRiskFacts(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return ErrConflict
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrConflict
	}
	canonical, e := json.Marshal(out)
	if e != nil {
		return e
	}
	normalize := func(b []byte) ([]byte, error) {
		var v any
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber()
		if e := d.Decode(&v); e != nil {
			return nil, e
		}
		return json.Marshal(v)
	}
	a, e := normalize(raw)
	if e != nil {
		return e
	}
	b, e := normalize(canonical)
	if e != nil {
		return e
	}
	if !bytes.Equal(a, b) {
		return ErrConflict
	}
	return nil
}
