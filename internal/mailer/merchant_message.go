package mailer

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"goepay/internal/merchantnotice"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
	"time"
)

func (*SMTP) NoticeTransport() merchantnotice.Transport { return merchantnotice.SMTP }
func (s *SMTP) NoticeScope() string                     { return s.ChannelNoticeScope() }
func (s *SMTP) ValidateNoticeRecipient(to string) error { return s.ValidateChannelNoticeRecipient(to) }

func writeNoticeAlternative(w *multipart.Writer, kind, body string) error {
	part, e := w.CreatePart(textproto.MIMEHeader{"Content-Type": {kind + "; charset=UTF-8"}, "Content-Transfer-Encoding": {"base64"}})
	if e != nil {
		return e
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	var lines strings.Builder
	for len(encoded) > 76 {
		lines.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	lines.WriteString(encoded + "\r\n")
	_, e = part.Write([]byte(lines.String()))
	return e
}

// Acceptance is the SMTP server's final DATA reply, never mailbox delivery.
// The same permanent source claim must precede token/auth/network work.
func (s *SMTP) SendMerchantNotice(ctx context.Context, to string, m merchantnotice.Message) error {
	if ctx == nil || ctx.Err() != nil || !m.Valid() || s.ValidateNoticeRecipient(to) != nil {
		return ErrSMTP
	}
	var body bytes.Buffer
	alternatives := multipart.NewWriter(&body)
	if writeNoticeAlternative(alternatives, "text/plain", m.Text) != nil || writeNoticeAlternative(alternatives, "text/html", m.HTML) != nil || alternatives.Close() != nil {
		return ErrSMTP
	}
	contentType := mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": alternatives.Boundary()})
	header := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: %s\r\n\r\n", s.headerFrom(), to, mime.BEncoding.Encode("UTF-8", m.Title), time.Now().UTC().Format(time.RFC1123Z), contentType)
	message := append([]byte(header), body.Bytes()...)
	return s.send(ctx, to, message)
}

var _ merchantnotice.Sender = (*SMTP)(nil)
