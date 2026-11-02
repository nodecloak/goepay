package adminnotice

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestWebhookMention231OnlyExplicitConfiguredPhone(t *testing.T) {
	for _, kind := range []string{"ding", "work"} {
		t.Run(kind, func(t *testing.T) {
			client, _ := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("invalid JSON")
				}
				if kind == "ding" {
					at, _ := body["at"].(map[string]any)
					phones, _ := at["atMobiles"].([]any)
					if len(phones) != 1 || phones[0] != "13800138231" || at["isAtAll"] != false {
						t.Error("wrong explicit mention")
					}
				} else {
					text, _ := body["text"].(map[string]any)
					phones, _ := text["mentioned_mobile_list"].([]any)
					if body["msgtype"] != "text" || len(phones) != 1 || phones[0] != "13800138231" {
						t.Error("wrong explicit mention")
					}
				}
				w.Write([]byte(`{"errcode":0}`))
			}))
			url := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=local231"
			if kind == "ding" {
				url = "https://oapi.dingtalk.com/robot/send?access_token=local231"
			}
			sender, e := NewWebhookMention(url, "13800138231", client)
			if e != nil {
				t.Fatal(e)
			}
			if e = sender.SendMerchantNotice(context.Background(), url, merchantMessage80(t)); e != nil {
				t.Fatal(e)
			}
		})
	}
	if _, e := NewWebhookMention("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=local231", "@all", nil); e == nil {
		t.Fatal("arbitrary mention accepted")
	}
}
