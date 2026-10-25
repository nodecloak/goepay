// Package ordersubject resolves an immutable upstream title. It never changes
// the order name used for merchant signatures, notifications or idempotency.
package ordersubject

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid upstream subject policy")

type Rule struct {
	Name     string
	Min, Max int64
}
type Policy struct {
	Scope               string
	ScopeID             int64
	Template, RulesText string
}
type Variables struct {
	Name, TradeNo, OutTradeNo, QQ, Phone string
	Now                                  time.Time
}

func validText(s string) bool { return utf8.ValidString(s) && !strings.ContainsAny(s, "\x00\r\n") }
func validateTemplate(s string) error {
	if len(s) > 1024 || !validText(s) {
		return ErrInvalid
	}
	a := strings.Split(s, "|")
	if len(a) > 16 {
		return ErrInvalid
	}
	for _, v := range a {
		if strings.TrimSpace(v) == "" {
			return ErrInvalid
		}
	}
	return nil
}
func (p Policy) Validate() error {
	if p.Scope != "global" && p.Scope != "merchant" && p.Scope != "channel" || p.Scope == "global" && p.ScopeID != 0 || p.Scope != "global" && p.ScopeID <= 0 {
		return ErrInvalid
	}
	if p.Template != "" {
		if e := validateTemplate(p.Template); e != nil {
			return e
		}
	}
	if p.RulesText != "" {
		_, e := ParseRules(p.RulesText)
		return e
	}
	return nil
}
func money(s string) (int64, error) {
	if s == "" || len(s) > 18 {
		return 0, ErrInvalid
	}
	a := strings.Split(s, ".")
	if len(a) > 2 || a[0] == "" || len(a[0]) > 1 && a[0][0] == '0' {
		return 0, ErrInvalid
	}
	for _, v := range a {
		if v == "" {
			return 0, ErrInvalid
		}
		for _, c := range v {
			if c < '0' || c > '9' {
				return 0, ErrInvalid
			}
		}
	}
	if len(a) == 2 && (len(a[1]) < 1 || len(a[1]) > 2) {
		return 0, ErrInvalid
	}
	whole, e := strconv.ParseInt(a[0], 10, 64)
	if e != nil || whole > 92233720368547758 {
		return 0, ErrInvalid
	}
	fraction := int64(0)
	if len(a) == 2 {
		v := a[1]
		if len(v) == 1 {
			v += "0"
		}
		fraction, _ = strconv.ParseInt(v, 10, 64)
	}
	if whole == 92233720368547758 && fraction > 7 {
		return 0, ErrInvalid
	}
	return whole*100 + fraction, nil
}

// ParseRules requires JSON text, exact decimal strings and disjoint inclusive
// ranges. Duplicate keys are rejected before they can be lost by json.Unmarshal.
func ParseRules(raw string) ([]Rule, error) {
	if len(raw) > 20000 {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(strings.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('[') {
		return nil, ErrInvalid
	}
	rules := []Rule{}
	for d.More() {
		if len(rules) >= 100 {
			return nil, ErrInvalid
		}
		t, e = d.Token()
		if e != nil || t != json.Delim('{') {
			return nil, ErrInvalid
		}
		m := map[string]string{}
		for d.More() {
			k, e := d.Token()
			key, ok := k.(string)
			if e != nil || !ok {
				return nil, ErrInvalid
			}
			if _, ok = m[key]; ok {
				return nil, ErrInvalid
			}
			if key != "name" && key != "min" && key != "max" {
				return nil, ErrInvalid
			}
			var v string
			if e = d.Decode(&v); e != nil {
				return nil, ErrInvalid
			}
			m[key] = v
		}
		t, e = d.Token()
		if e != nil || t != json.Delim('}') || len(m) != 3 {
			return nil, ErrInvalid
		}
		if e = validateTemplate(m["name"]); e != nil {
			return nil, e
		}
		lo, e := money(m["min"])
		if e != nil {
			return nil, e
		}
		hi, e := money(m["max"])
		if e != nil || lo > hi {
			return nil, ErrInvalid
		}
		rules = append(rules, Rule{Name: m["name"], Min: lo, Max: hi})
	}
	t, e = d.Token()
	if e != nil || t != json.Delim(']') {
		return nil, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrInvalid
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Min < rules[j].Min })
	for i := 1; i < len(rules); i++ {
		if rules[i].Min <= rules[i-1].Max {
			return nil, ErrInvalid
		}
	}
	return rules, nil
}

func Render(template string, v Variables) (string, error) {
	if e := validateTemplate(template); e != nil {
		return "", e
	}
	choices := strings.Split(template, "|")
	index := 0
	if len(choices) > 1 {
		n, e := rand.Int(rand.Reader, big.NewInt(int64(len(choices))))
		if e != nil {
			return "", e
		}
		index = int(n.Int64())
	}
	s := choices[index]
	// This order matches the plaintext Release helper; no regular expressions or
	// user-supplied code are evaluated. Bound every intermediate allocation.
	pairs := [][2]string{{"[name]", v.Name}, {"[order]", v.TradeNo}, {"[outorder]", v.OutTradeNo}, {"[qq]", v.QQ}, {"[phone]", v.Phone}, {"[time]", strconv.FormatInt(v.Now.Unix(), 10)}}
	for _, p := range pairs {
		if p[0] == "[outorder]" && (p[1] == "" || p[1] == "0") {
			continue
		}
		count := strings.Count(s, p[0])
		if len(s)+count*(len(p[1])-len(p[0])) > 4096 {
			return "", ErrInvalid
		}
		s = strings.ReplaceAll(s, p[0], p[1])
	}
	if strings.TrimSpace(s) == "" || len(s) > 128 || !validText(s) {
		return "", ErrInvalid
	}
	return s, nil
}
