package payment

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func heepayChannel(payType string, apptypes ...string) PluginChannel {
	return PluginChannel{
		ID: 14, Plugin: "heepay", PayType: payType, AppTypes: apptypes,
		Config: map[string]string{"appid": "AG01", "appkey": "PAYKEY", "appsecret": "REFKEY"},
	}
}

func TestPluginHeepayRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("heepay")
	if !ok || spec.Title != "汇付宝" || !spec.Serves("bank") || spec.Serves("paypal") {
		t.Fatal("heepay spec", spec, ok)
	}
	if len(spec.TransferTypes) == 0 {
		t.Fatal("heepay declares transfer types")
	}
	if _, e := BuildPluginChannel(heepayChannel("alipay")); e != nil {
		t.Fatal(e)
	}
	missing := heepayChannel("alipay")
	delete(missing.Config, "appkey")
	if _, e := BuildPluginChannel(missing); e == nil {
		t.Fatal("accepted missing appkey")
	}
	if _, e := BuildPluginChannel(heepayChannel("jdpay")); e == nil {
		t.Fatal("accepted unserved pay type")
	}
}

func TestPluginHeepaySignatureMatchesOriginal(t *testing.T) {
	// Fixed-order MD5 (no ksort): matches the original getPayParam $signstr.
	pairs := [][2]string{
		{"version", "1"}, {"agent_id", "AG01"}, {"agent_bill_id", "T1"},
		{"agent_bill_time", "20260101000000"}, {"pay_type", "22"}, {"pay_amt", "1.00"},
		{"notify_url", "https://pay.example/n"}, {"return_url", "https://pay.example/r"},
		{"user_ip", "1_2_3_4"},
	}
	if got := heepaySign(pairs, "PAYKEY", false); got != "17dce72f6370be691ea795ee805b269f" {
		t.Fatalf("heepay pay sign mismatch: %s", got)
	}
	cb := [][2]string{
		{"result", "1"}, {"agent_id", "AG01"}, {"jnet_bill_no", "J1"},
		{"agent_bill_id", "T1"}, {"pay_type", "22"}, {"pay_amt", "1.00"}, {"remark", ""},
	}
	if got := heepaySign(cb, "PAYKEY", false); got != "1e9a312bf102886ac0c03888abdf7ee7" {
		t.Fatalf("heepay callback sign mismatch: %s", got)
	}
}

func TestPluginHeepayCreateJump(t *testing.T) {
	p, _ := BuildPluginChannel(heepayChannel("alipay"))
	if m := p.Capabilities().Methods; len(m) != 2 || m[0] != "page" || m[1] != "wap" {
		t.Fatal("alipay methods", m)
	}
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "page", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", ClientIP: "1.2.3.4", Amount: 100})
	if e != nil || res.MaterialType != "jump" || !strings.HasPrefix(res.Material, heepayPayURL+"?") {
		t.Fatal(res, e)
	}
	u, _ := url.Parse(res.Material)
	q := u.Query()
	if q.Get("pay_type") != "22" || q.Get("pay_amt") != "1.00" || q.Get("user_ip") != "1_2_3_4" || q.Get("agent_bill_id") != "T1" || q.Get("sign") == "" || q.Get("sign_type") != "MD5" {
		t.Fatal("jump query", q)
	}
	// bank 网银 (apptype 1) uses pay_type 20 + bank sign variant.
	pb, _ := BuildPluginChannel(heepayChannel("bank", "1"))
	res, e = pb.Create(context.Background(), CreateRequest{TradeNo: "T2", Subject: "VIP", Method: "h5", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", ClientIP: "1.2.3.4", Amount: 100})
	if e != nil || res.MaterialType != "jump" {
		t.Fatal(res, e)
	}
	q2, _ := url.ParseQuery(strings.SplitN(res.Material, "?", 2)[1])
	if q2.Get("pay_type") != "20" || q2.Get("bank_card_type") != "-1" {
		t.Fatal("bank jump", q2)
	}
}

func TestPluginHeepayNotification(t *testing.T) {
	p, _ := BuildPluginChannel(heepayChannel("alipay"))
	v := p.(NotificationVerifier)
	g := url.Values{"result": {"1"}, "agent_id": {"AG01"}, "jnet_bill_no": {"J1"}, "agent_bill_id": {"T1"}, "pay_type": {"22"}, "pay_amt": {"1.00"}, "remark": {""}, "pay_user": {"u1"}, "sign": {"1e9a312bf102886ac0c03888abdf7ee7"}}
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := v.VerifyNotification(context.Background(), nil, []byte(g.Encode()), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "J1" || ev.BuyerID != "u1" || ev.Amount != 100 {
		t.Fatal(ev, e)
	}
	// bad sign
	g2 := cloneValues(g)
	g2.Set("sign", strings.Repeat("0", 32))
	if _, e := v.VerifyNotification(context.Background(), nil, []byte(g2.Encode()), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// wrong amount / trade
	if _, e := v.VerifyNotification(context.Background(), nil, []byte(g.Encode()), ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e := v.VerifyNotification(context.Background(), nil, []byte(g.Encode()), ExpectedPayment{TradeNo: "X", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// result != 1 -> invalid
	gf := cloneValues(g)
	gf.Set("result", "0")
	// recompute a valid sign for result=0 so we exercise the status branch, not sign.
	pairs := [][2]string{{"result", "0"}, {"agent_id", "AG01"}, {"jnet_bill_no", "J1"}, {"agent_bill_id", "T1"}, {"pay_type", "22"}, {"pay_amt", "1.00"}, {"remark", ""}}
	gf.Set("sign", heepaySign(pairs, "PAYKEY", false))
	if _, e := v.VerifyNotification(context.Background(), nil, []byte(gf.Encode()), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("status", e)
	}
	// ack + GET method
	if s, _, b := p.(NotificationAcker).NotificationAck(nil); s != 200 || b != "ok" {
		t.Fatal(s, b)
	}
	if m := p.(NotificationMethodsProvider).NotificationMethods(); len(m) != 2 || m[0] != http.MethodGet {
		t.Fatal(m)
	}
}

func TestPluginHeepayRefund(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(b))
		// GBK XML reply (ascii body is identical in GBK).
		gbk, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("<xml><ret_code>0000</ret_code></xml>"))
		w.Write(gbk)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(heepayChannel("alipay"))
	hp := p.(*heepayPlugin)
	hp.refundTarget = srv.URL
	rr, e := hp.Refund(context.Background(), RefundRequest{TradeNo: "T1", RefundNo: "R1", Amount: 100, TotalAmount: 100})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 100 {
		t.Fatal(rr, e)
	}
	// full-amount refund uses the agent_bill_id sign variant.
	if gotForm.Get("agent_bill_id") != "T1" || gotForm.Get("sign") != "d0bcefa4ff0c1a98a8666cdf481db17e" {
		t.Fatal("refund form", gotForm)
	}
	// rejection: ret_code != 0000
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		gbk, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("<xml><ret_code>9999</ret_code><ret_msg>bad</ret_msg></xml>"))
		w.Write(gbk)
	}))
	defer srv2.Close()
	hp.refundTarget = srv2.URL
	var pe *ProviderError
	if _, e := hp.Refund(context.Background(), RefundRequest{TradeNo: "T1", RefundNo: "R1", Amount: 100, TotalAmount: 100}); !errors.As(e, &pe) {
		t.Fatal("rejection expected", e)
	}
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vals := range v {
		out[k] = append([]string(nil), vals...)
	}
	return out
}
