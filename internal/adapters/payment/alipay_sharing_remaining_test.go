package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAlipaySharingRemainingUsesUnsettledSourceAndExplicitSignedAmount(t *testing.T) {
	merchant, platform := testKey(t), testKey(t)
	amount := json.RawMessage(`"0.00"`)
	calls := 0
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		r.ParseForm()
		var input map[string]string
		json.Unmarshal([]byte(r.Form.Get("biz_content")), &input)
		if r.Form.Get("method") != "alipay.trade.order.onsettle.query" || input["trade_no"] != "ALI_ORIGINAL_TRADE" || input["royalty_source"] != "unsettled" {
			t.Error("remaining lookup changed product or original trade", r.Form.Get("method"), input)
		}
		body := map[string]any{"code": "10000"}
		if amount != nil {
			body["unsettled_amount"] = amount
		}
		node, _ := json.Marshal(body)
		hash := sha256.Sum256(node)
		sig, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, hash[:])
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_order_onsettle_query_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(sig)})
	}))
	defer up.Close()
	p, e := NewAlipay(AlipayConfig{AppID: "local-app", SellerID: "2088000000000099", PrivateKey: merchant, PublicKey: &platform.PublicKey, EnableChannelSharing: true, GatewayURL: up.URL, HTTPClient: up.Client()})
	if e != nil {
		t.Fatal(e)
	}
	if n, e := p.ChannelSharingRemaining(context.Background(), "ALI_ORIGINAL_TRADE"); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	amount = json.RawMessage(`"1.23"`)
	if n, e := p.ChannelSharingRemaining(context.Background(), "ALI_ORIGINAL_TRADE"); e != nil || n != 123 {
		t.Fatal(n, e)
	}
	for _, bad := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`"-0.01"`), json.RawMessage(`"1.001"`), json.RawMessage(`{}`)} {
		amount = bad
		if n, e := p.ChannelSharingRemaining(context.Background(), "ALI_ORIGINAL_TRADE"); e == nil {
			t.Fatal("invalid remaining became credible amount", string(bad), n)
		}
	}
	before := calls
	if _, e := p.ChannelSharingRemaining(context.Background(), ""); e == nil || calls != before {
		t.Fatal("invalid original trade reached upstream", e, calls)
	}
	if p.ChannelSharingCapabilities().FinishSupported {
		t.Fatal("remaining lookup was incorrectly treated as standalone finish proof")
	}
}
