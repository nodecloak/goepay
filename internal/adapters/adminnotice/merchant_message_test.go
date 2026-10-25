package adminnotice

import (
	"context"
	"encoding/json"
	"goepay/internal/merchantnotice"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type merchantSender80 interface {
	SendMerchantNotice(context.Context, string, merchantnotice.Message) error
	NoticeTransport() merchantnotice.Transport
	NoticeScope() string
	ValidateNoticeRecipient(string) error
}

func merchantMessage80(t *testing.T) merchantnotice.Message {
	t.Helper()
	message, e := merchantnotice.Render(merchantnotice.Input{Scene: "order", SiteName: "本地易支付", At: time.Now(), Params: map[string]string{"name": `<at user_id="all">[商品](https://local.example.invalid)</at>`, "money": "12.30", "type": "微信", "out_trade_no": "OUT80", "trade_no": "TRADE80", "time": "2026-10-24 09:00:00"}})
	if e != nil {
		t.Fatal(e)
	}
	return message
}

func TestMerchantNotice80SharedTransportsUseActualTLSBoundRecipientsAndEscapedContent(t *testing.T) {
	endpoints := map[string]string{"ding": "https://oapi.dingtalk.com/robot/send?access_token=synthetic80", "work": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic80", "feishu": "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic80", "lark": "https://open.larksuite.com/open-apis/bot/v2/hook/synthetic80", "telegram": "", "qqbot": ""}
	for kind, endpoint := range endpoints {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			var failed atomic.Bool
			client, _ := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if failed.Load() {
					w.Write([]byte(`{"private_error":"synthetic-credential80"}`))
					return
				}
				if kind == "telegram" {
					if r.Host != "api.telegram.org" || r.ParseForm() != nil || r.Form.Get("chat_id") != "1000" || r.Form.Get("parse_mode") != "HTML" || strings.Contains(r.Form.Get("text"), "<at") || !strings.Contains(r.Form.Get("text"), "&lt;at") || !strings.Contains(r.Form.Get("text"), "TRADE80") {
						t.Error("unsafe merchant Telegram envelope")
					}
					w.Write([]byte(`{"ok":true}`))
					return
				}
				var in map[string]any
				if json.NewDecoder(r.Body).Decode(&in) != nil {
					t.Error("bad merchant message JSON")
					return
				}
				if kind == "qqbot" && r.URL.Path == "/app/getAppAccessToken" {
					if in["appId"] != "800001" || in["clientSecret"] != "synthetic-secret80" {
						t.Error("wrong QQ account")
					}
					w.Write([]byte(`{"access_token":"synthetic-token80","expires_in":7200}`))
					return
				}
				var text string
				switch kind {
				case "ding":
					text = in["markdown"].(map[string]any)["text"].(string)
				case "work":
					text = in["markdown"].(map[string]any)["content"].(string)
				case "qqbot":
					if r.URL.Path != "/v2/users/local80/messages" || r.Header.Get("Authorization") != "QQBot synthetic-token80" {
						t.Error("QQ message lost bound original destination")
					}
					text = in["markdown"].(map[string]any)["content"].(string)
				default:
					text = in["content"].(map[string]any)["text"].(string)
				}
				markdown := kind == "ding" || kind == "work" || kind == "qqbot"
				if strings.Contains(text, "<at") || markdown && strings.Contains(text, "[商品](") || !strings.Contains(text, "TRADE80") || !strings.Contains(text, "12.30") {
					t.Error("merchant content enabled markup/mentions or lost original values", text)
				}
				if kind == "ding" || kind == "work" {
					w.Write([]byte(`{"errcode":0}`))
				} else if kind == "qqbot" {
					w.Write([]byte(`{"id":"message80"}`))
				} else {
					w.Write([]byte(`{"code":0}`))
				}
			}))
			var base any
			var e error
			to := endpoint
			switch kind {
			case "telegram":
				base, e = NewTelegram("800001:synthetic-secret80", client)
				to = "1000"
			case "qqbot":
				base, e = NewQQBot("800001", "synthetic-secret80", client)
				to = "local80"
			default:
				base, e = NewWebhook(endpoint, client)
			}
			if e != nil {
				t.Fatal(e)
			}
			p, ok := base.(merchantSender80)
			if !ok {
				t.Fatal("verified notice transport cannot carry original merchant message")
			}
			message := merchantMessage80(t)
			if p.NoticeScope() == "" || p.NoticeTransport() == "" || p.ValidateNoticeRecipient(to) != nil {
				t.Fatal("merchant recipient profile missing")
			}
			if e = p.SendMerchantNotice(context.Background(), to, message); e != nil {
				t.Fatal(e)
			}
			want := int32(1)
			if kind == "qqbot" {
				want = 2
			}
			if calls.Load() != want {
				t.Fatal("message implicitly retried", calls.Load())
			}
			changed := message
			changed.Text += "changed"
			if e = p.SendMerchantNotice(context.Background(), to, changed); e == nil || calls.Load() != want {
				t.Fatal("mutated renderer message performed I/O", e)
			}
			if e = p.SendMerchantNotice(context.Background(), "/wrong-recipient", message); e == nil || calls.Load() != want {
				t.Fatal("invalid recipient performed I/O", e)
			}
			failed.Store(true)
			e = p.SendMerchantNotice(context.Background(), to, message)
			if e == nil || strings.Contains(e.Error(), "synthetic") || calls.Load() != want+1 {
				t.Fatal("untrusted acceptance retried or leaked", e, calls.Load())
			}
		})
	}
}
