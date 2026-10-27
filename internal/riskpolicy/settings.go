// Package riskpolicy defines the original payment-risk field contract. It has
// no database, network, payment or authority side effects.
package riskpolicy

import (
	"errors"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"goepay/internal/ledger"
)

var ErrInvalid = errors.New("invalid core payment risk setting")

type Settings map[string]string
type field struct {
	name, kind string
	max        int64
}

var fields = []field{
	{"pay_iplimit", "integer", 1000000}, {"pay_ip_interval", "integer", 31536000}, {"pay_iplimit_white", "ips", 0}, {"pay_userlimit", "integer", 1000000}, {"pay_daymoney", "money", 999999999999}, {"pay_region_block", "regions", 0}, {"pay_block_oversea", "flag", 1}, {"check_pay_regoin", "scan", 2},
	{"auto_check_payip", "flag", 1}, {"check_payip_second", "integer", 31536000}, {"check_payip_count", "integer", 1000000},
	{"auto_check_payspeed", "flag", 1}, {"check_payspeed_second", "integer", 31536000}, {"check_payspeed_count", "integer", 1000000}, {"check_payspeed_days", "integer", 3650},
	{"auto_check_sucrate", "flag", 1}, {"check_sucrate_second", "integer", 31536000}, {"check_sucrate_count", "integer", 1000000}, {"check_sucrate_value", "percent", 10000}, {"check_sucrate_notice", "flag", 1},
	{"auto_check_channel", "flag", 1}, {"check_channel_second", "integer", 31536000}, {"check_channel_failcount", "integer", 1000000}, {"check_channel_ids", "ids", 0}, {"check_channel_notice", "flag", 1},
}

func Defaults() Settings {
	out := Settings{}
	for _, f := range fields {
		out[f.name] = ""
		if f.kind == "flag" || f.kind == "scan" {
			out[f.name] = "0"
		}
	}
	return out
}

var integer = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
var decimal = regexp.MustCompile(`^(0|[1-9][0-9]*)([.][0-9]{1,2})?$`)

func Normalize(values Settings) (Settings, error) {
	if len(values) != len(fields) {
		return nil, ErrInvalid
	}
	out := Settings{}
	for _, f := range fields {
		v, ok := values[f.name]
		if !ok || len(v) > 8192 || !utf8.ValidString(v) || strings.ContainsAny(v, "\x00\r\n") {
			return nil, ErrInvalid
		}
		switch f.kind {
		case "scan":
			if v != "0" && v != "1" && v != "2" {
				return nil, ErrInvalid
			}
		case "flag":
			if v != "0" && v != "1" {
				return nil, ErrInvalid
			}
		case "integer":
			if v != "" {
				n, e := strconv.ParseInt(v, 10, 64)
				if e != nil || !integer.MatchString(v) || n > f.max {
					return nil, ErrInvalid
				}
				if n == 0 {
					v = ""
				}
			}
		case "money", "percent":
			if v != "" {
				n, e := ledger.Parse(v)
				if e != nil || !decimal.MatchString(v) || n > f.max {
					return nil, ErrInvalid
				}
				v = ledger.Format(n)
				if n == 0 {
					v = ""
				}
			}
		case "ips", "ids", "regions":
			if v != "" {
				separator := "|"
				if f.kind == "ids" {
					separator = ","
				}
				parts := strings.Split(v, separator)
				if len(parts) > 128 {
					return nil, ErrInvalid
				}
				seen := map[string]bool{}
				list := []string{}
				for _, part := range parts {
					part = strings.TrimSpace(part)
					if part == "" {
						return nil, ErrInvalid
					}
					switch f.kind {
					case "ips":
						ip, e := CanonicalIP(part)
						if e != nil {
							return nil, e
						}
						part = ip.String()
					case "ids":
						n, e := strconv.ParseInt(part, 10, 64)
						if e != nil || n < 1 || strconv.FormatInt(n, 10) != part {
							return nil, ErrInvalid
						}
					case "regions":
						if utf8.RuneCountInString(part) > 128 {
							return nil, ErrInvalid
						}
						for _, r := range part {
							if r < 32 || r == 127 {
								return nil, ErrInvalid
							}
						}
					}
					if !seen[part] {
						seen[part] = true
						list = append(list, part)
					}
				}
				sort.Strings(list)
				v = strings.Join(list, separator)
			}
		}
		out[f.name] = v
	}
	for _, rule := range []struct {
		flag     string
		required []string
	}{{"auto_check_payip", []string{"check_payip_second", "check_payip_count"}}, {"auto_check_payspeed", []string{"check_payspeed_second", "check_payspeed_count"}}, {"auto_check_sucrate", []string{"check_sucrate_second", "check_sucrate_count", "check_sucrate_value"}}, {"auto_check_channel", []string{"check_channel_second", "check_channel_failcount"}}} {
		if out[rule.flag] == "1" {
			for _, key := range rule.required {
				if out[key] == "" {
					return nil, ErrInvalid
				}
			}
		}
	}
	return out, nil
}
func CanonicalIP(raw string) (netip.Addr, error) {
	ip, e := netip.ParseAddr(raw)
	if e != nil || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() {
		return netip.Addr{}, ErrInvalid
	}
	return ip.Unmap(), nil
}
func IPWhitelisted(s Settings, raw string) bool {
	ip, e := CanonicalIP(raw)
	if e != nil {
		return false
	}
	for _, part := range strings.Split(s["pay_iplimit_white"], "|") {
		if part == ip.String() {
			return true
		}
	}
	return false
}
func Integer(s Settings, key string) int64 { n, _ := strconv.ParseInt(s[key], 10, 64); return n }
func Amount(s Settings, key string) int64  { n, _ := ledger.Parse(s[key]); return n }
