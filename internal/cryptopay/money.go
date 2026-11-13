package cryptopay

import (
	"strconv"
	"strings"
)

const maxAmount int64 = 9000000000000000

func ParseAmount(raw string) (int64, error) {
	p := strings.Split(raw, ".")
	if len(p) > 2 || len(p[0]) == 0 || len(raw) > 24 {
		return 0, ErrInvalid
	}
	frac := ""
	if len(p) == 2 {
		frac = p[1]
		if len(frac) == 0 || len(frac) > 6 {
			return 0, ErrInvalid
		}
	}
	for _, v := range p {
		for _, c := range v {
			if c < '0' || c > '9' {
				return 0, ErrInvalid
			}
		}
	}
	n, e := strconv.ParseInt(p[0]+frac+strings.Repeat("0", 6-len(frac)), 10, 64)
	if e != nil || n <= 0 || n > maxAmount {
		return 0, ErrInvalid
	}
	return n, nil
}
func FormatAmount(n int64) string {
	return strconv.FormatInt(n/1000000, 10) + func() string {
		f := strings.TrimRight(fmtFraction(n%1000000), "0")
		if f == "" {
			return ""
		}
		return "." + f
	}()
}
func fmtFraction(n int64) string {
	s := strconv.FormatInt(n, 10)
	return strings.Repeat("0", 6-len(s)) + s
}
