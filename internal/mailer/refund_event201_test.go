package mailer

import (
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"

	"goepay/internal/refundevent"
)

type refundEventClosedSender201 interface {
	SendRefundEventSubscriptionClosed(context.Context, string, refundevent.NoticeMessage) error
}

func TestRefundEvent201ClosedNoticeUsesVerifiedTLSAndRejectsUnsealedOrInjectedMailBeforeIO(t *testing.T) {
	for _, security := range []string{"implicit", "starttls"} {
		t.Run(security, func(t *testing.T) {
			f := localSMTP(t, security, "")
			smtp, err := NewSMTP(f.config)
			if err != nil {
				t.Fatal(err)
			}
			sender, ok := any(smtp).(refundEventClosedSender201)
			if !ok {
				t.Fatal("configured SMTP has no sealed refund subscription closure transport")
			}
			message, err := refundevent.RenderClosedNotice("本地易支付201", "https://events.example.invalid/refund?local=201", "receiver acknowledgement missing", time.Date(2026, 10, 27, 1, 2, 3, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			changed := message
			changed.HTML = "<script>changed201</script>"
			for _, item := range []struct {
				to      string
				message refundevent.NoticeMessage
			}{{"local@example.test", changed}, {"local@example.test", refundevent.NoticeMessage{}}, {"local@example.test\r\nBcc:injected@example.test", message}} {
				if err = sender.SendRefundEventSubscriptionClosed(t.Context(), item.to, item.message); err == nil {
					t.Fatal("unsafe closure notice accepted")
				}
			}
			if f.connections.Load() != 0 {
				t.Fatal("unsafe message validation started SMTP I/O")
			}
			if err = sender.SendRefundEventSubscriptionClosed(t.Context(), "local@example.test", message); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.messages) != 1 || f.dataAttempts.Load() != 1 {
				t.Fatal("closure notice was not a single SMTP DATA submission")
			}
			wire, err := mail.ReadMessage(strings.NewReader(f.messages[0]))
			if err != nil || wire.Header.Get("Bcc") != "" {
				t.Fatal("invalid closure mail envelope", err)
			}
			title, err := new(mime.WordDecoder).DecodeHeader(wire.Header.Get("Subject"))
			if err != nil || title != message.Title {
				t.Fatal("original closure notice subject changed", err)
			}
			kind, params, err := mime.ParseMediaType(wire.Header.Get("Content-Type"))
			if err != nil || kind != "multipart/alternative" {
				t.Fatal("missing safe plain/HTML alternatives", err)
			}
			reader := multipart.NewReader(wire.Body, params["boundary"])
			for _, expected := range []string{message.Text, message.HTML} {
				part, err := reader.NextPart()
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
				if err != nil || string(body) != expected {
					t.Fatal("rendered notice changed during transport", err)
				}
			}
			if _, err = reader.NextPart(); err != io.EOF {
				t.Fatal("unexpected extra MIME content", err)
			}
		})
	}
}
