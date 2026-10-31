package mailer

import (
	"context"
	"encoding/base64"
	"io"
	"net/mail"
	"strings"
	"testing"
	"time"
)

var pageNoticeAt = time.Date(2026, 10, 31, 10, 20, 5, 0, time.UTC)

func TestPageTemplateNoticeUsesVerifiedTLSAndBoundedNonPIIMessage(t *testing.T) {
	for _, security := range []string{"implicit", "starttls"} {
		t.Run(security, func(t *testing.T) {
			f := localSMTP(t, security, "")
			sender, e := NewSMTP(f.config)
			if e != nil {
				t.Fatal(e)
			}
			if e = sender.SendPageTemplateChanged(context.Background(), "admin@example.test", 5, 2, "admin", pageNoticeAt); e != nil {
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
			if msg.Header.Get("To") != "admin@example.test" || msg.Header.Get("Bcc") != "" {
				t.Fatal("wrong recipient headers")
			}
			if !strings.Contains(string(body), "恢复为第 2 版（新版本第 5 版）") || !strings.Contains(string(body), "管理员 admin 于 2026-10-31 18:20:05（UTC+8）") || !strings.Contains(string(body), "如果不是你本人操作") {
				t.Fatal("wrong original notice", string(body))
			}
			if strings.Contains(string(body), "<") || strings.Contains(string(body), "synthetic-password") || len(f.messages[0]) > 4096 {
				t.Fatal("notice leaked scope, HTML or exceeded message budget")
			}
		})
	}
}

func TestPageTemplateNoticeSaveWordingAndActorFallback(t *testing.T) {
	f := localSMTP(t, "implicit", "")
	sender, e := NewSMTP(f.config)
	if e != nil {
		t.Fatal(e)
	}
	if e = sender.SendPageTemplateChanged(context.Background(), "admin@example.test", 4, 0, "bad name", pageNoticeAt); e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	body, e := io.ReadAll(base64.NewDecoder(base64.StdEncoding, mustBody(t, f.messages[0])))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(body), "保存了首页自定义 HTML 第 4 版") || !strings.Contains(string(body), "管理员 （未知） 于") {
		t.Fatal("wrong save wording or actor fallback", string(body))
	}
	if strings.Contains(string(body), "恢复为第") {
		t.Fatal("save message mentions restore")
	}
}

func TestPageTemplateNoticeRejectsInvalidInputsWithoutIO(t *testing.T) {
	f := localSMTP(t, "implicit", "")
	sender, e := NewSMTP(f.config)
	if e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		to                    string
		version, restoredFrom int64
		at                    time.Time
	}{
		{"admin@example.test", 0, 0, pageNoticeAt},                         // version must be > 0
		{"admin@example.test", 4, 4, pageNoticeAt},                         // restoredFrom >= version
		{"admin@example.test", 4, 0, time.Time{}},                          // zero time
		{"admin@example.test\r\nBcc:bad@example.test", 4, 0, pageNoticeAt}, // header injection
		{"", 4, 0, pageNoticeAt},                                           // empty recipient
	}
	for _, c := range cases {
		if e = sender.SendPageTemplateChanged(context.Background(), c.to, c.version, c.restoredFrom, "admin", c.at); e != ErrSMTP {
			t.Fatal("invalid notice accepted", c)
		}
	}
	if f.connections.Load() != 0 {
		t.Fatal("invalid notice connected")
	}
}

func TestPageTemplateNoticeNeverRetriesUnknownSubmission(t *testing.T) {
	for _, failure := range []string{"wrong-name", "no-auth", "lost-final"} {
		t.Run(failure, func(t *testing.T) {
			f := localSMTP(t, "implicit", failure)
			sender, e := NewSMTP(f.config)
			if e != nil {
				t.Fatal(e)
			}
			if e = sender.SendPageTemplateChanged(context.Background(), "admin@example.test", 4, 0, "admin", pageNoticeAt); e != ErrSMTP {
				t.Fatal("unsafe SMTP accepted", e)
			}
			if f.connections.Load() != 1 || f.dataAttempts.Load() > 1 {
				t.Fatal("notice retried or did not validate TLS")
			}
		})
	}
}

func mustBody(t *testing.T, raw string) io.Reader {
	t.Helper()
	msg, e := mail.ReadMessage(strings.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	return msg.Body
}
