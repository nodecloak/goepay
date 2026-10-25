package adminnotice

import (
	"context"
	"encoding/json"
	"fmt"
	notice "goepay/internal/merchantnotice"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMerchantNotice80VoiceTLSOriginalPayloadAndNoImplicitRetry(t *testing.T) {
	for _, mode := range []string{"accepted", "rejected", "redirect", "duplicate", "altered"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			client, dials := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil || r.Method != "POST" || r.Host != "iot.solomo-info.com" || r.URL.Path != "/admin/common/msgpush" || body["agent_id"] != "synthetic-agent80" || body["agent_secret"] != "synthetic-voice-secret80" || body["sbx_id"] != "synthetic-device80" || body["msg"] != "微信支付收款12.34元" || body["debug"] != float64(0) {
					t.Error("source voice contract changed", body)
				}
				switch mode {
				case "accepted":
					fmt.Fprint(w, `{"result":"success"}`)
				case "redirect":
					w.Header().Set("Location", "https://iot.solomo-info.com/other")
					w.WriteHeader(302)
				case "duplicate":
					fmt.Fprint(w, `{"result":"error","result":"success"}`)
				default:
					fmt.Fprint(w, `{"result":"error","msg":"synthetic-voice-secret80"}`)
				}
			}))
			input := wechatNotice80Config().Input
			sender, err := NewVoiceMerchant(VoiceMerchantConfig{Endpoint: "https://iot.solomo-info.com/admin/common/msgpush", AgentID: "synthetic-agent80", Secret: "synthetic-voice-secret80", Input: input}, client)
			if err != nil || dials.Load() != 0 {
				t.Fatal("pure voice constructor", err, dials.Load())
			}
			message, _ := notice.Render(input)
			if mode == "altered" {
				message.Text += " changed"
			}
			err = sender.SendMerchantNotice(context.Background(), "synthetic-device80", message)
			if mode == "accepted" {
				if err != nil || calls.Load() != 1 {
					t.Fatal(err, calls.Load())
				}
			} else if err == nil || strings.Contains(err.Error(), "synthetic") {
				t.Fatal("voice failure accepted or exposed secret", err)
			}
			if mode == "altered" {
				if dials.Load() != 0 {
					t.Fatal("altered source reached device")
				}
			} else if calls.Load() != 1 {
				t.Fatal("voice implicitly retried", calls.Load())
			}
			if strings.Contains(fmt.Sprintf("%v %#v", sender, sender), "synthetic-voice-secret80") {
				t.Fatal("voice credential in string")
			}
		})
	}
}
func TestMerchantNotice80VoiceDoesNotUpgradeOriginalPlaintextURLOrAcceptArbitraryGateway(t *testing.T) {
	for _, endpoint := range []string{"http://iot.solomo-info.com:9306/admin/common/msgpush", "https://127.0.0.1/admin/common/msgpush", "https://iot.solomo-info.com/other", "https://iot.solomo-info.com/admin/common/msgpush?secret=bad"} {
		if s, err := NewVoiceMerchant(VoiceMerchantConfig{Endpoint: endpoint, AgentID: "agent80", Secret: "secret80", Input: wechatNotice80Config().Input}, nil); err == nil || s != nil {
			t.Fatal("unsafe/unverified gateway silently accepted", endpoint)
		}
	}
}
