package adminnotice

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
)

// Scope fingerprints stay inside the application boundary. The store wraps
// them in a keyed digest before persistence; no raw credentials enter a claim.
func routeScope(fields ...string) string {
	raw, _ := json.Marshal(fields)
	d := sha256.Sum256(raw)
	return hex.EncodeToString(d[:])
}
func (*Webhook) ChannelNoticeMode() string { return "robot" }
func (w *Webhook) ChannelNoticeScope() string {
	if w == nil {
		return ""
	}
	return routeScope("robot", w.endpoint)
}
func (w *Webhook) ValidateChannelNoticeRecipient(to string) error {
	if w == nil || to != w.target {
		return ErrDelivery
	}
	return nil
}
func (*Telegram) ChannelNoticeMode() string { return "telegram" }
func (t *Telegram) ChannelNoticeScope() string {
	if t == nil {
		return ""
	}
	return routeScope("telegram", t.endpoint)
}
func (t *Telegram) ValidateChannelNoticeRecipient(to string) error {
	if t == nil {
		return ErrDelivery
	}
	id, e := strconv.ParseInt(to, 10, 64)
	if (e != nil || id == 0 || strconv.FormatInt(id, 10) != to) && !telegramRecipientName.MatchString(to) {
		return ErrDelivery
	}
	return nil
}
func (*QQBot) ChannelNoticeMode() string { return "qqbot" }
func (q *QQBot) ChannelNoticeScope() string {
	if q == nil {
		return ""
	}
	return routeScope("qqbot", q.appID, q.secret)
}
func (q *QQBot) ValidateChannelNoticeRecipient(to string) error {
	if q == nil || !qqIdentity.MatchString(to) {
		return ErrDelivery
	}
	return nil
}
