package payment

import (
	"crypto/hmac"
	"encoding/hex"
	"net/mail"
	"strings"
)

// ChannelNoticeRoute is pure configuration metadata. Implementations must not
// perform DNS or other I/O while deriving or validating it. The scope is an
// opaque SHA-256 fingerprint; only a keyed derivative reaches persistence.
type ChannelNoticeRoute interface {
	ChannelNoticeMode() string
	ChannelNoticeScope() string
	ValidateChannelNoticeRecipient(string) error
}

func validChannelNoticeMode(mode string) bool {
	return mode == "smtp" || mode == "robot" || mode == "telegram" || mode == "qqbot"
}
func (s *Store) channelNoticeRoute(recipient string, sender ChannelNoticeSender) (channelNoticeClaim, error) {
	var out channelNoticeClaim
	if route, ok := sender.(ChannelNoticeRoute); ok {
		mode, scope := route.ChannelNoticeMode(), route.ChannelNoticeScope()
		raw, e := hex.DecodeString(scope)
		if !validChannelNoticeMode(mode) || e != nil || len(raw) != 32 || hex.EncodeToString(raw) != scope || route.ValidateChannelNoticeRecipient(recipient) != nil {
			return out, ErrInvalid
		}
		out.RouteVersion, out.RouteMode = 1, mode
		out.RouteScopeHash, e = s.referralDigest("channel-stop77/notice-scope", scope)
		if e != nil {
			return out, e
		}
	} else {
		address, e := mail.ParseAddress(recipient)
		if e != nil || address.Address != recipient || address.Name != "" || len(recipient) > 254 || strings.ContainsAny(recipient, "\r\n") {
			return out, ErrInvalid
		}
	}
	var e error
	out.RecipientHash, e = s.referralDigest("channel-stop76/notice-recipient", recipient)
	if e != nil {
		return out, e
	}
	if out.RouteVersion == 1 {
		out.RouteHash, e = s.channelNoticeRouteDigest(out)
	}
	return out, e
}
func (s *Store) channelNoticeRouteDigest(v channelNoticeClaim) ([]byte, error) {
	return s.referralDigest("channel-stop77/notice-route", []any{v.RouteMode, v.RouteScopeHash, v.RecipientHash})
}
func (s *Store) validChannelNoticeRoute(v channelNoticeClaim) bool {
	if v.RouteVersion == 0 {
		return v.RouteMode == "" && v.RouteScopeHash == nil && v.RouteHash == nil
	}
	if v.RouteVersion != 1 || !validChannelNoticeMode(v.RouteMode) || len(v.RouteScopeHash) != 32 || len(v.RouteHash) != 32 {
		return false
	}
	d, e := s.channelNoticeRouteDigest(v)
	return e == nil && hmac.Equal(d, v.RouteHash)
}
func sameChannelNoticeRoute(old, next channelNoticeClaim) bool {
	// Legacy proofs never contained an SMTP sending account. A same-recipient
	// SMTP replay is a read-only no-op; it cannot acquire any new sending right.
	if old.RouteVersion == 0 {
		return next.RouteVersion == 0 || next.RouteVersion == 1 && next.RouteMode == "smtp"
	}
	return next.RouteVersion == 1 && old.RouteMode == next.RouteMode && hmac.Equal(old.RouteScopeHash, next.RouteScopeHash) && hmac.Equal(old.RouteHash, next.RouteHash)
}
