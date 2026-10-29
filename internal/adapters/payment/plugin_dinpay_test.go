package payment

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const dinpayTestPrivHex = "3945208F7B2144B13F36E38AC6D39F95889393692860B51A42FB81EF4DF7C5B8"

func dinpayTestPubHex(t *testing.T) string {
	t.Helper()
	priv, err := sm2ParsePrivateHex(dinpayTestPrivHex)
	if err != nil {
		t.Fatal(err)
	}
	return "04" + hex.EncodeToString(sm2FieldBytes(priv.x)) + hex.EncodeToString(sm2FieldBytes(priv.y))
}

func dinpayChannel(t *testing.T, payType string) PluginChannel {
	return PluginChannel{ID: 12, Plugin: "dinpay", PayType: payType, Config: map[string]string{
		"appid": "DM1", "appsecret": dinpayTestPrivHex, "appkey": dinpayTestPubHex(t),
	}}
}

func TestPluginDinpayRegistryAndBuild(t *testing.T) {
	spec, ok := LookupPlugin("dinpay")
	if !ok || spec.Title != "智付" || !spec.Serves("wxpay") || spec.Serves("qqpay") {
		t.Fatal("dinpay spec", ok)
	}
	if _, e := BuildPluginChannel(dinpayChannel(t, "bank")); e != nil {
		t.Fatal(e)
	}
	miss := dinpayChannel(t, "alipay")
	delete(miss.Config, "appsecret")
	badKey := dinpayChannel(t, "alipay")
	badKey.Config["appkey"] = strings.Repeat("00", 64) // off curve
	for i, c := range []PluginChannel{miss, badKey, dinpayChannel(t, "qqpay")} {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatalf("accepted bad channel %d", i)
		}
	}
	// appmchid overrides appid as the merchant number.
	sub := dinpayChannel(t, "alipay")
	sub.Config["appmchid"] = "SUBMCH"
	p, e := BuildPluginChannel(sub)
	if e != nil {
		t.Fatal(e)
	}
	if p.(*dinpayPlugin).mchID != "SUBMCH" {
		t.Fatal("appmchid not applied")
	}
}

