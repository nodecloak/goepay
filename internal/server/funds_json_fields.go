package server

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"

	"goepay/internal/identity"
)

var fundsJSONUnmarshaler = reflect.TypeFor[json.Unmarshaler]()

// encoding/json accepts case-insensitive aliases for struct fields. Console
// contracts use exact JSON names: preserve arbitrary map keys and opaque raw
// bodies, but reject aliases before the business decoder can overwrite a field.
func validateFundsJSONFields(raw []byte, out any) error {
	target := reflect.TypeOf(out)
	if target == nil || target.Kind() != reflect.Pointer || reflect.ValueOf(out).IsNil() {
		return identity.ErrFundsInvalid
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return identity.ErrFundsInvalid
	}
	return validateFundsJSONValue(value, target.Elem())
}

func validateFundsJSONValue(value any, target reflect.Type) error {
	if value == nil {
		return nil
	}
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	// Custom decoders own their schema, including json.RawMessage bodies passed
	// unchanged to the authoritative, endpoint-specific normalizer.
	if target.Implements(fundsJSONUnmarshaler) || reflect.PointerTo(target).Implements(fundsJSONUnmarshaler) {
		return nil
	}
	switch target.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		} // type errors remain with encoding/json
		fields := fundsJSONFields(target)
		for key, child := range object {
			field, ok := fields[key]
			if !ok {
				return identity.ErrFundsInvalid
			}
			if err := validateFundsJSONValue(child, field); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if items, ok := value.([]any); ok {
			for _, child := range items {
				if err := validateFundsJSONValue(child, target.Elem()); err != nil {
					return err
				}
			}
		}
	case reflect.Map:
		if object, ok := value.(map[string]any); ok {
			for _, child := range object {
				if err := validateFundsJSONValue(child, target.Elem()); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type fundsJSONField struct {
	typ    reflect.Type
	depth  int
	tagged bool
}

// Match encoding/json's embedded-field dominance: shallower fields win, and
// an explicit JSON name wins at equal depth. Ambiguous fields are not accepted.
func fundsJSONFields(target reflect.Type) map[string]reflect.Type {
	candidates := map[string][]fundsJSONField{}
	var visit func(reflect.Type, int, map[reflect.Type]bool)
	visit = func(t reflect.Type, depth int, ancestors map[reflect.Type]bool) {
		if ancestors[t] {
			return
		}
		next := make(map[reflect.Type]bool, len(ancestors)+1)
		for k, v := range ancestors {
			next[k] = v
		}
		next[t] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if f.PkgPath != "" && (!f.Anonymous || ft.Kind() != reflect.Struct) {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "-" {
				continue
			}
			if f.Anonymous && tag == "" && ft.Kind() == reflect.Struct {
				visit(ft, depth+1, next)
				continue
			}
			name := tag
			if name == "" {
				name = f.Name
			}
			candidates[name] = append(candidates[name], fundsJSONField{f.Type, depth, tag != ""})
		}
	}
	visit(target, 0, map[reflect.Type]bool{})
	fields := map[string]reflect.Type{}
	for name, options := range candidates {
		best := options[0]
		matches := 1
		for _, candidate := range options[1:] {
			if candidate.depth < best.depth || candidate.depth == best.depth && candidate.tagged && !best.tagged {
				best = candidate
				matches = 1
			} else if candidate.depth == best.depth && candidate.tagged == best.tagged {
				matches++
			}
		}
		if matches == 1 {
			fields[name] = best.typ
		}
	}
	return fields
}
