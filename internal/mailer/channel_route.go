package mailer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func (*SMTP) ChannelNoticeMode() string { return "smtp" }
func (s *SMTP) ChannelNoticeScope() string {
	if s == nil {
		return ""
	}
	fields := []string{"smtp", s.host, s.address, s.security, s.username, s.password, s.from}
	if s.fromName != "" || s.replyTo != "" {
		fields = append(fields, s.fromName, s.replyTo)
	}
	raw, _ := json.Marshal(fields)
	d := sha256.Sum256(raw)
	return hex.EncodeToString(d[:])
}
func (s *SMTP) ValidateChannelNoticeRecipient(to string) error {
	if s == nil || !mailbox(to) {
		return ErrSMTP
	}
	return nil
}
