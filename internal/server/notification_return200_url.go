package server

import (
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// A synchronous return is interpreted by a browser, not the webhook dialer.
// Inspect numeric hosts after browser-compatible width, dot and IPv4-number
// normalization, then apply the existing public endpoint policy. Never rewrite
// the original destination or its signed query. This does not resolve/pin DNS
// answers, so browser navigation cannot provide complete DNS-rebinding defense.
func validNotificationReturnURL200(raw string) bool {
	if raw == "" || !validURL(raw) || strings.ContainsAny(raw, "\\#") {
		return false
	}
	for _, r := range raw {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := norm.NFKC.String(strings.ToLower(parsed.Hostname()))
	host = strings.Map(func(r rune) rune {
		switch r {
		case '\u3002', '\uff0e', '\uff61':
			return '.'
		}
		// Conservatively remove ignored/combining code points when checking
		// whether an internationalized host can become a numeric IP. Normal
		// internationalized DNS names retain their original return address.
		if unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r) {
			return -1
		}
		return r
	}, host)
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return false
	}
	if !strings.Contains(host, ":") {
		var ok bool
		host, ok = notificationReturnIPv4Host200(host)
		if !ok {
			return false
		}
	}
	if parsed.Port() != "" {
		parsed.Host = net.JoinHostPort(host, parsed.Port())
	} else if strings.Contains(host, ":") {
		parsed.Host = "[" + host + "]"
	} else {
		parsed.Host = host
	}
	return validURL(parsed.String())
}

// Browser IPv4 syntax permits one to four numbers; the last number fills the
// remaining octets. Numbers may be decimal, 0-prefixed octal or 0x-prefixed hex.
func notificationReturnIPv4Host200(host string) (string, bool) {
	parts := strings.Split(host, ".")
	last := parts[len(parts)-1]
	_, numeric := notificationReturnIPv4Number200(last)
	digits := last != ""
	for _, c := range last {
		if c < '0' || c > '9' {
			digits = false
		}
	}
	if !numeric && !digits {
		return host, true
	}
	if len(parts) > 4 {
		return "", false
	}
	values := make([]uint64, len(parts))
	for i, part := range parts {
		value, ok := notificationReturnIPv4Number200(part)
		if !ok || i < len(parts)-1 && value > 255 {
			return "", false
		}
		values[i] = value
	}
	lastValue := values[len(values)-1]
	if lastValue >= uint64(1)<<(8*(5-len(values))) {
		return "", false
	}
	address := lastValue
	for i, value := range values[:len(values)-1] {
		address += value << (8 * (3 - i))
	}
	return netip.AddrFrom4([4]byte{byte(address >> 24), byte(address >> 16), byte(address >> 8), byte(address)}).String(), true
}

func notificationReturnIPv4Number200(input string) (uint64, bool) {
	if input == "" {
		return 0, false
	}
	base := 10
	if len(input) >= 2 && input[0] == '0' {
		if input[1] == 'x' || input[1] == 'X' {
			base, input = 16, input[2:]
		} else {
			base, input = 8, input[1:]
		}
	}
	if input == "" {
		return 0, true
	}
	// ParseUint permits a leading '+', while a browser IPv4 number does not.
	if input[0] == '+' || input[0] == '-' {
		return 0, false
	}
	value, err := strconv.ParseUint(input, base, 64)
	return value, err == nil
}
