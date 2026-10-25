package backoffice

import (
	"fmt"
	"net"
	"net/url"
	"time"
)

// ActiveBlacklistReason matches an already enabled stored rule using the same
// expiry, IP/CIDR and account semantics as payment admission. Callers select
// enabled blacklist records; malformed historical expiry remains active.
func ActiveBlacklistReason(rule map[string]any, p PaymentContext, now time.Time) string {
	if blacklistExpired(rule, now) {
		return ""
	}
	value := fmt.Sprint(rule["value"])
	switch rule["kind"] {
	case "ip":
		ip := net.ParseIP(p.ClientIP)
		if ip != nil {
			if other := net.ParseIP(value); other != nil && ip.Equal(other) {
				return "client IP blocked"
			}
			if _, network, e := net.ParseCIDR(value); e == nil && network.Contains(ip) {
				return "client network blocked"
			}
		}
	case "buyer":
		if p.Buyer != "" && p.Buyer == value {
			return "buyer blocked"
		}
	case "domain":
		for _, raw := range []string{p.NotifyURL, p.ReturnURL} {
			u, e := url.Parse(raw)
			if e == nil && domainMatches(u.Hostname(), value) {
				return "callback domain blocked"
			}
		}
	}
	return ""
}
