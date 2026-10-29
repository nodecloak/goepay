package payment

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func hlpayChannel(payType string, apptypes ...string) PluginChannel {
	return PluginChannel{
		ID: 15, Plugin: "hlpay", PayType: payType, AppTypes: apptypes,
		Config: map[string]string{"appid": "APP01", "appkey": fuiou2TestPriv, "appsecret": fuiou2TestPub, "channelcode": "", "appmchid": "SUB1", "appswitch": "2"},
	}
}

func TestPluginHlpayRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("hlpay")
	if !ok || spec.Title != "汇联支付" || !spec.Serves("bank") || spec.Serves("paypal") {
		t.Fatal("hlpay spec", spec, ok)
	}
	if len(spec.TransferTypes) == 0 {
		t.Fatal("hlpay declares transfer types")
	}
	if _, e := BuildPluginChannel(hlpayChannel("alipay")); e != nil {
		t.Fatal(e)
	}
	missing := hlpayChannel("alipay")
	delete(missing.Config, "appsecret")
	if _, e := BuildPluginChannel(missing); e == nil {
		t.Fatal("accepted missing public key")
	}
	if _, e := BuildPluginChannel(hlpayChannel("qqpay")); e == nil {
		t.Fatal("accepted unserved pay type")
	}
}

func TestPluginHlpaySignatureMatchesOriginal(t *testing.T) {
	p, _ := BuildPluginChannel(hlpayChannel("alipay"))
	hp := p.(*hlpayPlugin)
	// Envelope RSA2 sign over the exact ksort "k=v&..." computed independently.
	env := map[string]string{
		"appId": "APP01", "subSn": "SUB1", "timestamp": "1700000000", "requestId": "REQ1",
		"version": "1.01", "signType": "RSA2", "bizContent": `{"amount":"1.00","mchOrderNo":"T1"}`,
	}
	got, e := hp.hlpaySign(env)
	if e != nil {
		t.Fatal(e)
	}
	const want = "eaXKG3OHrQZdUVCtYJb2YWSLuv26Oz14Gt6Naqy82BZHpJyGTy9FZQF6MbI6vyiqF/EZHt5nvrjVQmW2Cl3HcRx/thZBavl/KGzVKbCYNJ0zK5YejECEaAYPBAp4AapQxAIp84E0Q9tV+u6nLZSXURa3jtn5M9lbzpoywZkXvSO6IkusN/avK0HDlBgV8Xz7C3owN09qqX0O+3d7eW4J9QCH3n3OmiDWGytkCfvaE0E+fzW1zrrWfnvD5oZSAp1cx4F41aOYIBCKihC8lx4aaADsnK6QL/3d7XwtnsFJF7C0mt/jM/Ww38B1EvtgheQFjKQMgHD1rYFsHauua6hbHQ=="
	if got != want {
		t.Fatalf("hlpay envelope sign mismatch\n got=%s\nwant=%s", got, want)
	}
}

func TestPluginHlpayCanonicalDataMatchesNode(t *testing.T) {
	// The original processDataField: parse, drop empties, deep-sort, compact.
	// Nested data object given out of order with an empty field to drop.
	in := map[string]any{"state": "3", "mchOrderNo": "T1", "payOrderNo": "P1", "amount": "1.00", "empty": ""}
	got := hlpayCanonicalData(in)
	const want = `{"amount":"1.00","mchOrderNo":"T1","payOrderNo":"P1","state":"3"}`
	if got != want {
		t.Fatalf("canonical data mismatch\n got=%s\nwant=%s", got, want)
	}
}

func TestPluginHlpayCreate(t *testing.T) {
	var gotPath string
	var gotEnv map[string]json.RawMessage
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotEnv)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	// native: a signed reply {code:200,data:{payInfo,payOrderNo},sign}.
	p := hlpayWithHost(t, "alipay", srv.URL, "1")
	reply = hlpayTestReply(t, map[string]any{"payInfo": "https://qr.hl/x", "payOrderNo": "P1"})
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.hl/x" || res.ProviderTradeNo != "P1" {
		t.Fatal(res, e)
	}
	if gotPath != "/openapi/pay/create" || gotEnv["sign"] == nil || gotEnv["bizContent"] == nil {
		t.Fatal("request", gotPath, gotEnv)
	}
	// page (PC) -> jump
	pp := hlpayWithHost(t, "alipay", srv.URL, "3")
	reply = hlpayTestReply(t, map[string]any{"payInfo": "https://pc.hl/pay", "payOrderNo": "P2"})
	res, e = pp.Create(context.Background(), CreateRequest{TradeNo: "T2", Subject: "VIP", Method: "page", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 100})
	if e != nil || res.MaterialType != "jump" || res.Material != "https://pc.hl/pay" {
		t.Fatal(res, e)
	}
}

