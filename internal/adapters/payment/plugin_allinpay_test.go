package payment

import (
	"context"
	"crypto"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Reuses the throwaway keypair in plugin_adapay_test.go (adapayTestPriv/Pub):
// allinpay signs requests with the merchant private key and verifies callbacks
// with the platform public key. Vectors below were produced by node RSA-SHA1
// (scratch/vec.js).
func allinpayChannel(payType string, appTypes []string) PluginChannel {
	return PluginChannel{
		ID:       41,
		Plugin:   "allinpay",
		PayType:  payType,
		AppTypes: appTypes,
		Config:   map[string]string{"appmchid": "990001", "appid": "app1", "orgid": ""},
		Certificates: map[string]string{
			"appsecret": adapayTestPriv, // 商户私钥
			"appkey":    adapayTestPub,  // 通联公钥
		},
	}
}

func TestPluginAllinpayRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("allinpay")
	if !ok || spec.Title != "通联支付" || !spec.Serves("alipay") || !spec.Serves("bank") || spec.Serves("jdpay") {
		t.Fatal("allinpay spec", spec, ok)
	}
	if _, e := BuildPluginChannel(allinpayChannel("alipay", []string{"1"})); e != nil {
		t.Fatal(e)
	}
	missingPriv := allinpayChannel("alipay", []string{"1"})
	delete(missingPriv.Certificates, "appsecret")
	missingMch := allinpayChannel("alipay", []string{"1"})
	delete(missingMch.Config, "appmchid")
	notServed := allinpayChannel("jdpay", []string{"1"})
	for i, c := range []PluginChannel{missingPriv, missingMch, notServed} {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginAllinpaySignContent(t *testing.T) {
	// ksort, k=v joined by &, skip sign/empty — exactly the node vector base.
	params := map[string]string{
		"orgid": "", "appid": "app1", "cusid": "990001", "version": "11",
		"randomstr": "rnd123", "signtype": "RSA", "trxamt": "100", "reqsn": "T1",
		"paytype": "A01", "body": "商品", "validtime": "30",
		"notify_url": "https://pay.example/n", "cusip": "192.0.2.1", "sign": "x",
	}
	got := allinpaySignContent(params)
	want := "appid=app1&body=商品&cusid=990001&cusip=192.0.2.1&notify_url=https://pay.example/n&paytype=A01&randomstr=rnd123&reqsn=T1&signtype=RSA&trxamt=100&validtime=30&version=11"
	if got != want {
		t.Fatalf("sign content mismatch:\n got=%s\nwant=%s", got, want)
	}
	p, _ := BuildPluginChannel(allinpayChannel("alipay", []string{"1"}))
	ap := p.(*allinpayPlugin)
	sig, e := rsaSign(ap.privateKey, crypto.SHA1, want)
	if e != nil {
		t.Fatal(e)
	}
	const wantSig = "YxFRx22aiCCsFHh0KBrhOphK+RzKadjoRQgaPN+QkVkhMZ1RxjAoHWLXkJgyv6xYVTj7K246jUa//PL6jTGyW7GgRK/8ork2sVUiSr9ANPJAtpCfnHPjboDWCPxEWwtl3mFaGMQ1XgybNUFpe/4tdr2eoUeMDMXQvGePWEE1U5nL/BtgtShen2U1cjnTPM1W2uE3K3UutQQjWICXoPr6tiCTyjeM2IKbxhXdR6CPvpV8cxkQmSCa2LFMBjMoEIr/eiQxK/DVDWADaLzF5OXBOEvvrwvqRc2Zm6xk9ntNXq/W3NNpK768yywDYI1z7hcIPYoxeoYj081knt2jrIMjMg=="
	if sig != wantSig {
		t.Fatalf("allinpay sign vector mismatch:\n got=%s\nwant=%s", sig, wantSig)
	}
}

func TestPluginAllinpayCreateAndRefund(t *testing.T) {
	var gotForm url.Values
	var reply string
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(b))
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(allinpayChannel("alipay", []string{"1"}))
	ap := p.(*allinpayPlugin)

	// Native create via submit() against the test server.
	status = 200
	reply = `{"retcode":"SUCCESS","trxstatus":"0000","payinfo":"https://qr.allinpay/x","trxid":"A99"}`
	params := ap.publicParams("11")
	params["trxamt"] = "100"
	params["reqsn"] = "T1"
	params["paytype"] = "A01"
	params["body"] = "商品"
	params["validtime"] = "30"
	params["notify_url"] = "https://pay.example/n"
	result, e := ap.submit(context.Background(), "op", srv.URL, params)
	if e != nil || result["trxstatus"] != "0000" || result["payinfo"] != "https://qr.allinpay/x" {
		t.Fatal(result, e)
	}
	if gotForm.Get("sign") == "" || gotForm.Get("cusid") != "990001" || gotForm.Get("signtype") != "RSA" {
		t.Fatal("request form", gotForm)
	}
	// Verify the request signature with our own public key.
	sent := map[string]string{}
	for k := range gotForm {
		sent[k] = gotForm.Get(k)
	}
	if !rsaVerify(ap.publicKey, crypto.SHA1, allinpaySignContent(sent), gotForm.Get("sign")) {
		t.Fatal("request sign does not verify")
	}

	// retcode != SUCCESS is definitive.
	reply = `{"retcode":"FAIL","retmsg":"bad"}`
	var pe *ProviderError
	if _, e := ap.submit(context.Background(), "op", srv.URL, ap.publicParams("11")); !errors.As(e, &pe) {
		t.Fatal("comm fail not definitive", e)
	}
	// 5xx is unknown.
	status, reply = 500, "oops"
	var uo *UnknownOutcome
	if _, e := ap.submit(context.Background(), "op", srv.URL, ap.publicParams("11")); !errors.As(e, &uo) {
		t.Fatal("5xx not unknown", e)
	}

	// Refund trxstatus mapping.
	status, reply = 200, `{"retcode":"SUCCESS","trxstatus":"0000","trxid":"R1","fee":"0"}`
	rp := ap.publicParams("11")
	rp["trxamt"] = "50"
	rp["reqsn"] = "R1"
	rp["oldtrxid"] = "A99"
	rres, e := ap.submit(context.Background(), "op", srv.URL, rp)
	if e != nil || rres["trxstatus"] != "0000" {
		t.Fatal(rres, e)
	}
}

func TestPluginAllinpayNotification(t *testing.T) {
	p, _ := BuildPluginChannel(allinpayChannel("alipay", []string{"1"}))
	ap := p.(*allinpayPlugin)
	params := map[string]string{
		"trxstatus": "0000", "cusorderid": "T1", "trxid": "A99", "initamt": "100",
		"acct": "oABC", "chnltrxid": "CH1", "cusid": "990001", "appid": "app1",
	}
	const sig = "Gjw5jV3MTPvFjYyGLphAJW11kwMGwKsQBH2bnXNUijR8drjPcSc680aFWLcFqBLgLPREqG+KVddtBa7Y9/xrnPrHKXTdHQr8JvlWVnRlWm0V/TSABtSEqy0ndNjlPhPvjoDmrx4qX+byAX20AYqLa9Qtw1E1xJHij9dCj6+LLkkfMuk/WAbNjQ5MJHvRRWG3ZesMmiT8IB0q2FBDhg9KGl7TYjpuaaRBLD9DpC2Xk8TQhmVKM1XSHqlDNQSqzYykluwis1eS+NI8QFUdUsu2b0mOgEvOM7z9msBn5dpyJE4RCwli4Vys+s3Gqv5Sl0dNJjwhrA7eowX+/aiECVM0kg=="
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	form.Set("sign", sig)
	raw := []byte(form.Encode())
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := ap.VerifyNotification(context.Background(), nil, raw, exp)
	if e != nil || ev.State != "SUCCESS" || ev.Amount != 100 || ev.ProviderTradeNo != "A99" || ev.BuyerID != "oABC" {
		t.Fatal(ev, e)
	}
	// Bad signature.
	bad := url.Values{}
	for k, v := range params {
		bad.Set(k, v)
	}
	bad.Set("initamt", "200") // change a signed field so the sig no longer matches
	bad.Set("sign", sig)
	if _, e := ap.VerifyNotification(context.Background(), nil, []byte(bad.Encode()), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("tampered accepted", e)
	}
	// Correct sig but wrong expected amount / trade no.
	if _, e := ap.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount")
	}
	if _, e := ap.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade")
	}
	// Ack bodies.
	if st, _, b := ap.NotificationAck(nil); st != 200 || b != "success" {
		t.Fatal("ack ok", st, b)
	}
	if _, _, b := ap.NotificationAck(errors.New("x")); b != "fail" {
		t.Fatal("ack fail", b)
	}
}
