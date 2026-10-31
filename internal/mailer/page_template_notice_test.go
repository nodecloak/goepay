package mailer

import (
	"context"
	"encoding/base64"
	"goepay/internal/templatechange"
	"io"
	"mime"
	"net/mail"
	"strings"
	"testing"
	"time"
)

var pageNoticeAt = time.Date(2026, 10, 31, 10, 20, 5, 0, time.UTC)

func uploadNotice() templatechange.Notice {
	return templatechange.Notice{Kind: "home", Name: "aurora", Title: "极光", Operation: "upload", Version: 4, Actor: "admin", At: pageNoticeAt}
}

func restoreNotice() templatechange.Notice {
	return templatechange.Notice{Kind: "home", Name: "aurora", Title: "极光", Operation: "restore", Version: 5, RestoredFrom: 2, Actor: "admin", At: pageNoticeAt}
}

func TestPageTemplateNoticeUsesVerifiedTLSAndBoundedNonPIIMessage(t *testing.T) {
	for _, security := range []string{"implicit", "starttls"} {
		t.Run(security, func(t *testing.T) {
			f := localSMTP(t, security, "")
			sender, e := NewSMTP(f.config)
			if e != nil {
				t.Fatal(e)
			}
			if e = sender.SendPageTemplateNotice(context.Background(), "admin@example.test", restoreNotice()); e != nil {
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

func TestPageTemplateNoticeSubjectAndActorFallback(t *testing.T) {
	f := localSMTP(t, "implicit", "")
	sender, e := NewSMTP(f.config)
	if e != nil {
		t.Fatal(e)
	}
	n := templatechange.Notice{Kind: "home", Name: "aurora", Title: "极光", Operation: "upload", Version: 4, Actor: "bad name", At: pageNoticeAt}
	if e = sender.SendPageTemplateNotice(context.Background(), "admin@example.test", n); e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	msg, e := mail.ReadMessage(strings.NewReader(f.messages[0]))
	if e != nil {
		t.Fatal(e)
	}
	// Subject is the MIME B-encoded notice title.
	if msg.Header.Get("Subject") != mimeTitle() {
		t.Fatal("wrong subject", msg.Header.Get("Subject"))
	}
	body, e := io.ReadAll(base64.NewDecoder(base64.StdEncoding, msg.Body))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(body), "上传了首页模板「极光」（aurora）第 4 版") || !strings.Contains(string(body), "管理员 （未知） 于") {
		t.Fatal("wrong upload wording or actor fallback", string(body))
	}
	if strings.Contains(string(body), "恢复为第") {
		t.Fatal("upload message mentions restore")
	}
}

func TestPageTemplateNoticeRejectsInvalidInputsWithoutIO(t *testing.T) {
	f := localSMTP(t, "implicit", "")
	sender, e := NewSMTP(f.config)
	if e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		to string
		n  templatechange.Notice
	}{
		{"admin@example.test", templatechange.Notice{Kind: "home", Name: "aurora", Title: "极光", Operation: "upload", Version: 0, Actor: "admin", At: pageNoticeAt}},                         // version must be > 0
		{"admin@example.test", templatechange.Notice{Kind: "home", Name: "aurora", Title: "极光", Operation: "restore", Version: 4, RestoredFrom: 4, Actor: "admin", At: pageNoticeAt}},       // restored_from >= version
		{"admin@example.test", templatechange.Notice{Kind: "home", Name: "aurora", Title: "极光", Operation: "upload", Version: 4, Actor: "admin"}},                                           // zero time
		{"admin@example.test\r\nBcc:bad@example.test", templatechange.Notice{Kind: "home", Name: "aurora", Title: "极光", Operation: "upload", Version: 4, Actor: "admin", At: pageNoticeAt}}, // header injection
		{"", templatechange.Notice{Kind: "home", Name: "aurora", Title: "极光", Operation: "upload", Version: 4, Actor: "admin", At: pageNoticeAt}},                                           // empty recipient
	}
	for _, c := range cases {
		if e = sender.SendPageTemplateNotice(context.Background(), c.to, c.n); e != ErrSMTP {
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
			if e = sender.SendPageTemplateNotice(context.Background(), "admin@example.test", uploadNotice()); e != ErrSMTP {
				t.Fatal("unsafe SMTP accepted", e)
			}
			if f.connections.Load() != 1 || f.dataAttempts.Load() > 1 {
				t.Fatal("notice retried or did not validate TLS")
			}
		})
	}
}

// mimeTitle returns the subject header the sender writes for the fixed notice
// title: the MIME B-encoded 页面模板已修改.
func mimeTitle() string {
	title, _, _ := uploadNotice().Message()
	return mime.BEncoding.Encode("UTF-8", title)
}