func TestPluginHlpayCreateOutcomes(t *testing.T) {
	var status int
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := hlpayWithHost(t, "alipay", srv.URL, "1")
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100}
	status, reply = 200, `{"code":4001,"msg":"closed"}`
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business refusal must be definitive", e)
	}
	for _, c := range []struct {
		s int
		b string
	}{{200, "not json"}, {502, "bad"}} {
		status, reply = c.s, c.b
		var unknown *UnknownOutcome
		if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
			t.Fatal("unknown expected", c, e)
		}
	}
}

func TestPluginHlpayNotification(t *testing.T) {
	p, _ := BuildPluginChannel(hlpayChannel("alipay"))
	v := p.(NotificationVerifier)
	// Callback envelope signed over code/data(canonical)/msg; HL_CB_SIGN is the
	// independently computed RSA2 signature.
	const cbSign = "S6sGFpOBLrGcI+KSGkKSIj+nOUGVYlRMASFn0u6V0Col3/rpPAZD5PhTZqcFga8lsy8wGDrIlmKxwRrhSSxSDzFxZ/wx58B1/Fz4aH416zlWJwMkXJojlLI07o4Z46D5BPIaoMHQg6Kk6fpqa0brVuPa/Hy47nM+FJrRUD+eyLpCrsnVu8lOZlWEJv590S613OBGKjfdcJun7hA4XKPTkbeiAhVb2+FMhAmv57bohN22bvfAR5niKHuaW5HYTGHIJCHhmkuIw802wINyIuKNyaZQhxY7WvvcTktI0uvKRVzML6JjeI9OpBLs23bu3iHjoYD8ys47BFHvlgcgLhgBcA=="
	body := `{"code":"200","msg":"success","data":{"state":"3","mchOrderNo":"T1","payOrderNo":"P1","amount":"1.00"},"sign":"` + cbSign + `"}`
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := v.VerifyNotification(context.Background(), nil, []byte(body), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "P1" || ev.Amount != 100 {
		t.Fatal(ev, e)
	}
	// bad sign
	bad := strings.Replace(body, cbSign, "AAAA", 1)
	if _, e := v.VerifyNotification(context.Background(), nil, []byte(bad), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// wrong amount / trade
	if _, e := v.VerifyNotification(context.Background(), nil, []byte(body), ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e := v.VerifyNotification(context.Background(), nil, []byte(body), ExpectedPayment{TradeNo: "X", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	if s, _, b := p.(NotificationAcker).NotificationAck(nil); s != 200 || b != "success" {
		t.Fatal(s, b)
	}
}

func TestPluginHlpayRefund(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		io.ReadAll(r.Body)
		io.WriteString(w, hlpayTestReply(t, map[string]any{"instOrderNo": "I1", "refundAmount": "0.50"}))
	}))
	defer srv.Close()
	p := hlpayWithHost(t, "alipay", srv.URL, "1")
	rr, e := p.Refund(context.Background(), RefundRequest{ProviderTradeNo: "P1", RefundNo: "R1", Amount: 50, TotalAmount: 100})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 50 || gotPath != "/openapi/pay/refund" {
		t.Fatal(rr, e, gotPath)
	}
}

// ---- helpers ----

func hlpayWithHost(t *testing.T, payType, host string, apptypes ...string) *hlpayPlugin {
	t.Helper()
	p, e := BuildPluginChannel(hlpayChannel(payType, apptypes...))
	if e != nil {
		t.Fatal(e)
	}
	// hlpayHost is a package const; redirect by overriding postJSON target via
	// a per-plugin host field is not present, so wrap the client instead.
	hp := p.(*hlpayPlugin)
	hp.testHost = host
	return hp
}

// hlpayTestReply signs {code:200,data,sign} with the test private key (acting
// as the platform key, equal to our public key here).
func hlpayTestReply(t *testing.T, data map[string]any) string {
	t.Helper()
	p, _ := BuildPluginChannel(hlpayChannel("alipay"))
	hp := p.(*hlpayPlugin)
	base := map[string]string{"code": "200", "msg": "success", "data": hlpayCanonicalData(data)}
	sign, e := hp.hlpaySign(base)
	if e != nil {
		t.Fatal(e)
	}
	dataJSON, _ := json.Marshal(data)
	env := map[string]json.RawMessage{
		"code": json.RawMessage(`"200"`),
		"msg":  json.RawMessage(`"success"`),
		"data": json.RawMessage(dataJSON),
		"sign": mustJSONString(sign),
	}
	out, _ := json.Marshal(env)
	return string(out)
}

func mustJSONString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}
