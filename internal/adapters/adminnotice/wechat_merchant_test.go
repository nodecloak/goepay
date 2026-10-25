package adminnotice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	notice "goepay/internal/merchantnotice"
)

func wechatNotice80Config() WechatMerchantConfig {
	return WechatMerchantConfig{AppID: "wxsynthetic80", Secret: "synthetic-wechat-secret80", Template: notice.WechatTemplateConfig{ID: "synthetic-template80", ConsoleURL: "https://console.example.test", Fields: map[string]string{"wxnotice_tpl_order_name": "thing1", "wxnotice_tpl_order_money": "amount2"}}, Input: notice.Input{Scene: "order", SiteName: "Local80", At: time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC), Params: map[string]string{"name": "<safe>商品80", "money": "12.34", "type": "微信支付", "out_trade_no": "merchant80", "trade_no": "202610240080", "time": "2026-10-24 09:00:00"}}}
}
func TestMerchantNotice80WechatActualTLSOriginalTokenAndTemplatePayload(t *testing.T) {
	var tokens, messages atomic.Int64
	client, dials := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "api.weixin.qq.com" || r.Method != "POST" {
			t.Error("wrong destination/method", r.Host, r.Method)
			w.WriteHeader(400)
			return
		}
		switch r.URL.Path {
		case "/cgi-bin/stable_token":
			tokens.Add(1)
			var in map[string]any
			if json.NewDecoder(r.Body).Decode(&in) != nil || in["appid"] != "wxsynthetic80" || in["secret"] != "synthetic-wechat-secret80" || in["grant_type"] != "client_credential" || in["force_refresh"] != false {
				t.Error("wrong token contract", in)
			}
			fmt.Fprint(w, `{"access_token":"synthetic-token80","expires_in":7200}`)
		case "/cgi-bin/message/template/send":
			messages.Add(1)
			var in struct {
				To         string                                `json:"touser"`
				TemplateID string                                `json:"template_id"`
				URL        string                                `json:"url"`
				Data       map[string]notice.WechatTemplateValue `json:"data"`
			}
			if json.NewDecoder(r.Body).Decode(&in) != nil || r.URL.Query().Get("access_token") != "synthetic-token80" || in.To != "synthetic-openid80" || in.TemplateID != "synthetic-template80" || in.URL != "https://console.example.test/merchant/trade/order" || in.Data["thing1"].Value != "<safe>商品80" || in.Data["amount2"].Value != "¥12.34" {
				t.Error("wrong original template payload", in)
			}
			fmt.Fprint(w, `{"errcode":0,"errmsg":"ok","msgid":2106918981956616192}`)
		default:
			t.Error("unexpected endpoint", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	cfg := wechatNotice80Config()
	sender, err := NewWechatMerchant(cfg, client)
	if err != nil || dials.Load() != 0 {
		t.Fatal("constructor performed I/O", err, dials.Load())
	}
	message, err := notice.RenderWechat(cfg.Input)
	if err != nil {
		t.Fatal(err)
	}
	if err = sender.SendMerchantNotice(context.Background(), "synthetic-openid80", message); err != nil {
		t.Fatal(err)
	}
	if tokens.Load() != 1 || messages.Load() != 1 || sender.NoticeTransport() != notice.Wechat || sender.NoticeScope() == "" {
		t.Fatal("wrong token/message count", tokens.Load(), messages.Load())
	}
	if strings.Contains(fmt.Sprintf("%v %#v", sender, sender), "synthetic-wechat-secret80") {
		t.Fatal("credential in formatted sender")
	}
}
func TestMerchantNotice80WechatRejectsUnsealedMessagesAndDoesNotRetryUncertainResponses(t *testing.T) {
	for _, mode := range []string{"altered", "token-error", "token-duplicate", "token-short-expiry", "message-error", "message-missing-id", "message-redirect"} {
		t.Run(mode, func(t *testing.T) {
			var tokens, messages atomic.Int64
			client, dials := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/cgi-bin/stable_token" {
					tokens.Add(1)
					switch mode {
					case "token-error":
						fmt.Fprint(w, `{"errcode":40013,"errmsg":"synthetic-wechat-secret80"}`)
					case "token-duplicate":
						fmt.Fprint(w, `{"access_token":"a","access_token":"b","expires_in":7200}`)
					case "token-short-expiry":
						fmt.Fprint(w, `{"access_token":"synthetic-token80","expires_in":0}`)
					default:
						fmt.Fprint(w, `{"access_token":"synthetic-token80","expires_in":7200}`)
					}
					return
				}
				messages.Add(1)
				switch mode {
				case "message-error":
					fmt.Fprint(w, `{"errcode":40001,"errmsg":"synthetic-token80"}`)
				case "message-missing-id":
					fmt.Fprint(w, `{"errcode":0}`)
				case "message-redirect":
					w.Header().Set("Location", "https://api.weixin.qq.com/unexpected")
					w.WriteHeader(302)
				default:
					fmt.Fprint(w, `{"errcode":0,"msgid":80}`)
				}
			}))
			cfg := wechatNotice80Config()
			sender, err := NewWechatMerchant(cfg, client)
			if err != nil {
				t.Fatal(err)
			}
			message, _ := notice.RenderWechat(cfg.Input)
			if mode == "altered" {
				message.Text += " altered"
			}
			err = sender.SendMerchantNotice(context.Background(), "synthetic-openid80", message)
			if err == nil || strings.Contains(err.Error(), "synthetic") {
				t.Fatal("unsafe response accepted or secret exposed", err)
			}
			if mode == "altered" {
				if dials.Load() != 0 {
					t.Fatal("altered message reached network")
				}
			} else if tokens.Load() != 1 || messages.Load() > 1 {
				t.Fatal("uncertain message implicitly retried", tokens.Load(), messages.Load())
			}
		})
	}
}
