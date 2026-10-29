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

func fuiouChannel(payType string, apps ...string) PluginChannel {
	return PluginChannel{ID: 12, Plugin: "fuiou", PayType: payType, AppTypes: apps,
		Config: map[string]string{"appid": "MC1", "appkey": "fuiou-key", "appurl": "PFX"}}
}

func TestPluginFuiouRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("fuiou")
	if !ok || spec.Title != "富友支付(前置商户)" || !spec.Serves("wxpay") || spec.Serves("jdpay") {
		t.Fatal("fuiou spec", ok)
	}
	if _, e := BuildPluginChannel(fuiouChannel("alipay")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{fuiouChannel("jdpay")}
	for _, miss := range []string{"appid", "appkey"} {
		c := fuiouChannel("alipay")
		delete(c.Config, miss)
		bad = append(bad, c)
	}
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginFuiouSignatureVector(t *testing.T) {
	p, _ := BuildPluginChannel(fuiouChannel("alipay"))
	fp := p.(*fuiouPlugin)
	params := map[string]string{"version": "1.0", "mchnt_cd": "MC1", "random_str": "r1", "order_type": "ALIPAY", "order_amt": "123", "mchnt_order_no": "PFXT1", "txn_begin_ts": "20261029010101", "goods_des": "VIP", "term_id": "12345678", "term_ip": "1.2.3.4", "notify_url": "https://pay.example/n"}
	order := []string{"mchnt_cd", "order_type", "order_amt", "mchnt_order_no", "txn_begin_ts", "goods_des", "term_id", "term_ip", "notify_url", "random_str", "version"}
	// Independently computed with node crypto (md5 of pipe-joined fields + key).
	if got := fp.signPipe(params, order); got != "ace676c3b24d36624b1a1ae164d4bea4" {
		t.Fatal(got)
	}
}

func TestPluginFuiouCreateNative(t *testing.T) {
	var reply string
	var status int
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := mustFuiou(t, srv.URL, "alipay")
	status, reply = 200, `{"result_code":"000000","qr_code":"https://qr.fuiou/U1"}`
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", Amount: 123, ClientIP: "1.2.3.4"}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.fuiou/U1" {
		t.Fatal(res, e)
	}
	if !strings.Contains(gotPath, "preCreate") {
		t.Fatal("path", gotPath)
	}
	var sent map[string]string
	json.Unmarshal([]byte(gotBody), &sent)
	if sent["mchnt_order_no"] != "PFXT1" || sent["order_amt"] != "123" || sent["order_type"] != "ALIPAY" {
		t.Fatal("request", sent)
	}
	order := []string{"mchnt_cd", "order_type", "order_amt", "mchnt_order_no", "txn_begin_ts", "goods_des", "term_id", "term_ip", "notify_url", "random_str", "version"}
	if sent["sign"] != p.(*fuiouPlugin).signPipe(sent, order) {
		t.Fatal("sign mismatch")
	}
}

func TestPluginFuiouCreateOutcomes(t *testing.T) {
	var reply string
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := mustFuiou(t, srv.URL, "alipay")
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", Amount: 123, ClientIP: "1.2.3.4"}
	status, reply = 200, `{"result_code":"999999","result_msg":"渠道异常"}`
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business refusal must be definitive", e)
	}
	var unknown *UnknownOutcome
	for _, c := range []struct {
		s int
		b string
	}{{200, "not json"}, {500, "boom"}, {200, `{"foo":"bar"}`}} {
		status, reply = c.s, c.b
		if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
			t.Fatal("ambiguous must be unknown", c, e)
		}
	}
}

func TestPluginFuiouNotification(t *testing.T) {
	p, _ := BuildPluginChannel(fuiouChannel("alipay"))
	v := p.(NotificationVerifier)
	// sign computed independently over the fixed field order.
	body := map[string]any{
		"mchnt_cd": "MC1", "mchnt_order_no": "PFXT1", "settle_order_amt": "123", "order_amt": "123",
		"txn_fin_ts": "20261029010200", "reserved_fy_settle_dt": "20261029", "random_str": "r2",
		"transaction_id": "U9", "reserved_buyer_logon_id": "2088xx", "sign": "5b8b64f1c6d6d4db0b220cbaec9267db",
	}
	raw, _ := json.Marshal(body)
	exp := ExpectedPayment{TradeNo: "T1", Amount: 123}
	ev, e := v.VerifyNotification(context.Background(), nil, raw, exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "U9" || ev.Amount != 123 || ev.BuyerID != "2088xx" {
		t.Fatal(ev, e)
	}
	tamper := func(k string, val any) []byte {
		m := map[string]any{}
		for kk, vv := range body {
			m[kk] = vv
		}
		m[k] = val
		b, _ := json.Marshal(m)
		return b
	}
	if _, e := v.VerifyNotification(context.Background(), nil, tamper("sign", strings.Repeat("0", 32)), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign accepted")
	}
	if _, e := v.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "T1", Amount: 999}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong amount accepted")
	}
	if _, e := v.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "OTHER", Amount: 123}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong trade accepted")
	}
	if s, _, b := p.(NotificationAcker).NotificationAck(nil); s != 200 || b != "1" {
		t.Fatal(s, b)
	}
	if s, _, b := p.(NotificationAcker).NotificationAck(ErrInvalid); s != 200 || b != "0" {
		t.Fatal(s, b)
	}
}

func TestPluginFuiouRefund(t *testing.T) {
	var reply string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := mustFuiou(t, srv.URL, "alipay")
	reply = `{"result_code":"000000","mchnt_order_no":"PFXT1","reserved_refund_amt":"1.00"}`
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "U9", RefundNo: "R1", Amount: 100, TotalAmount: 123})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 100 || !strings.Contains(gotPath, "commonRefund") {
		t.Fatal(rr, e, gotPath)
	}
	reply = `{"result_code":"888888","result_msg":"原交易不存在"}`
	var pe *ProviderError
	if _, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "U9", RefundNo: "R1", Amount: 100, TotalAmount: 123}); !errors.As(e, &pe) {
		t.Fatal(e)
	}
}

func mustFuiou(t *testing.T, base, payType string, apps ...string) Provider {
	t.Helper()
	p, e := BuildPluginChannel(fuiouChannel(payType, apps...))
	if e != nil {
		t.Fatal(e)
	}
	fp := p.(*fuiouPlugin)
	fp.preCreateURL = base + "/aggregatePay/preCreate"
	fp.wxPreCreateURL = base + "/aggregatePay/wxPreCreate"
	fp.refundURL = base + "/aggregatePay/commonRefund"
	return p
}
