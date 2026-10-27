// Package privatesearch builds opaque candidates for encrypted original text.
// Candidates never replace verification of the decrypted original substring.
package privatesearch

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Field string

const (
	Account Field = "account"
	Name    Field = "name"
	Remark  Field = "remark"
)

type Fields map[Field]string
type Terms map[Field][]string

var ErrInvalid = errors.New("invalid private search input")
var fields = []Field{Account, Name, Remark}

func validField(f Field) bool    { return f == Account || f == Name || f == Remark }
func normalized(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
func keyFor(key []byte, purpose string) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrInvalid
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte("goepay/private-search/v1/" + purpose))
	return m.Sum(nil), nil
}
func token(key []byte, f Field, gram string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(string(f) + "\x00" + gram))
	return hex.EncodeToString(m.Sum(nil))
}
func grams(key []byte, f Field, s string, includeSingle bool) []string {
	runes := []rune(normalized(s))
	unique := map[string]bool{}
	for i := range runes {
		if includeSingle || len(runes) == 1 {
			unique[token(key, f, string(runes[i]))] = true
		}
		if i+1 < len(runes) {
			unique[token(key, f, string(runes[i:i+2]))] = true
		}
	}
	out := make([]string, 0, len(unique))
	for value := range unique {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
func Build(key []byte, in Fields) (Terms, error) {
	derived, e := keyFor(key, "terms")
	if e != nil {
		return nil, e
	}
	for f, s := range in {
		if !validField(f) || !utf8.ValidString(s) || len(s) > 4096 {
			return nil, ErrInvalid
		}
	}
	out := Terms{}
	for _, f := range fields {
		out[f] = grams(derived, f, in[f], true)
	}
	return out, nil
}
func validQuery(raw string) bool {
	return utf8.ValidString(raw) && len(raw) <= 256 && !strings.ContainsAny(raw, "\x00\r\n") && normalized(raw) != ""
}
func Query(key []byte, f Field, raw string) (Terms, error) {
	if !validField(f) || !validQuery(raw) {
		return nil, ErrInvalid
	}
	derived, e := keyFor(key, "terms")
	if e != nil {
		return nil, e
	}
	return Terms{f: grams(derived, f, raw, false)}, nil
}
func MayMatch(have, want Terms) bool {
	if len(want) == 0 {
		return false
	}
	for f, needed := range want {
		if !validField(f) || len(needed) == 0 {
			return false
		}
		set := map[string]bool{}
		for _, v := range have[f] {
			set[v] = true
		}
		for _, v := range needed {
			if !set[v] {
				return false
			}
		}
	}
	return true
}
func Match(in Fields, f Field, raw string) bool {
	return validField(f) && validQuery(raw) && strings.Contains(normalized(in[f]), normalized(raw))
}
func validTerms(in Terms) bool {
	if len(in) != len(fields) {
		return false
	}
	for _, f := range fields {
		values, ok := in[f]
		if !ok || values == nil || len(values) > 8192 {
			return false
		}
		last := ""
		for _, v := range values {
			raw, e := hex.DecodeString(v)
			if len(v) != 64 || e != nil || len(raw) != 32 || v != strings.ToLower(v) || last != "" && v <= last {
				return false
			}
			last = v
		}
	}
	return true
}
func Proof(key []byte, kind string, id, merchant int64, source []byte, terms Terms) ([]byte, error) {
	if (kind != "settlement_config" && kind != "transfer_intent") || id <= 0 || merchant <= 0 || len(source) != 32 || !validTerms(terms) {
		return nil, ErrInvalid
	}
	derived, e := keyFor(key, "proof")
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(struct {
		Version  int    `json:"version"`
		Kind     string `json:"kind"`
		ID       string `json:"id"`
		Merchant string `json:"merchant_id"`
		Source   string `json:"source_hash"`
		Terms    Terms  `json:"terms"`
	}{1, kind, strconv.FormatInt(id, 10), strconv.FormatInt(merchant, 10), hex.EncodeToString(source), terms})
	if e != nil {
		return nil, e
	}
	m := hmac.New(sha256.New, derived)
	m.Write(raw)
	return m.Sum(nil), nil
}
func Verify(key []byte, kind string, id, merchant int64, source []byte, terms Terms, proof []byte) bool {
	expected, e := Proof(key, kind, id, merchant, source, terms)
	return e == nil && len(proof) == 32 && hmac.Equal(expected, proof)
}
