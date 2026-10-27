package payment

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
)

// Keep the existing Marshal/MAC representation. Decoder field folding must
// not make a SQL-added case alias look like the original authenticated fact.
// Parent execution facts are flat structs with primitive and slice values.
func parentFactsJSON(raw []byte, target any) error {
	t := reflect.TypeOf(target)
	if t == nil || t.Kind() != reflect.Pointer || t.Elem().Kind() != reflect.Struct || reflect.ValueOf(target).IsNil() {
		return ErrConflict
	}
	allowed := map[string]bool{}
	for i := 0; i < t.Elem().NumField(); i++ {
		f := t.Elem().Field(i)
		if f.PkgPath != "" {
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		allowed[name] = true
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return ErrConflict
	}
	seen := map[string]bool{}
	for d.More() {
		token, e = d.Token()
		name, ok := token.(string)
		if e != nil || !ok || !allowed[name] || seen[name] {
			return ErrConflict
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return ErrConflict
		}
	}
	if token, e = d.Token(); e != nil || token != json.Delim('}') {
		return ErrConflict
	}
	if _, e = d.Token(); e != io.EOF {
		return ErrConflict
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil {
		return ErrConflict
	}
	return nil
}
