package mailer

import "testing"

func TestChannelNotice77SMTPRouteBindsSendingAccountAndValidatesRecipientWithoutIO(t *testing.T) {
	c := SMTPConfig{Host: "smtp.example.invalid", Port: 465, Security: "implicit", Username: "local77", Password: "synthetic77", From: "sender@example.test"}
	s, e := NewSMTP(c)
	if e != nil {
		t.Fatal(e)
	}
	type route interface {
		ChannelNoticeMode() string
		ChannelNoticeScope() string
		ValidateChannelNoticeRecipient(string) error
	}
	r, ok := any(s).(route)
	if !ok {
		t.Fatal("SMTP sender lacks route identity")
	}
	same, _ := NewSMTP(c)
	c.Password = "changed77"
	changed, _ := NewSMTP(c)
	if r.ChannelNoticeMode() != "smtp" || len(r.ChannelNoticeScope()) != 64 || r.ChannelNoticeScope() != any(same).(route).ChannelNoticeScope() || r.ChannelNoticeScope() == any(changed).(route).ChannelNoticeScope() {
		t.Fatal("SMTP account identity not bound")
	}
	if r.ValidateChannelNoticeRecipient("admin@example.test") != nil || r.ValidateChannelNoticeRecipient("Name <admin@example.test>") == nil || r.ValidateChannelNoticeRecipient("a@example.test\r\nBcc:b@example.test") == nil {
		t.Fatal("recipient validation failed")
	}
}
