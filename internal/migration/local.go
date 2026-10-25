package migration

import (
	"net"
	"net/url"
	"strings"
)

// LocalDSN accepts only explicit loopback TCP URLs. Connection-query overrides
// and service discovery are rejected so a dry-run tool cannot reach production.
func LocalDSN(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	for k := range u.Query() {
		switch strings.ToLower(k) {
		case "host", "hostaddr", "port", "service", "servicefile":
			return false
		}
	}
	return u.Fragment == ""
}
