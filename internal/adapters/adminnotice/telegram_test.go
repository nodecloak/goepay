package adminnotice

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAdminNotice77TelegramActualTLSOriginalFormAndAcceptedResult(t *testing.T) {
	const token = "770001:synthetic-telegram-secret77"
	var calls atomic.Int32
	client, dials := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Host != "api.telegram.org" || r.URL.Path != "/bot"+token+"/sendMessage" || r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("wrong original Telegram request")
		}
		if e := r.ParseForm(); e != nil {
			t.Error(e)
			return
		}
		if len(r.PostForm) != 4 || r.PostForm.Get("chat_id") != "-10000000000077" || r.PostForm.Get("parse_mode") != "HTML" || r.PostForm.Get("disable_web_page_preview") != "true" || !strings.Contains(r.PostForm.Get("text"), "<strong>支付通道自动关闭提醒</strong>") || !strings.Contains(r.PostForm.Get("text"), "通道 7") || !strings.Contains(r.PostForm.Get("text"), "记录 31") {
			t.Error("wrong original form", r.PostForm)
		}
		w.Write([]byte(`{"ok":true,"result":{"message_id":77}}`))
	}))
	sender, e := NewTelegram(token, client)
	if e != nil {
		t.Fatal(e)
	}
	if dials.Load() != 0 {
		t.Fatal("constructor contacted bot API")
	}
	if e = sender.SendChannelStopped(context.Background(), "-10000000000077", 7, 31, "SYSTEMERROR"); e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 1 {
		t.Fatal("Telegram submit retried", calls.Load())
	}
}

func TestAdminNotice77TelegramRejectsUnsafeConfigurationAndUncertainReply(t *testing.T) {
	var calls atomic.Int32
	client, dials := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"ok":false,"description":"private-synthetic-error77"}`))
	}))
	for _, token := range []string{"", "plain-token", "770001:secret/redirect", "770001:secret\r\n", "770001:secret?recipient=another"} {
		if _, e := NewTelegram(token, client); e != ErrDelivery {
			t.Fatal("unsafe bot credential accepted")
		}
	}
	sender, e := NewTelegram("770001:synthetic-telegram-secret77", client)
	if e != nil {
		t.Fatal(e)
	}
	for _, to := range []string{"", "0", "-0", "01", "user/../../messages", "@bad name", "1\r\n"} {
		if e = sender.SendChannelStopped(context.Background(), to, 7, 31, "SYSTEMERROR"); e != ErrDelivery {
			t.Fatal("unsafe Telegram destination accepted")
		}
	}
	if dials.Load() != 0 {
		t.Fatal("invalid configuration/destination caused IO")
	}
	if e = sender.SendChannelStopped(context.Background(), "77", 7, 31, "SYSTEMERROR"); e != ErrDelivery || calls.Load() != 1 || strings.Contains(e.Error(), "private-synthetic-error77") {
		t.Fatal("uncertain Telegram result accepted, retried or leaked", e, calls.Load())
	}
}
