package mailer

import (
	"context"
	"encoding/base64"
	"fmt"
	"goepay/internal/templatechange"
	"mime"
	"strings"
	"time"
)

// SendPageTemplateNotice submits a bounded, non-PII administrator notice that
// an uploaded page template changed. A nil error means SMTP DATA acceptance
// only, never inbox delivery; the caller must commit its one-shot claim before
// invoking this. The subject is the notice title and the body is its text
// lines. The template source and uploaded files are never included.
func (s *SMTP) SendPageTemplateNotice(ctx context.Context, to string, n templatechange.Notice) error {
	if s == nil || !mailbox(to) {
		return ErrSMTP
	}
	title, text, e := n.Message()
	if e != nil {
		return ErrSMTP
	}
	body := strings.ReplaceAll(text, "\n", "\r\n") + "\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	var lines strings.Builder
	for len(encoded) > 76 {
		lines.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	lines.WriteString(encoded + "\r\n")
	message := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s", s.headerFrom(), to, mime.BEncoding.Encode("UTF-8", title), time.Now().UTC().Format(time.RFC1123Z), lines.String()))
	return s.send(ctx, to, message)
}
