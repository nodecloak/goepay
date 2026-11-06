package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"time"
)

type transportPeerKey struct{}

// TransportPeer returns the original TCP peer when the trusted proxy wrapper is
// installed. Business rules should use the wrapper's canonical RemoteAddr.
func TransportPeer(r *http.Request) string {
	if peer, ok := r.Context().Value(transportPeerKey{}).(string); ok {
		return peer
	}
	return r.RemoteAddr
}

// NewTrustedProxy trusts only explicitly configured direct proxy prefixes and a
// single proxy-overwritten X-Real-IP. It never interprets a client supplied chain.
func NewTrustedProxy(raw string) (func(http.Handler) http.Handler, error) {
	var prefixes []netip.Prefix
	if strings.TrimSpace(raw) != "" {
		if len(raw) > 8192 {
			return nil, errors.New("trusted proxy configuration too large")
		}
		for _, part := range strings.Split(raw, ",") {
			prefix, e := netip.ParsePrefix(strings.TrimSpace(part))
			if e != nil || prefix.Bits() == 0 || prefix.Addr().Is4In6() {
				return nil, errors.New("explicit IPv4 or IPv6 trusted proxy CIDRs required; /0 forbidden")
			}
			prefixes = append(prefixes, prefix.Masked())
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			original := r.RemoteAddr
			host, port, e := net.SplitHostPort(original)
			peer, parseErr := netip.ParseAddr(host)
			trusted := false
			if e == nil && parseErr == nil && peer.Zone() == "" {
				peer = peer.Unmap()
				for _, prefix := range prefixes {
					if prefix.Contains(peer) {
						trusted = true
						break
					}
				}
			}
			if trusted {
				values := r.Header.Values("X-Real-IP")
				var client netip.Addr
				if len(values) == 1 && values[0] == strings.TrimSpace(values[0]) {
					client, _ = netip.ParseAddr(values[0])
				}
				if client.Zone() == "" {
					client = client.Unmap()
				}
				if !client.IsValid() || client.Zone() != "" || client.IsUnspecified() || client.IsMulticast() {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.Header().Set("Cache-Control", "no-store")
					w.WriteHeader(400)
					_ = json.NewEncoder(w).Encode(map[string]any{"code": -1, "message": "invalid trusted proxy client IP", "data": nil})
					return
				}
				r = r.Clone(context.WithValue(r.Context(), transportPeerKey{}, original))
				r.RemoteAddr = net.JoinHostPort(client.Unmap().String(), port)
			} else {
				if e == nil && parseErr == nil && (r.Header.Get("X-Real-IP") != "" || r.Header.Get("X-Forwarded-For") != "") {
					noteUntrustedProxy(peer.Unmap(), time.Now())
				}
				r = r.WithContext(context.WithValue(r.Context(), transportPeerKey{}, original))
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

// A private or loopback peer that forwards X-Real-IP/X-Forwarded-For is almost
// certainly a reverse proxy missing from GOEPAY_TRUSTED_PROXY_CIDRS. Its headers
// stay ignored (fail closed); the hint only makes the shared source budget
// visible, because every client behind that proxy then shares one login,
// CAPTCHA and payment-risk identity. Public peers are clients and never logged.
var untrustedProxyLastHint atomic.Int64

var untrustedProxyHint = func(peer netip.Addr) {
	slog.Warn("ignored client address headers from an untrusted reverse proxy; IP-based login, CAPTCHA and payment limits treat all of its clients as one source. Add this proxy to GOEPAY_TRUSTED_PROXY_CIDRS and let it overwrite X-Real-IP", "peer", peer.String())
}

func noteUntrustedProxy(peer netip.Addr, now time.Time) {
	if !peer.IsValid() || peer.Zone() != "" || !(peer.IsPrivate() || peer.IsLoopback()) {
		return
	}
	last := untrustedProxyLastHint.Load()
	if last != 0 && now.Sub(time.Unix(0, last)) < 10*time.Minute {
		return
	}
	if untrustedProxyLastHint.CompareAndSwap(last, now.UnixNano()) {
		untrustedProxyHint(peer)
	}
}
