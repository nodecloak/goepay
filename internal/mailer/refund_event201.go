package mailer

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"mime/multipart"
	"time"

	"goepay/internal/refundevent201"
)

// The durable closure claim must precede this transport. A final SMTP DATA
// acceptance is submission to the configured server, not mailbox delivery.
func (s *SMTP) SendRefundEventSubscriptionClosed(ctx context.Context, to string, message refundevent201.NoticeMessage) error {
	if s == nil || ctx == nil || ctx.Err() != nil || !message.Valid() || s.ValidateNoticeRecipient(to) != nil {
		return ErrSMTP
	}
	var body bytes.Buffer
	alternatives := multipart.NewWriter(&body)
	if writeNoticeAlternative(alternatives, "text/plain", message.Text) != nil || writeNoticeAlternative(alternatives, "text/html", message.HTML) != nil || alternatives.Close() != nil {
		return ErrSMTP
	}
	contentType := mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": alternatives.Boundary()})
	header := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: %s\r\n\r\n", s.headerFrom(), to, mime.BEncoding.Encode("UTF-8", message.Title), time.Now().UTC().Format(time.RFC1123Z), contentType)
	return s.send(ctx, to, append([]byte(header), body.Bytes()...))
}

var _ refundevent201.NoticeSender = (*SMTP)(nil)
