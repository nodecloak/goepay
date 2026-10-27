package mailer

import (
	"context"
	"encoding/base64"
	"io"
	"net/mail"
	"strings"
	"testing"
)

func TestSMTP197ContactProofHasDistinctPurposeAndValidatedTLS(t *testing.T) {
	for _, sample := range []struct{ purpose, want string }{{"contact-old", "验证原邮箱"}, {"contact-new", "绑定新邮箱"}} {
		t.Run(sample.purpose, func(t *testing.T) {
			f := localSMTP(t, "implicit", "")
			sender, e := NewSMTP(f.config)
			if e != nil {
				t.Fatal(e)
			}
			if e = sender.SendVerification(context.Background(), "contact197@example.test", sample.purpose, "1234567"); e != nil {
				t.Fatal("contact-purpose TLS delivery unavailable", e)
			}
			if f.dataAttempts.Load() != 1 {
				t.Fatal("contact proof not submitted exactly once")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.messages) != 1 {
				t.Fatal("contact submission missing")
			}
			msg, e := mail.ReadMessage(strings.NewReader(f.messages[0]))
			if e != nil {
				t.Fatal(e)
			}
			body, e := io.ReadAll(base64.NewDecoder(base64.StdEncoding, msg.Body))
			if e != nil || !strings.Contains(string(body), sample.want) || strings.Contains(string(body), "重置密码") || strings.Contains(string(body), "注册") {
				t.Fatal("contact proof confused with registration/recovery", e)
			}
		})
	}
}
