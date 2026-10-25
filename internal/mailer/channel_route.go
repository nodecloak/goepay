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
	raw, _ := json.Marshal([]string{"smtp", s.host, s.address, s.security, s.username, s.password, s.from})
	d := sha256.Sum256(raw)
	return hex.EncodeToString(d[:])
}
func (s *SMTP) ValidateChannelNoticeRecipient(to string) error {
	if s == nil || !mailbox(to) {
		return ErrSMTP
	}
	return nil
}