// The upstream decrypts data with the SM4 key it recovers from encryptionKey;
// since encryptionKey is SM2-encrypted to the platform public key (our test
// key pair), the test server can mimic the upstream: recover the SM4 key with
// the private key, SM4-CBC-decrypt data, and confirm the signed ciphertext
// matches. This independently exercises the full assemble path.
func dinpayDecodeRequest(t *testing.T, priv *sm2PrivateKey, iv []byte, body []byte) map[string]string {
	t.Helper()
	var env map[string]string
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatal(err)
	}
	// Verify the merchant signature over data with the merchant public key.
	der, err := base64.StdEncoding.DecodeString(env["sign"])
	if err != nil {
		t.Fatal(err)
	}
	merchantPub := &sm2PublicKey{x: priv.x, y: priv.y}
	if !sm2VerifyASN1(merchantPub, "", []byte(env["data"]), der) {
		t.Fatal("request signature did not verify")
	}
	// Recover the SM4 key: encryptionKey = base64(0x04 || C1C3C2).
	encKey, err := base64.StdEncoding.DecodeString(env["encryptionKey"])
	if err != nil || len(encKey) < 1 || encKey[0] != 0x04 {
		t.Fatal("bad encryptionKey")
	}
	sm4Key, err := sm2DecryptC1C3C2(priv, encKey[1:])
	if err != nil {
		t.Fatal("sm2 decrypt key:", err)
	}
	ct, err := base64.StdEncoding.DecodeString(env["data"])
	if err != nil {
		t.Fatal(err)
	}
	pt, err := sm4DecryptCBC(sm4Key, iv, ct)
	if err != nil {
		t.Fatal("sm4 decrypt:", err)
	}
	var data map[string]string
	if err := json.Unmarshal(pt, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPluginDinpayCreateNative(t *testing.T) {
	priv, _ := sm2ParsePrivateHex(dinpayTestPrivHex)
	iv, _ := base64.StdEncoding.DecodeString(dinpaySM4IVBase)
	var captured map[string]string
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		captured = dinpayDecodeRequest(t, priv, iv, b)
		io.WriteString(w, reply)
	}))
	defer srv.Close()

	p, _ := BuildPluginChannel(dinpayChannel(t, "wxpay"))
	dp := p.(*dinpayPlugin)
	dp.baseURL = srv.URL // intercept upstream calls

	reply = `{"code":"0000","data":"{\"qrcode\":\"weixin://x\",\"channelNumber\":\"CN1\"}"}`
	res, err := dp.Create(context.Background(), CreateRequest{TradeNo: "TN1", Subject: "商品", Method: "native", NotifyURL: "https://pay.example/n", Amount: 2500, ClientIP: "1.1.1.1"})
	if err != nil || res.MaterialType != "qrcode" || res.ProviderTradeNo != "CN1" {
		t.Fatal(res, err)
	}
	if captured["orderNo"] != "TN1" || captured["payAmount"] != "25.00" || captured["paymentType"] != "WXPAY" || captured["paymentMethods"] != "SCAN" {
		t.Fatal("business params", captured)
	}

	// definitive refusal
	reply = `{"code":"9999","msg":"closed"}`
	var pe *ProviderError
	if _, e := dp.Create(context.Background(), CreateRequest{TradeNo: "TN2", Subject: "x", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.As(e, &pe) {
		t.Fatal("expected refusal", e)
	}
}

func TestPluginDinpayVerifyNotification(t *testing.T) {
	p, _ := BuildPluginChannel(dinpayChannel(t, "alipay"))
	dp := p.(*dinpayPlugin)
	priv, _ := sm2ParsePrivateHex(dinpayTestPrivHex)
	dataJSON := `{"orderStatus":"SUCCESS","orderNo":"TN9","channelNumber":"CN9","payAmount":"12.00","subOpenId":"2088xx"}`
	// The platform signs the plaintext data JSON with its private key; our test
	// key pair stands in for the platform, so a signature made with priv
	// verifies against dp.pub.
	der, err := sm2SignASN1(priv, "", []byte(dataJSON))
	if err != nil {
		t.Fatal(err)
	}
	form := func(data, sign string) []byte {
		q := "data=" + url.QueryEscape(data) + "&sign=" + url.QueryEscape(sign)
		return []byte(q)
	}
	exp := ExpectedPayment{TradeNo: "TN9", Amount: 1200}
	ev, err := dp.VerifyNotification(context.Background(), nil, form(dataJSON, base64.StdEncoding.EncodeToString(der)), exp)
	if err != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "CN9" || ev.Amount != 1200 || ev.BuyerID != "2088xx" {
		t.Fatal(ev, err)
	}
	// bad signature
	if _, e := dp.VerifyNotification(context.Background(), nil, form(dataJSON, base64.StdEncoding.EncodeToString([]byte("bad"))), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign accepted", e)
	}
	// wrong amount / trade
	if _, e := dp.VerifyNotification(context.Background(), nil, form(dataJSON, base64.StdEncoding.EncodeToString(der)), ExpectedPayment{TradeNo: "TN9", Amount: 1}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong amount accepted", e)
	}
	if _, e := dp.VerifyNotification(context.Background(), nil, form(dataJSON, base64.StdEncoding.EncodeToString(der)), ExpectedPayment{TradeNo: "X", Amount: 1200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong trade accepted", e)
	}
	// non-success
	nd := `{"orderStatus":"FAIL","orderNo":"TN9","payAmount":"12.00"}`
	ns, _ := sm2SignASN1(priv, "", []byte(nd))
	if _, e := dp.VerifyNotification(context.Background(), nil, form(nd, base64.StdEncoding.EncodeToString(ns)), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success accepted", e)
	}
	// ack
	if s, _, b := dp.NotificationAck(nil); s != 200 || b != "success" {
		t.Fatal("ack", s, b)
	}
}
