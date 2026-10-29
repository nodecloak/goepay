package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fuiou3Channel(payType string) PluginChannel {
	return PluginChannel{
		ID: 12, Plugin: "fuiou3", PayType: payType,
		Config: map[string]string{"appid": "M01", "appsecret": fuiou2TestPriv, "appkey": fuiou2TestPub, "appswitch": "0"},
	}
}

func TestPluginFuiou3RegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("fuiou3")
	if !ok || spec.Title != "富友支付(互联网)" || !spec.Serves("wxpay") || spec.Serves("qqpay") {
		t.Fatal("fuiou3 spec", spec, ok)
	}
	if _, e := BuildPluginChannel(fuiou3Channel("alipay")); e != nil {
		t.Fatal(e)
	}
	missing := fuiou3Channel("alipay")
	delete(missing.Config, "appkey")
	if _, e := BuildPluginChannel(missing); e == nil {
		t.Fatal("accepted missing public key")
	}
	if _, e := BuildPluginChannel(fuiou3Channel("paypal")); e == nil {
		t.Fatal("accepted unserved pay type")
	}
}

// fuiou3TestKeys returns the shared keypair parsed as rsa keys for the test
// server to decrypt requests and encrypt replies.
func fuiou3TestKeys(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	priv, e := pluginPrivateKey(fuiou2TestPriv)
	if e != nil {
		t.Fatal(e)
	}
	pub, e := pluginPublicKey(fuiou2TestPub)
	if e != nil {
		t.Fatal(e)
	}
	return priv, pub
}

// fuiou3Encode chunk-encrypts like the plugin (for building replies/callbacks).
func fuiou3Encode(t *testing.T, pub *rsa.PublicKey, data []byte) string {
	t.Helper()
	chunk := pub.Size() - 11
	var out []byte
	for len(data) > 0 {
		n := chunk
		if n > len(data) {
			n = len(data)
		}
		enc, e := rsa.EncryptPKCS1v15(rand.Reader, pub, data[:n])
		if e != nil {
			t.Fatal(e)
		}
		out = append(out, enc...)
		data = data[n:]
	}
	return base64.StdEncoding.EncodeToString(out)
}

func fuiou3Decode(t *testing.T, priv *rsa.PrivateKey, b64 string) []byte {
	t.Helper()
	raw, e := base64.StdEncoding.DecodeString(b64)
	if e != nil {
		t.Fatal(e)
	}
	chunk := priv.Size()
	var out []byte
	for len(raw) > 0 {
		dec, e := rsa.DecryptPKCS1v15(rand.Reader, priv, raw[:chunk])
		if e != nil {
			t.Fatal(e)
		}
		out = append(out, dec...)
		raw = raw[chunk:]
	}
	return out
}

func TestPluginFuiou3CreateNativeRoundTrip(t *testing.T) {
	priv, pub := fuiou3TestKeys(t)
	var gotReq map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var env map[string]string
		json.Unmarshal(b, &env)
		plain := fuiou3Decode(t, priv, env["message"])
		json.Unmarshal(plain, &gotReq)
		replyData, _ := json.Marshal(map[string]string{"order_info": "https://qr.fuiou/x", "fy_order_id": "FY1"})
		msg := fuiou3Encode(t, pub, replyData)
		resp, _ := json.Marshal(map[string]string{"resp_code": "0000", "message": msg})
		w.Write(resp)
	}))
	defer srv.Close()
	p := fuiou3WithHost(t, "alipay", srv.URL)
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.fuiou/x" || res.ProviderTradeNo != "FY1" {
		t.Fatal(res, e)
	}
	if gotReq["order_id"] != "T1" || gotReq["order_amt"] != "100" || gotReq["order_pay_type"] != "ALIPAY" || gotReq["mchnt_cd"] != "M01" {
		t.Fatal("decrypted request", gotReq)
	}
}

func TestPluginFuiou3CreateRejectAndUnknown(t *testing.T) {
	priv, _ := fuiou3TestKeys(t)
	var mode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		_ = priv
		switch mode {
		case "reject":
			resp, _ := json.Marshal(map[string]string{"resp_code": "9999", "resp_desc": "closed"})
			w.Write(resp)
		case "garbage":
			io.WriteString(w, "not json")
		case "5xx":
			w.WriteHeader(502)
		}
	}))
	defer srv.Close()
	p := fuiou3WithHost(t, "alipay", srv.URL)
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100}
	mode = "reject"
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("reject must be definitive", e)
	}
	for _, m := range []string{"garbage", "5xx"} {
		mode = m
		var unknown *UnknownOutcome
		if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
			t.Fatal("unknown expected", m, e)
		}
	}
}

func TestPluginFuiou3Notification(t *testing.T) {
	_, pub := fuiou3TestKeys(t)
	p, _ := BuildPluginChannel(fuiou3Channel("alipay"))
	v := p.(NotificationVerifier)
	mkCallback := func(fields map[string]string) []byte {
		plain, _ := json.Marshal(fields)
		msg := fuiou3Encode(t, pub, plain)
		raw, _ := json.Marshal(map[string]string{"message": msg})
		return raw
	}
	good := mkCallback(map[string]string{"order_st": "1", "order_id": "T1", "order_amt": "100", "order_fas_ssn": "FAS1", "openid": "o1"})
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := v.VerifyNotification(context.Background(), nil, good, exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "FAS1" || ev.Amount != 100 {
		t.Fatal(ev, e)
	}
	// non-success status
	fail := mkCallback(map[string]string{"order_st": "0", "order_id": "T1", "order_amt": "100"})
	if _, e := v.VerifyNotification(context.Background(), nil, fail, exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success", e)
	}
	// wrong amount / trade
	if _, e := v.VerifyNotification(context.Background(), nil, good, ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e := v.VerifyNotification(context.Background(), nil, good, ExpectedPayment{TradeNo: "X", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// undecryptable message -> authentication failure
	bad, _ := json.Marshal(map[string]string{"message": base64.StdEncoding.EncodeToString([]byte("garbage-not-rsa-block-garbage-not-rsa-block-garbage-not-rsa-blockk"))})
	if _, e := v.VerifyNotification(context.Background(), nil, bad, exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad crypt", e)
	}
	if s, ct, b := p.(NotificationAcker).NotificationAck(nil); s != 200 || b != "success" || !strings.Contains(ct, "text/plain") {
		t.Fatal(s, b)
	}
}

func fuiou3WithHost(t *testing.T, payType, host string) *fuiou3Plugin {
	t.Helper()
	p, e := BuildPluginChannel(fuiou3Channel(payType))
	if e != nil {
		t.Fatal(e)
	}
	fp := p.(*fuiou3Plugin)
	fp.ep.order = host + "/aggpos/order.fuiou"
	fp.ep.refund = host + "/refund"
	return fp
}
