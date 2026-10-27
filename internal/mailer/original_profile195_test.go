package mailer

import (
	"context"
	"net/mail"
	"strings"
	"testing"
)

func TestSMTP195OriginalSiteDisplayNameAndUsernameReplyToUseAuthenticatedEnvelope(t *testing.T) {
	f := localSMTP(t, "implicit", "")
	cfg := f.config
	cfg.FromName = "本地网站195"
	cfg.ReplyTo = cfg.From
	sender, e := NewSMTP(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = sender.SendVerification(context.Background(), "recipient@example.test", "recover", "1234567"); e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	messages := append([]string(nil), f.messages...)
	f.mu.Unlock()
	if len(messages) != 1 {
		t.Fatal("missing original SMTP message")
	}
	message, e := mail.ReadMessage(strings.NewReader(messages[0]))
	if e != nil {
		t.Fatal(e)
	}
	from, e := mail.ParseAddress(message.Header.Get("From"))
	if e != nil || from.Address != cfg.From || from.Name != cfg.FromName {
		t.Fatal("original site display name missing", e)
	}
	reply, e := mail.ParseAddress(message.Header.Get("Reply-To"))
	if e != nil || reply.Address != cfg.From {
		t.Fatal("original username Reply-To missing", e)
	}
	for _, name := range []string{"bad\r\nBcc: victim@example.test", strings.Repeat("x", 257)} {
		cfg.FromName = name
		if _, e = NewSMTP(cfg); e == nil {
			t.Fatal("unsafe From display name accepted")
		}
	}
	cfg.FromName = "safe"
	cfg.ReplyTo = "a@example.test\r\nBcc: other@example.test"
	if _, e = NewSMTP(cfg); e == nil {
		t.Fatal("unsafe Reply-To accepted")
	}
}
