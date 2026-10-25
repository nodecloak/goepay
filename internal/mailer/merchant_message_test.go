package mailer

import (
	"context"
	"encoding/base64"
	"goepay/internal/merchantnotice"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestMerchantNotice80SMTPUsesOriginalRenderedAlternativesAndRejectsUnsealedMessagesBeforeIO(t *testing.T) {
	for _, security := range []string{"implicit", "starttls"} {
		t.Run(security, func(t *testing.T) {
			f := localSMTP(t, security, "")
			sender, e := NewSMTP(f.config)
			if e != nil {
				t.Fatal(e)
			}
			message, e := merchantnotice.Render(merchantnotice.Input{Scene: "login", SiteName: "本地易支付", At: time.Now(), Params: map[string]string{"user": "local80", "time": "2026-10-24 09:00:00", "clientip": "127.0.0.1"}})
			if e != nil {
				t.Fatal(e)
			}
			if sender.NoticeTransport() != merchantnotice.SMTP || sender.NoticeScope() == "" || sender.ValidateNoticeRecipient("local@example.test") != nil {
				t.Fatal("SMTP generic profile missing")
			}
			changed := message
			changed.HTML = "<script>changed</script>"
			for _, item := range []struct {
				to      string
				message merchantnotice.Message
			}{{"local@example.test", changed}, {"local@example.test\r\nBcc:injected@example.test", message}} {
				if e = sender.SendMerchantNotice(context.Background(), item.to, item.message); e == nil {
					t.Fatal("untrusted SMTP notice accepted")
				}
			}
			if f.connections.Load() != 0 {
				t.Fatal("unsafe SMTP validation connected")
			}
			if e = sender.SendMerchantNotice(context.Background(), "local@example.test", message); e != nil {
				t.Fatal(e)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.messages) != 1 || f.dataAttempts.Load() != 1 {
				t.Fatal("SMTP notice retried")
			}
			msg, e := mail.ReadMessage(strings.NewReader(f.messages[0]))
			if e != nil {
				t.Fatal(e)
			}
			kind, params, e := mime.ParseMediaType(msg.Header.Get("Content-Type"))
			if e != nil || kind != "multipart/alternative" || msg.Header.Get("Bcc") != "" {
				t.Fatal("not a safe original notice MIME envelope", e)
			}
			reader := multipart.NewReader(msg.Body, params["boundary"])
			count := 0
			for {
				part, e := reader.NextPart()
				if e == io.EOF {
					break
				}
				if e != nil {
					t.Fatal(e)
				}
				body, e := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
				if e != nil {
					t.Fatal(e)
				}
				expected := message.Text
				if strings.HasPrefix(part.Header.Get("Content-Type"), "text/html") {
					expected = message.HTML
				}
				if string(body) != expected {
					t.Fatal("rendered original notification changed in transport")
				}
				count++
			}
			if count != 2 {
				t.Fatal("plain/HTML alternative missing")
			}
		})
	}
}
