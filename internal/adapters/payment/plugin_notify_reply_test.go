package payment

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The original huifu notify() answers a verified paid callback with
// "RECV_ORD_ID_<TRADE_NO>"; other results keep the plain failure reply.
func TestPluginHuifuNotificationResponseEchoesOrder(t *testing.T) {
	const nraw = `{"trans_stat":"S","trans_amt":"1.00","req_seq_id":"T1","hf_seq_id":"HF1","wx_response":{"sub_openid":"o-1"}}`
	built, e := BuildPluginChannel(huifuChannel("wxpay"))
	if e != nil {
		t.Fatal(e)
	}
	r, ok := built.(NotificationResponder)
	if !ok {
		t.Fatal("huifu must answer from the callback")
	}
	form := huifuEncodeForm(map[string]string{"resp_data": nraw, "sign": "ignored-here"})
	status, kind, body := r.NotificationResponse(nil, form, nil)
	if status != 200 || !strings.HasPrefix(kind, "text/plain") || body != "RECV_ORD_ID_T1" {
		t.Fatal(status, kind, body)
	}
	if status, _, _ = r.NotificationResponse(nil, form, ErrAuthentication); status != 0 {
		t.Fatal("a failed verification must keep the plain failure reply", status)
	}
	odd := huifuEncodeForm(map[string]string{"resp_data": `{"req_seq_id":"T1<script>"}`, "sign": "x"})
	if status, _, _ = r.NotificationResponse(nil, odd, nil); status != 0 {
		t.Fatal("only a plain order number may be echoed")
	}
}

// The original lakalamoss notify() calls MossClient::echoNotify: the callback
// head's serviceSn/serviceId come back in a signed head with code 000000.
func TestPluginLakalamossNotificationResponseSignsHead(t *testing.T) {
	p, env := lakalamossSetup(t)
	lakalamossNow = func() time.Time { return time.Date(2026, 10, 29, 1, 2, 3, 0, time.UTC) }
	defer func() { lakalamossNow = time.Now }()
	callback, _ := json.Marshal(map[string]any{"head": map[string]any{"code": "000000", "serviceSn": "SN123", "serviceId": "lfops.moss.order.pay"}, "requestEncrypted": "x", "sign": "y"})

	status, kind, body := p.NotificationResponse(nil, callback, nil)
	if status != 200 || !strings.HasPrefix(kind, "application/json") {
		t.Fatal(status, kind, body)
	}
	var reply map[string]any
	if e := json.Unmarshal([]byte(body), &reply); e != nil {
		t.Fatal(e)
	}
	head, _ := reply["head"].(map[string]any)
	if head["code"] != "000000" || head["desc"] != "success" || head["serviceSn"] != "SN123" || head["serviceId"] != "lfops.moss.order.pay" || head["serviceTime"] != "20261029090203" {
		t.Fatal(reply)
	}
	// Independently rebuilt sign content (ksort + json_encode of the head),
	// verified with the merchant public key the upstream holds.
	content := `head={"code":"000000","desc":"success","serviceId":"lfops.moss.order.pay","serviceSn":"SN123","serviceTime":"20261029090203"}`
	sig, _ := base64.StdEncoding.DecodeString(reply["sign"].(string))
	h := sha1.Sum([]byte(content))
	if e := rsa.VerifyPKCS1v15(&env.merchant.PublicKey, crypto.SHA1, h[:], sig); e != nil {
		t.Fatal("reply signature", e)
	}

	status, _, body = p.NotificationResponse(nil, callback, ErrAuthentication)
	if status != 400 || !strings.Contains(body, `"code":"000001"`) || !strings.Contains(body, `"desc":"failure"`) {
		t.Fatal("failure echo", status, body)
	}
	bad, _ := json.Marshal(map[string]any{"head": map[string]any{"serviceSn": "SN\"1", "serviceId": "x"}})
	if status, _, _ = p.NotificationResponse(nil, bad, nil); status != 0 {
		t.Fatal("only plain identifiers may be echoed")
	}
	if status, _, _ = p.NotificationResponse(nil, []byte("not json"), nil); status != 0 {
		t.Fatal("unreadable callback keeps the built-in reply")
	}
}
