package merchantnotice_test

import (
	"context"
	"testing"
)

func TestPlatformNoticeSwitches134ReadsPerSceneMailAndGlobalBots(t *testing.T) {
	s, pool := profileStore80(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES
('settings','{"key":"msgconfig_order","value":"1"}'),
('settings','{"key":"msgconfig_daily_report","value":"0"}'),
('settings','{"key":"telegramnotice","value":"1"}'),
('settings','{"key":"qqbotnotice","value":"0"}'),
('settings','{"key":"voicenotice","value":"1"}')`); err != nil {
		t.Fatal(err)
	}
	sw, err := s.PlatformNoticeSwitches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !sw.MailRobot["order"] || sw.MailRobot["daily_report"] || sw.MailRobot["settle"] {
		t.Fatalf("per-scene mail/robot switch wrong: %+v", sw.MailRobot)
	}
	if !sw.Telegram || sw.QQBot || !sw.Voice || !sw.GroupSMS {
		t.Fatalf("global switches wrong: %+v", sw)
	}
	// All nine notice scenes are represented so the console can draw the full grid.
	for _, scene := range []string{"order", "settle", "login", "complain", "mchrisk", "balance", "group", "domain_audit", "daily_report"} {
		if _, ok := sw.MailRobot[scene]; !ok {
			t.Fatalf("scene %s missing from matrix facts", scene)
		}
	}
}

func TestPlatformNoticeSwitches134RejectsNonBooleanSettingValue(t *testing.T) {
	s, pool := profileStore80(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"telegramnotice","value":"2"}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlatformNoticeSwitches(ctx); err == nil {
		t.Fatal("non-0/1 switch value accepted")
	}
}
