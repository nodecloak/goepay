package adminnotice

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var pageNoticeAt = time.Date(2026, 10, 31, 10, 20, 5, 0, time.UTC)

func TestPageTemplateTelegramOriginalFormAndAcceptedResult(t *testing.T) {
	const token = "770001:synthetic-telegram-secret77"
	var calls atomic.Int32
	client, _ := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Host != "api.telegram.org" || r.URL.Path != "/bot"+token+"/sendMessage" || r.Method != "POST" {
			t.Error("wrong original Telegram request")
		}
		if e := r.ParseForm(); e != nil {
			t.Error(e)
			return
		}
		text := r.PostForm.Get("text")
		if r.PostForm.Get("chat_id") != "77" || r.PostForm.Get("parse_mode") != "HTML" || !strings.Contains(text, "<strong>首页自定义 HTML 已修改</strong>") || !strings.Contains(text, "第 4 版") {
			t.Error("wrong original form", r.PostForm)
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	sender, e := NewTelegram(token, client)
	if e != nil {
		t.Fatal(e)
	}
	if e = sender.SendPageTemplateChanged(context.Background(), "77", 4, 0, "admin", pageNoticeAt); e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 1 {
		t.Fatal("Telegram submit retried", calls.Load())
	}
}

func TestPageTemplateTelegramRejectsUnsafeDestinationAndUncertainReply(t *testing.T) {
	var calls atomic.Int32
	client, dials := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"ok":false,"description":"private-synthetic-error77"}`))
	}))
	sender, e := NewTelegram("770001:synthetic-telegram-secret77", client)
	if e != nil {
		t.Fatal(e)
	}
	for _, to := range []string{"", "0", "@bad name", "1\r\n"} {
		if e = sender.SendPageTemplateChanged(context.Background(), to, 4, 0, "admin", pageNoticeAt); e != ErrDelivery {
			t.Fatal("unsafe Telegram destination accepted", to)
		}
	}
	// Invalid builder input (restoredFrom >= version) never touches the network.
	if e = sender.SendPageTemplateChanged(context.Background(), "77", 4, 4, "admin", pageNoticeAt); e != ErrDelivery {
		t.Fatal("invalid version accepted")
	}
	if dials.Load() != 0 {
		t.Fatal("invalid destination/input caused IO", dials.Load())
	}
	if e = sender.SendPageTemplateChanged(context.Background(), "77", 4, 0, "admin", pageNoticeAt); e != ErrDelivery || calls.Load() != 1 || strings.Contains(e.Error(), "private-synthetic-error77") {
		t.Fatal("uncertain Telegram result accepted, retried or leaked", e, calls.Load())
	}
}

func TestPageTemplateWebhookOriginalPayloadAndAcceptedReply(t *testing.T) {
	for _, tc := range []struct{ endpoint, host, kind string }{
		{"https://oapi.dingtalk.com/robot/send?access_token=synthetic-token77", "oapi.dingtalk.com", "ding"},
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-key77", "qyapi.weixin.qq.com", "work"},
		{"https://open.feishu.cn/open-apis/bot/v2/hook/local-hook77", "open.feishu.cn", "feishu"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			var calls atomic.Int32
			client, dials := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.TLS == nil || r.Method != "POST" || r.Host != tc.host {
					t.Error("wrong original provider route")
				}
				var payload map[string]any
				if e := json.NewDecoder(r.Body).Decode(&payload); e != nil {
					t.Error(e)
					return
				}
				raw, _ := json.Marshal(payload)
				if !strings.Contains(string(raw), "首页自定义 HTML 已修改") || !strings.Contains(string(raw), "第 5 版") || !strings.Contains(string(raw), "恢复为第 2 版") || strings.Contains(string(raw), "synthetic-token77") {
					t.Error("wrong original notice or token leaked", string(raw))
				}
				if tc.kind == "ding" || tc.kind == "work" {
					w.Write([]byte(`{"errcode":0}`))
				} else {
					w.Write([]byte(`{"code":0}`))
				}
			}))
			transport, e := NewWebhook(tc.endpoint, client)
			if e != nil {
				t.Fatal(e)
			}
			if dials.Load() != 0 {
				t.Fatal("constructor contacted upstream")
			}
			if e = transport.SendPageTemplateChanged(context.Background(), tc.endpoint, 5, 2, "admin", pageNoticeAt); e != nil {
				t.Fatal(e)
			}
			if calls.Load() != 1 {
				t.Fatal("wrong send count", calls.Load())
			}
			// A notice to any other target never leaves.
			if e = transport.SendPageTemplateChanged(context.Background(), "https://other.example/hook", 5, 2, "admin", pageNoticeAt); e != ErrDelivery {
				t.Fatal("wrong target accepted")
			}
		})
	}
}

func TestPageTemplateQQBotTokenThenOriginalMarkdown(t *testing.T) {
	var tokens, messages atomic.Int32
	client, dials := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if json.NewDecoder(r.Body).Decode(&payload) != nil {
			t.Error("invalid QQ JSON")
			return
		}
		switch r.URL.Path {
		case "/app/getAppAccessToken":
			tokens.Add(1)
			w.Write([]byte(`{"access_token":"synthetic-access-token77","expires_in":"7200"}`))
		case "/v2/users/local-openid77/messages":
			messages.Add(1)
			if payload["msg_type"] != float64(2) || r.Header.Get("Authorization") != "QQBot synthetic-access-token77" {
				t.Error("QQ message missing token or wrong type")
			}
			markdown := payload["markdown"].(map[string]any)["content"].(string)
			if !strings.HasPrefix(markdown, "# 首页自定义 HTML 已修改\n\n") || !strings.Contains(markdown, "第 4 版") {
				t.Error("wrong QQ markdown", markdown)
			}
			w.Write([]byte(`{"id":"local-message77"}`))
		default:
			t.Error("unexpected QQ API path", r.URL.Path)
		}
	}))
	sender, e := NewQQBot("770001", "synthetic-qq-secret77", client)
	if e != nil {
		t.Fatal(e)
	}
	if dials.Load() != 0 {
		t.Fatal("constructor fetched credentials")
	}
	if e = sender.SendPageTemplateChanged(context.Background(), "local-openid77", 4, 0, "admin", pageNoticeAt); e != nil {
		t.Fatal(e)
	}
	if tokens.Load() != 1 || messages.Load() != 1 {
		t.Fatal("QQ notice retried", tokens.Load(), messages.Load())
	}
	for _, to := range []string{"", "openid/../../other", "openid\r\n"} {
		if e = sender.SendPageTemplateChanged(context.Background(), to, 4, 0, "admin", pageNoticeAt); e != ErrDelivery {
			t.Fatal("unsafe QQ destination accepted", to)
		}
	}
}
