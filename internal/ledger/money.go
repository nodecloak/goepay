package ledger

import (
	"errors"
	"strconv"
	"strings"
)

var ErrAmount = errors.New("invalid amount")

func Parse(raw string) (int64, error) {
	p := strings.Split(raw, ".")
	if len(p) > 2 || len(p[0]) == 0 {
		return 0, ErrAmount
	}
	for _, c := range p[0] {
		if c < '0' || c > '9' {
			return 0, ErrAmount
		}
	}
	frac := "00"
	if len(p) == 2 {
		if len(p[1]) < 1 || len(p[1]) > 2 {
			return 0, ErrAmount
		}
		for _, c := range p[1] {
			if c < '0' || c > '9' {
				return 0, ErrAmount
			}
		}
		frac = p[1]
		if len(frac) == 1 {
			frac += "0"
		}
	}
	n, e := strconv.ParseInt(p[0]+frac, 10, 64)
	if e != nil {
		return 0, ErrAmount
	}
	return n, nil
}
func Format(cents int64) string {
	negative := cents < 0
	n := uint64(cents)
	if negative {
		n = uint64(-(cents + 1)) + 1
	}
	s := strconv.FormatUint(n/100, 10) + "."
	if n%100 < 10 {
		s += "0"
	}
	s += strconv.FormatUint(n%100, 10)
	if negative {
		s = "-" + s
	}
	return s
}
