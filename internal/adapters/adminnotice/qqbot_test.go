package adminnotice

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAdminNotice77QQBotActualTLSTokenThenOriginalMarkdown(t *testing.T) {
	var tokens, messages atomic.Int32
	client, dials := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "api.bot.qq.com" || r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("wrong QQ Bot transport or method")
		}
		var payload map[string]any
		if json.NewDecoder(r.Body).Decode(&payload) != nil {
			t.Error("invalid QQ JSON")
			return
		}
		switch r.URL.Path {
		case "/app/getAppAccessToken":
			tokens.Add(1)
			if len(payload) != 2 || payload["appId"] != "770001" || payload["clientSecret"] != "synthetic-qq-secret77" || r.Header.Get("Authorization") != "" {
				t.Error("wrong original client credentials payload")
			}
			w.Write([]byte(`{"access_token":"synthetic-access-token77","expires_in":"7200"}`))
		case "/v2/users/local-openid77/messages":
			messages.Add(1)
			if tokens.Load() != 1 || payload["msg_type"] != float64(2) || r.Header.Get("Authorization") != "QQBot synthetic-access-token77" {
				t.Error("QQ message missing original token or wrong type")
			}
			markdown := payload["markdown"].(map[string]any)["content"].(string)
			if !strings.HasPrefix(markdown, "# 支付通道自动关闭提醒\n\n") || !strings.Contains(markdown, "通道 7") || !strings.Contains(markdown, "记录 31") || strings.Contains(markdown, "synthetic-qq-secret77") {
				t.Error("wrong QQ Markdown")
			}
			w.Write([]byte(`{"id":"local-message77"}`))
		default:
			t.Error("unexpected QQ API path")
		}
	}))
	sender, e := NewQQBot("770001", "synthetic-qq-secret77", client)
	if e != nil {
		t.Fatal(e)
	}
	if dials.Load() != 0 {
		t.Fatal("constructor fetched credentials")
	}
	if e = sender.SendChannelStopped(context.Background(), "local-openid77", 7, 31, "SYSTEMERROR"); e != nil {
		t.Fatal(e)
	}
	if tokens.Load() != 1 || messages.Load() != 1 {
		t.Fatal("QQ notice retried", tokens.Load(), messages.Load())
	}
}

func TestAdminNotice77QQBotRejectsUnsafeConfigAndDoesNotLeakOrRetryFailures(t *testing.T) {
	for _, mode := range []string{"token-error", "message-error"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			client, dials := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/app/getAppAccessToken" && mode == "message-error" {
					w.Write([]byte(`{"access_token":"synthetic-access-token77","expires_in":7200}`))
					return
				}
				w.Write([]byte(`{"message":"private-synthetic-error77"}`))
			}))
			for _, cfg := range [][2]string{{"", "secret77"}, {"770001", ""}, {"770001", "secret\r\n77"}, {"bad/app", "secret77"}} {
				if _, e := NewQQBot(cfg[0], cfg[1], client); e != ErrDelivery {
					t.Fatal("unsafe QQ config accepted")
				}
			}
			sender, e := NewQQBot("770001", "synthetic-qq-secret77", client)
			if e != nil {
				t.Fatal(e)
			}
			for _, to := range []string{"", "openid/../../other", "openid\r\n"} {
				if e = sender.SendChannelStopped(context.Background(), to, 7, 31, "SYSTEMERROR"); e != ErrDelivery {
					t.Fatal("unsafe QQ destination accepted")
				}
			}
			if dials.Load() != 0 {
				t.Fatal("invalid config caused IO")
			}
			e = sender.SendChannelStopped(context.Background(), "local-openid77", 7, 31, "SYSTEMERROR")
			expected := int32(1)
			if mode == "message-error" {
				expected = 2
			}
			if e != ErrDelivery || calls.Load() != expected || strings.Contains(e.Error(), "private-synthetic-error77") {
				t.Fatal("QQ failure accepted, retried or leaked", e, calls.Load())
			}
		})
	}
}
