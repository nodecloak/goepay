package main

import (
	"context"
	"goepay/internal/businessnotice"
	"goepay/internal/merchantnotice"
	"goepay/internal/noticeconfig"
	"testing"
)

func TestManagedNotice231AccountsNeverUseEnvironmentFallback(t *testing.T) {
	runtime := &businessNoticeRuntime{getenv: func(k string) string { return "must-not-select-environment" }}
	cases := []struct {
		kind merchantnotice.Transport
		c    noticeconfig.Credential
		to   string
	}{
		{merchantnotice.Robot, noticeconfig.Credential{AppID: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=local231", Phone: "13800138231"}, "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=local231"},
		{merchantnotice.Telegram, noticeconfig.Credential{AppID: "-100231", Secret: "123:synthetic231"}, "-100231"},
		{merchantnotice.QQBot, noticeconfig.Credential{AppID: "123231", Secret: "synthetic231", Recipient: "recipient231"}, "recipient231"},
	}
	for _, c := range cases {
		route, e := runtime.ResolveBusinessNotice(context.Background(), c.kind, businessnotice.Account{Managed231: true, Credential231: c.c})
		if e != nil || route.Recipient != c.to || route.Sender == nil {
			t.Fatal(c.kind, "managed route unavailable", e)
		}
	}
	for _, kind := range []merchantnotice.Transport{merchantnotice.Robot, merchantnotice.Telegram, merchantnotice.QQBot} {
		route, e := runtime.ResolveBusinessNotice(context.Background(), kind, businessnotice.Account{Managed231: true})
		if e == nil || route.Sender != nil {
			t.Fatal(kind, "cleared account adopted environment")
		}
	}
}

func TestManagedNotice231ScopeBindsEffectiveRevision(t *testing.T) {
	v := noticeconfig.View{Enabled: true, Revision: 1}
	c := noticeconfig.Credential{AppID: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=local231"}
	one, _, e := managedFactory231(nil, "robot", v, c, "stamp-one")
	if e != nil {
		t.Fatal(e)
	}
	two, _, e := managedFactory231(nil, "robot", v, c, "stamp-two")
	if e != nil {
		t.Fatal(e)
	}
	if one.(*managedNotice231).NoticeScope() == two.(*managedNotice231).NoticeScope() {
		t.Fatal("revision absent from sending scope")
	}
	v.Enabled = false
	if _, _, e = managedFactory231(nil, "robot", v, c, "stamp"); e == nil {
		t.Fatal("disabled managed account accepted")
	}
}
