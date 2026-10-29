package payment

import (
	"context"
	"crypto"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// adapayTestPriv/Pub are a throwaway 2048-bit keypair generated with node; the
// signature vectors below were produced independently by node's crypto (see
// scratch/vec.js, RSA-SHA1). The adapay public key the plugin uses for
// callback verification is the FIXED AdaPay key compiled into the plugin, so
// here we verify the plugin against a notify signed with that same test key by
// overriding the plugin's publicKey in the test.
const adapayTestPriv = "MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQCF16+cZH+G7+n60rgbsiP3KNN8EpmB8pUx6GMvDo+QaSbD2jZE1i1Fr7emMv3Xm6CBRxg40rYRRuJROp64Nv7PAjpSxJ0aq1K3mq7PHLX/RgBK7d78ZATaQ+LCqM+UcHkF8XAt1RgEbBbUBC77RsvtB61nRMpLP48kdmyzD2lPC8jSpR8yTVZXlAFWRsqt26Ykw/WoksG3oMf5yvdR3cu5wZ/b80Ez5WS9thgRXrp9BWlEsCk6l4cWz43eAYojJVLaHiQfKmLoh3k6Hy1R2pTt67jY5mFM77klO2LT7MbtOkfkN7qWs8oyWxHZuzgEWSlEuCIIzN15ExiQ7m6CSEDFAgMBAAECggEAF32sIlBXO6k3EWFTk/TXsCUXvUKMc8zOV8SYR+WP2AJEngJaHQnUR1NYcdCoUePML3R5cVu2PeEJjsUxn1ZzsYSMFQPSKSChs3zwKoS5ArVYDs6iIP0zBtX8Yjr7t+tnql3eWQT6IqiP9yVG065w00jSK/pcYQmmR3mLiX/udUN0vRy86ATV+yn1rDGTBBlceDkWTg82zaotITlR6JiLPG86bJAClJkD5eKAybaGHDPfBSufbHTIVQ996Jx89vuXsRB3O652NzpmA68kFbV3LICiYrjzHJvNUNpeWPjGYcCM8UnFvrltIc42j/Ui8geZSaxfZs2SXEsSDDW1RowvjwKBgQC9Rqmsx5mq70vwVAVOEDGN8yTXQdnitLGp5yEjJOk3bl5vREjT1fquiMPbXLhfeRdbdOIVzIVyQ+Xm3tYugIAcdTAWJYIkuGRNLQd73kC5TIbE5rvQGYdqXjq7Nc09MaZBWMLiO6ttEyg6/sesFz/Rq+J4otzjRoENNV5PQ0StTwKBgQC1BmQSdyBeNln2w/paEhalkspFx7D2UPP5TuOrVnESduM+N8E8iOQfwh5SjmgyyFQjLTXairLL/pgVrHffW7mpcX1r2+hdZdxker6bf9RfJF0+YmsRoMDVMG+4EYSbHTSwAXmyOyiBMZi90D3lH+iVUWwtdimjmjIzymdIVhJzqwKBgQCjCDSqcJr6U3hzn8uZ+/TTMjXve/czgTTvBfjTDr4bUuPgr06TuOJeZ8tcLbdZ9TGbQ0hiQnJHhBiJDjQqheDTolUzHkfFXVZ362tg1IWnljUXcS8hBnxPsK6iBqusJBHyg4QJ3h/AqhmjB0XyA2yjK4y9IHtnAJm0SN3dJDIJnQKBgQCfpJc0QaJl84C7QnHSZQlb02xWxZimYRYH0y6st3rYUcht4jWj8QVdjI+5NqVlw42PyBsqNA8iH9B3rLW6YOYHLHLzX+g4FdCbPDREZiMgFK22iozp6t6kJCciKTDMqqEAZwyvxcQey2JsYu1y1wUCSeAg54gpI58KVYFoJZ/4KQKBgCTmiHgZcSU4w8Vphfoy35xFGkPFqp0Txf6TvB1UbCHCgFwmQPE0ZBPCc3AGIAdkUX9275cgZHX8WzonnP+VzamyFbG/fcROjqHQuhGr/OeeXyGbY4N0GRms7ijYHvjCuZ2IL4LqIuCs7s6nBCn89+o8tpBum08crYr8TkgrP/0H"

const adapayTestPub = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAhdevnGR/hu/p+tK4G7Ij9yjTfBKZgfKVMehjLw6PkGkmw9o2RNYtRa+3pjL915uggUcYONK2EUbiUTqeuDb+zwI6UsSdGqtSt5quzxy1/0YASu3e/GQE2kPiwqjPlHB5BfFwLdUYBGwW1AQu+0bL7QetZ0TKSz+PJHZssw9pTwvI0qUfMk1WV5QBVkbKrdumJMP1qJLBt6DH+cr3Ud3LucGf2/NBM+VkvbYYEV66fQVpRLApOpeHFs+N3gGKIyVS2h4kHypi6Id5Oh8tUdqU7eu42OZhTO+5JTti0+zG7TpH5De6lrPKMlsR2bs4BFkpRLgiCMzdeRMYkO5ugkhAxQIDAQAB"

func adapayChannel(payType string, appTypes []string) PluginChannel {
	return PluginChannel{
		ID:       31,
		Plugin:   "adapay",
		PayType:  payType,
		AppTypes: appTypes,
		Config:   map[string]string{"appid": "app123", "appkey": "api-key-live"},
		Certificates: map[string]string{
			"appsecret": adapayTestPriv,
		},
	}
}

func TestPluginAdapayRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("adapay")
	if !ok || spec.Title != "AdaPay聚合支付" || !spec.Serves("alipay") || !spec.Serves("bank") || spec.Serves("qqpay") {
		t.Fatal("adapay spec", spec, ok)
	}
	if _, e := BuildPluginChannel(adapayChannel("alipay", []string{"1"})); e != nil {
		t.Fatal(e)
	}
	missing := adapayChannel("alipay", []string{"1"})
	delete(missing.Certificates, "appsecret")
	badKey := adapayChannel("alipay", []string{"1"})
	badKey.Certificates["appsecret"] = "not-a-key"
	notServed := adapayChannel("qqpay", []string{"1"})
	for i, c := range []PluginChannel{missing, badKey, notServed} {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginAdapayRequestSignature(t *testing.T) {
	p, e := BuildPluginChannel(adapayChannel("alipay", []string{"1"}))
	if e != nil {
		t.Fatal(e)
	}
	ap := p.(*adapayPlugin)
	body := ap.orderBody(CreateRequest{TradeNo: "T1", Subject: "商品", Amount: 100, NotifyURL: "https://pay.example/n"}, "alipay_qr")
	gotBody := adapayCompactJSON(body)
	wantBody := `{"order_no":"T1","pay_channel":"alipay_qr","pay_amt":"1.00","goods_title":"商品","goods_desc":"商品","currency":"cny","notify_url":"https:\/\/pay.example\/n","app_id":"app123"}`
	if gotBody != wantBody {
		t.Fatalf("body mismatch:\n got=%s\nwant=%s", gotBody, wantBody)
	}
	// Independently computed by node RSA-SHA1 over gateway+endpoint+body.
	sig, se := rsaSign(ap.privateKey, crypto.SHA1, adapayGateway+"/v1/payments"+gotBody)
	if se != nil {
		t.Fatal(se)
	}
	const want = "LiNyd0fqB+g2ggbrimqUHKUKrXOiWYPyvJB6rnD9fP/sLE4piVin5qzLpVYeKps9PtNpQtKNXXM+w6LrlYwH86UeBsdt5uvTsupHUnv01sCR3zhmKqFVqyMfSDGWXsY5Xe1yJjAJUl6BiXXkriSMT14g1+ZkowZBYDCLV9b9d5p54ogHhAs0tHdXWChVJbXnahlyI6DCfs/zTnyzEcR/BKEoMOv9Im0aLcqz0XDiBKb5Dj1gQnGhkKPr4bnBqg8gbMrSAuMQQvXK040bxfZ310R3Gx2A/y6KyR7nhrfjqCPYGSqZNJKprClAQMnKPFo07zT7ikytQCs4OhLqD+WIlg=="
	if sig != want {
		t.Fatalf("adapay sign vector mismatch:\n got=%s\nwant=%s", sig, want)
	}
}

func TestPluginAdapayCreateNativeAndRefund(t *testing.T) {
	var lastPath, lastAuth, lastSig, reqBody string
	var reply string
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		lastAuth = r.Header.Get("Authorization")
		lastSig = r.Header.Get("Signature")
		b, _ := io.ReadAll(r.Body)
		reqBody = string(b)
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(adapayChannel("alipay", []string{"1"}))
	ap := p.(*adapayPlugin)

	// Drive request() directly against the test server (gateway is a const).
	status = 200
	reply = `{"data":"{\"status\":\"succeeded\",\"id\":\"pay_x\",\"expend\":{\"qrcode_url\":\"https://qr.adapay/x\"}}"}`
	body := ap.orderBody(CreateRequest{TradeNo: "T1", Subject: "商品", Amount: 100, NotifyURL: "https://pay.example/n"}, "alipay_qr")
	data, e := ap.request(context.Background(), "op", http.MethodPost, srv.URL, "/v1/payments", body)
	if e != nil {
		t.Fatal(e)
	}
	if lastPath != "/v1/payments" || lastAuth != "api-key-live" || lastSig == "" {
		t.Fatal("headers", lastPath, lastAuth, lastSig)
	}
	if !strings.Contains(reqBody, `"pay_channel":"alipay_qr"`) {
		t.Fatal("body", reqBody)
	}
	if jsonText(adapayObject(data, "expend"), "qrcode_url") != "https://qr.adapay/x" {
		t.Fatal("expend", data)
	}

	// Business failure: data.status failed with no expend.
	reply = `{"data":"{\"status\":\"failed\",\"error_code\":\"E1\",\"error_msg\":\"x\"}"}`
	var pe *ProviderError
	if _, e := ap.request(context.Background(), "op", http.MethodPost, srv.URL, "/v1/payments", body); !errors.As(e, &pe) {
		t.Fatal("business fail not definitive", e)
	}
	// Message-only error (no data).
	reply = `{"message":"bad request"}`
	if _, e := ap.request(context.Background(), "op", http.MethodPost, srv.URL, "/v1/payments", body); !errors.As(e, &pe) {
		t.Fatal("message error not definitive", e)
	}
	// Unknown: 5xx.
	status, reply = 500, "oops"
	var uo *UnknownOutcome
	if _, e := ap.request(context.Background(), "op", http.MethodPost, srv.URL, "/v1/payments", body); !errors.As(e, &uo) {
		t.Fatal("5xx not unknown", e)
	}

	// Refund mapping via request().
	status, reply = 200, `{"data":"{\"status\":\"succeeded\",\"id\":\"re_1\",\"refund_amt\":\"1.00\"}"}`
	rbody := map[string]string{"payment_id": "pay_x", "refund_order_no": "R1", "refund_amt": "1.00"}
	rd, e := ap.request(context.Background(), "op", http.MethodPost, srv.URL, "/v1/payments/pay_x/refunds", rbody)
	if e != nil || jsonText(rd, "status") != "succeeded" {
		t.Fatal(rd, e)
	}
}

func TestPluginAdapayNotification(t *testing.T) {
	p, _ := BuildPluginChannel(adapayChannel("alipay", []string{"1"}))
	ap := p.(*adapayPlugin)
	// Override the fixed AdaPay key with our test public key so we can sign.
	pub, e := pluginPublicKey(adapayTestPub)
	if e != nil {
		t.Fatal(e)
	}
	ap.publicKey = pub

	data := `{"status":"succeeded","id":"pay_x","order_no":"T1","pay_amt":"1.00","expend":{"sub_open_id":"oABC"}}`
	const sig = "Bprz5Ro56flCRRqwiW3jSrFASzqInzfxouJ/gHlYtDIy6X64F+OG7KF8QHlhD7l6cUZPL0z71L4UsGSuZwsKNzge2Vyvf7melocAJN8fERK+j0p7yND1RPMpY458vCeNB6V+qGTTImPz6P2I1VgbB33dlTLfAh2sxAyBIZLkjOvkaq8FI063pf2XZokGuCpq0ez/cktLKfEhGIsVfNGZFwMlJhQJNLOgAUOOFSo9lqrKuuptksPAMHPVtrlZ6cVFtBOQNfJWr0TAVFH0JJz6Mf3G8tcyqDKEeu1xOKHcpAcmq+vErxpdmzbxIFPVRbAkj3gH4NIfuohuLhvT/FYH4w=="
	form := url.Values{"sign": {sig}, "data": {data}}
	raw := []byte(form.Encode())
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := ap.VerifyNotification(context.Background(), nil, raw, exp)
	if e != nil || ev.State != "SUCCESS" || ev.Amount != 100 || ev.ProviderTradeNo != "pay_x" || ev.BuyerID != "oABC" {
		t.Fatal(ev, e)
	}
	// Bad signature.
	bad := url.Values{"sign": {sig}, "data": {data + " "}}
	if _, e := ap.VerifyNotification(context.Background(), nil, []byte(bad.Encode()), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("tampered data accepted", e)
	}
	// Wrong amount / trade no.
	if _, e := ap.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount")
	}
	if _, e := ap.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade")
	}
	// Ack.
	if st, _, b := ap.NotificationAck(nil); st != 200 || b != "Ok" {
		t.Fatal("ack ok", st, b)
	}
	if _, _, b := ap.NotificationAck(errors.New("x")); b != "No" {
		t.Fatal("ack fail", b)
	}
}
