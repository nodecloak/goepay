package mailer

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"time"
)

// SendTestEmail exposes a fixed original test action, never an arbitrary relay.
// Callers must durably consume their one-shot declaration before invoking it.
func (s *SMTP) SendTestEmail(ctx context.Context, to string) error {
	if s == nil || !mailbox(to) {
		return ErrSMTP
	}
	body := base64.StdEncoding.EncodeToString([]byte("SMTP测试邮件\r\n\r\n这是一封用于确认邮箱渠道配置的测试邮件。SMTP服务器接受提交不等于已送达收件箱。\r\n"))
	var wrapped string
	for len(body) > 76 {
		wrapped += body[:76] + "\r\n"
		body = body[76:]
	}
	wrapped += body + "\r\n"
	message := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s", s.headerFrom(), to, mime.BEncoding.Encode("UTF-8", "SMTP测试邮件"), time.Now().UTC().Format(time.RFC1123Z), wrapped))
	return s.send(ctx, to, message)
}
