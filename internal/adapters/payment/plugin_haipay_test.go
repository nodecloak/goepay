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

func haipayChannel(payType string, apptypes ...string) PluginChannel {
	return PluginChannel{
		ID: 13, Plugin: "haipay", PayType: payType, AppTypes: apptypes,
		Config: map[string]string{"accessid": "AID", "accesskey": "SKEY", "agent_no": "A01", "merch_no": "M01", "pn": "PN1"},
	}
}

func TestPluginHaipayRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("haipay")
	if !ok || spec.Title != "海科聚合支付" || !spec.Serves("bank") || spec.Serves("jdpay") {
		t.Fatal("haipay spec", spec, ok)
	}
	if _, e := BuildPluginChannel(haipayChannel("alipay")); e != nil {
		t.Fatal(e)
	}
	missing := haipayChannel("alipay")
	delete(missing.Config, "accesskey")
	if _, e := BuildPluginChannel(missing); e == nil {
		t.Fatal("accepted missing accesskey")
	}
	if _, e := BuildPluginChannel(haipayChannel("qqpay")); e == nil {
		t.Fatal("accepted unserved pay type")
	}
}

func TestPluginHaipaySignatureMatchesOriginal(t *testing.T) {
	p, _ := BuildPluginChannel(haipayChannel("alipay"))
	hp := p.(*haipayPlugin)
	// Independently computed: strtoupper(md5(sorted "k=v&..." + accesskey)),
	// extend_params is a nested map rendered as "subject=VIP".
	params := map[string]any{
		"accessid": "AID", "agent_no": "A01", "merch_no": "M01", "pay_type": "ALI",
		"pay_mode": "NATIVE", "out_trade_no": "T1", "total_amount": "1.00", "pn": "PN1",
		"notify_url": "https://pay.example/n", "req_id": "R1",
		"extend_params": map[string]any{"subject": "VIP"},
	}
	if got := hp.haipaySign(params); got != "482808A30D8837F0A25F91CB7980B53B" {
		t.Fatalf("haipay sign mismatch: %s", got)
	}
}

func TestPluginHaipayCreate(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	// alipay native -> ali_qr_code
	p := haipayWithHost(t, "alipay", srv.URL, "1")
	reply = `{"result_code":"10000","ali_qr_code":"https://qr.hkrt/a","trade_no":"TR1"}`
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.hkrt/a" || res.ProviderTradeNo != "TR1" {
		t.Fatal(res, e)
	}
	if gotPath != "/api/v2/pay/pre-pay" || gotBody["pay_type"] != "ALI" || gotBody["total_amount"] != "1.00" || gotBody["sign"] == nil {
		t.Fatal("request", gotPath, gotBody)
	}
	// wxpay jsapi -> wc_pay_data, with appid:openid split
	pw := haipayWithHost(t, "wxpay", srv.URL)
	reply = `{"result_code":"10000","wc_pay_data":"{\"package\":\"x\"}","trade_no":"TR2"}`
	res, e = pw.Create(context.Background(), CreateRequest{TradeNo: "T2", Subject: "VIP", Method: "jsapi", NotifyURL: "https://pay.example/n", OpenID: "wxAppid:openid123", Amount: 100})
	if e != nil || res.MaterialType != "jsapi" || !strings.Contains(res.Material, "package") {
		t.Fatal(res, e)
	}
	if gotBody["appid"] != "wxAppid" || gotBody["openid"] != "openid123" {
		t.Fatal("wx split", gotBody)
	}
	// bank native -> uniqr_qr_code
	pb := haipayWithHost(t, "bank", srv.URL)
	reply = `{"result_code":"10000","uniqr_qr_code":"https://qr.hkrt/u","trade_no":"TR3"}`
	res, e = pb.Create(context.Background(), CreateRequest{TradeNo: "T3", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.hkrt/u" {
		t.Fatal(res, e)
	}
}

func TestPluginHaipayCreateOutcomes(t *testing.T) {
	var status int
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := haipayWithHost(t, "alipay", srv.URL, "1")
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100}
	status, reply = 200, `{"result_code":"20001","result_msg":"fail"}`
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

func TestPluginHaipayNotification(t *testing.T) {
	p, _ := BuildPluginChannel(haipayChannel("alipay"))
	v := p.(NotificationVerifier)
	// Signed callback; HP_CB_SIGN computed independently over the sorted base.
	body := `{"openid":"o1","order_amount":"1.00","out_trade_no":"T1","trade_no":"TR1","trade_status":"1","sign":"28A14E002D7091B3F0A8CC80DC585343"}`
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := v.VerifyNotification(context.Background(), nil, []byte(body), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "TR1" || ev.BuyerID != "o1" || ev.Amount != 100 {
		t.Fatal(ev, e)
	}
	// bad sign
	badSign := strings.Replace(body, "28A14E002D7091B3F0A8CC80DC585343", "00000000000000000000000000000000", 1)
	if _, e := v.VerifyNotification(context.Background(), nil, []byte(badSign), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// wrong amount / trade
	if _, e := v.VerifyNotification(context.Background(), nil, []byte(body), ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e := v.VerifyNotification(context.Background(), nil, []byte(body), ExpectedPayment{TradeNo: "X", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// ack
	if s, ct, b := p.(NotificationAcker).NotificationAck(nil); s != 200 || !strings.Contains(b, "SUCCESS") || !strings.Contains(ct, "json") {
		t.Fatal(s, b)
	}
	if _, _, b := p.(NotificationAcker).NotificationAck(ErrInvalid); !strings.Contains(b, "FAIL") {
		t.Fatal(b)
	}
}

func TestPluginHaipayQueryAndRefund(t *testing.T) {
	var gotPath string
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		io.ReadAll(r.Body)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := haipayWithHost(t, "alipay", srv.URL, "1")
	reply = `{"result_code":"10000","trade_status":"1","out_trade_no":"T1","order_amount":"1.00","openid":"o1"}`
	q, e := p.Query(context.Background(), QueryRequest{TradeNo: "T1", ProviderTradeNo: "TR1"})
	if e != nil || q.State != "SUCCESS" || q.Amount != 100 || gotPath != "/api/v2/pay/order-query" {
		t.Fatal(q, e, gotPath)
	}
	reply = `{"result_code":"10000","refund_no":"RF1"}`
	rr, e := p.Refund(context.Background(), RefundRequest{ProviderTradeNo: "TR1", RefundNo: "R1", Amount: 50, TotalAmount: 100})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 50 || gotPath != "/api/v2/pay/refund" {
		t.Fatal(rr, e, gotPath)
	}
}

func haipayWithHost(t *testing.T, payType, host string, apptypes ...string) *haipayPlugin {
	t.Helper()
	p, e := BuildPluginChannel(haipayChannel(payType, apptypes...))
	if e != nil {
		t.Fatal(e)
	}
	hp := p.(*haipayPlugin)
	hp.host = host
	return hp
}
