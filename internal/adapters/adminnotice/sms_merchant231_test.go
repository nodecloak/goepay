package adminnotice

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/businesssms"
	notice "goepay/internal/merchantnotice"
	"goepay/internal/smsgateway"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBusinessSMS231PureConstructorFixedTLSAndTemplateEscaping(t *testing.T) {
	for _, provider := range []string{"aliyun", "qcloud", "smsbao"} {
		t.Run(provider, func(t *testing.T) {
			var calls atomic.Int64
			param := `会员"\组`
			client, dials := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch provider {
				case "aliyun":
					if r.Host != "dysmsapi.aliyuncs.com" {
						t.Error("unexpected API host")
					}
					r.ParseForm()
					var p map[string]string
					if json.Unmarshal([]byte(r.Form.Get("TemplateParam")), &p) != nil || p["code"] != param || r.Form.Get("TemplateCode") != "SMS_BUSINESS" || r.Form.Get("PhoneNumbers") != "13800138000" {
						t.Error("wrong escaped business parameter")
					}
					fmt.Fprint(w, `{"Code":"OK"}`)
				case "qcloud":
					if r.Host != "yun.tim.qq.com" {
						t.Error("unexpected API host")
					}
					var p struct {
						Params []string `json:"params"`
						TplID  int      `json:"tpl_id"`
					}
					if json.NewDecoder(r.Body).Decode(&p) != nil || len(p.Params) != 1 || p.Params[0] != param || p.TplID != 2345 {
						t.Error("wrong business template")
					}
					fmt.Fprint(w, `{"result":0}`)
				case "smsbao":
					if r.Host != "api.smsbao.com" || r.URL.Query().Get("m") != "13800138000" || r.URL.Query().Get("c") != "【sign】会员到期："+param {
						t.Error("wrong SMSBao business body")
					}
					fmt.Fprint(w, "0")
				}
			}))
			template := "SMS_BUSINESS"
			if provider == "qcloud" {
				template = "2345"
			}
			if provider == "smsbao" {
				template = "会员到期：{code}"
			}
			resolved := &businesssms.Resolved{Snapshot: businesssms.Snapshot{Revision: 1, Provider: provider, Template: template, Phone: "13800138000", Fingerprint: strings.Repeat("a", 64)}, Credentials: smsgateway.Credentials{AccessKeyID: "AK", AccessKeySecret: "synthetic-secret231", AppID: "1234", AppKey: "synthetic-secret231", User: "user", Pass: "synthetic-secret231", Sign: "sign", Template: "VERIFICATION_TEMPLATE"}}
			input := notice.Input{Scene: "group", SiteName: "Test", At: time.Now(), Params: map[string]string{"group": param, "endtime": "2026-11-01 00:00:00"}}
			sender, e := NewSMSMerchant231(resolved, input, client)
			if e != nil || dials.Load() != 0 {
				t.Fatal("constructor performed IO", e)
			}
			message, _ := notice.Render(input)
			if e = sender.SendMerchantNotice(context.Background(), "13900139000", message); e == nil || dials.Load() != 0 {
				t.Fatal("recipient changed")
			}
			if e = sender.SendMerchantNotice(context.Background(), "13800138000", message); e != nil || calls.Load() != 1 {
				t.Fatal("send failed", e, calls.Load())
			}
			if strings.Contains(fmt.Sprintf("%v %#v", sender, sender), "synthetic-secret231") {
				t.Fatal("secret formatter")
			}
		})
	}
}

func TestBusinessSMS231RejectsRedirectAndNeverRetries(t *testing.T) {
	var calls atomic.Int64
	client, _ := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Location", "https://api.smsbao.com/other")
		w.WriteHeader(302)
	}))
	r := &businesssms.Resolved{Snapshot: businesssms.Snapshot{Revision: 1, Provider: "smsbao", Template: "{code}", Phone: "13800138000", Fingerprint: strings.Repeat("a", 64)}, Credentials: smsgateway.Credentials{User: "user", Pass: "secret", Sign: "sign"}}
	input := notice.Input{Scene: "balance", SiteName: "Test", At: time.Now(), Params: map[string]string{"money": "0.00", "msgmoney": "10.00"}}
	sender, e := NewSMSMerchant231(r, input, client)
	if e != nil {
		t.Fatal(e)
	}
	message, _ := notice.Render(input)
	if e = sender.SendMerchantNotice(context.Background(), "13800138000", message); e == nil || calls.Load() != 1 {
		t.Fatal("redirect followed or send retried", e, calls.Load())
	}
}
