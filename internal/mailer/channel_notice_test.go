package mailer

import (
	"context"
	"encoding/base64"
	"io"
	"net/mail"
	"strings"
	"testing"
)

func TestChannelNotice76UsesVerifiedTLSAndBoundedNonPIIMessage(t *testing.T) {
	for _, security := range []string{"implicit", "starttls"} {
		t.Run(security, func(t *testing.T) {
			f := localSMTP(t, security, "")
			sender, e := NewSMTP(f.config)
			if e != nil {
				t.Fatal(e)
			}
			if e = sender.SendChannelStopped(context.Background(), "admin@example.test", 7, 31, "40004:PAYER_BALANCE_NOT_ENOUGH"); e != nil {
				t.Fatal(e)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.messages) != 1 || f.dataAttempts.Load() != 1 {
				t.Fatal("wrong SMTP submit count")
			}
			msg, e := mail.ReadMessage(strings.NewReader(f.messages[0]))
			if e != nil {
				t.Fatal(e)
			}
			if _, e = msg.Header.Date(); e != nil {
				t.Fatal("notice has no valid message date", e)
			}
			body, e := io.ReadAll(base64.NewDecoder(base64.StdEncoding, msg.Body))
			if e != nil {
				t.Fatal(e)
			}
			if msg.Header.Get("To") != "admin@example.test" || msg.Header.Get("Bcc") != "" || !strings.Contains(string(body), "通道 7") || !strings.Contains(string(body), "记录 31") || !strings.Contains(string(body), "40004:PAYER_BALANCE_NOT_ENOUGH") {
				t.Fatal("wrong original notice", string(body))
			}
			if strings.Contains(string(body), "synthetic-password") || strings.Contains(string(body), "seller:") || len(f.messages[0]) > 4096 {
				t.Fatal("notice leaked scope or exceeded message budget")
			}
		})
	}
}

func TestChannelNotice76RejectsInjectionWithoutIOAndNeverRetriesUnknownSubmission(t *testing.T) {
	f := localSMTP(t, "implicit", "")
	sender, e := NewSMTP(f.config)
	if e != nil {
		t.Fatal(e)
	}
	for _, code := range []string{"", "40004:FAIL\r\nBcc:other@example.test", "private message", "<script>", strings.Repeat("A", 129)} {
		if e = sender.SendChannelStopped(context.Background(), "admin@example.test", 7, 31, code); e != ErrSMTP {
			t.Fatal("unsafe code accepted", code)
		}
	}
	if e = sender.SendChannelStopped(context.Background(), "admin@example.test\r\nBcc:bad@example.test", 7, 31, "SYSTEMERROR"); e != ErrSMTP {
		t.Fatal("unsafe recipient")
	}
	if f.connections.Load() != 0 {
		t.Fatal("invalid notice connected")
	}
	for _, failure := range []string{"wrong-name", "no-auth", "lost-final"} {
		t.Run(failure, func(t *testing.T) {
			f := localSMTP(t, "implicit", failure)
			sender, e := NewSMTP(f.config)
			if e != nil {
				t.Fatal(e)
			}
			if e = sender.SendChannelStopped(context.Background(), "admin@example.test", 7, 31, "SYSTEMERROR"); e != ErrSMTP {
				t.Fatal("unsafe SMTP accepted", e)
			}
			if f.connections.Load() != 1 || f.dataAttempts.Load() > 1 {
				t.Fatal("notice retried or did not validate TLS")
			}
		})
	}
}
