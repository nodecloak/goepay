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
)

// Shared 2048-bit RSA test keypair (PKCS8 private / PKIX public, raw base64 as
// the 富友 console stores them). Generated offline; used across the fuiou/hlpay
// tests as both merchant and platform key so a self-signed vector verifies.
const fuiou2TestPriv = "MIIEvAIBADANBgkqhkiG9w0BAQEFAASCBKYwggSiAgEAAoIBAQCeubqhrVVHPShWMo+BS7uLUn5d4N69vg9AobPVFcgiW3loHVmnS+Al+u9mqw+Z2i+3fnugCs44ksE3580WRa3e5vqgZD/ga1dNiP9AkZL2UWublqsAF9XRDIKnp5sXJqb/rMzGCc/ANhCof3g7RCHHpFuPYIcfCbnusUEbVSmzxqCmQ1Xuc+Z9c6M+hxqh0iZeW11hfZrTHevA3Kb7yvTfaHGvY2F6LiBUEkKrX2v/lSkT3GDaq6y5pSo1gTgSWV+RzWKUbFakMPB2QzDMYwWK1VF8zix8a2IavAqpfRfqrTFQXCu+aKzrQg9ZpmkcesseHE0M+0dkwCmtn3c5Rb+lAgMBAAECggEAKQ7BDJ5SH13eB8IHZfoplqXtoyoOZAV8W4sYggei3+RKmNhyvvg9EIYDFEsue205h50TrHfvFSo9CdGPtvGqQqz76q5T6aDKkfdck560Zbv0+me0bw87U1qJl4We9BOfAib4R0f1cfJqR7PFbYs3s7U7q/Bbs3coHerC/Sd/5I0VbtqFrAqFNf3bcdTeh3JIk/cNU/W/q1YGeBEGILWU11bjtCvRRF8KaYr+C1E+i0ThyyKBczcnJTOARWT114FziGzuKUG2lOCdu0wvXqn6vQkxaOfCvMs0iQn871gAhiHsZlcK7E1HuTKihDuvSTE1gFiHNftKo9RX/HusTyC2JwKBgQDd+TuihSvvevCn89rReKGnf7Ym7BPF+0g7uRlWxSY0iW3CY5GHA3kF2FLzLbJfAquSzBZW5BNUcj+xSR9AG19sDw3l3pNHr/cfeAfCfH18QSPxedZ65R0qieSVwg6mGdiusEXSI4/WVb2q94llayZ3RlkQtgXmQJLtSejXIyiZawKBgQC3Dn4jQYhGvEaZ5RN+7F2ItErneuqkIiyRFFO8jVulgpWLh0t2eh2ZJFQ2eK5EucB1XhDTTzDzRTg4+sz4igKzoLOQR0e2rA4a1p+Z2cGPQKX9XNqRuiEc5riD20iaexJ97MlHKzt6SXaIoLBcwLeouY13uWrHzcLplAEH/AD/LwKBgC5CdLUn/8IG4wR9g4rIExhvOyzdYjmOZr1bk/ZLW+zy/xbV3Uw/4wbe0FqxmUzHp64OLlGpcoXU0YVG8eZQThCdCo5vVk5IU74RK7zSaOxuPuliNNqjyPJGjobPTHI9TLULM2HZ224VrFNnBxFFxVxyIrmi1ZPovTYKgyXiw+FFAoGADwqQ0MLMuMs1neWVwYMYeGggE38lEpGASfd6ne6bjjD5qrpWOAj2r6Oti8BEyX7d0ocFKOfD5OlWIlBxUSa97BaTSpKg0OC4U/o4qQKchfHQ8c2UZA8AM4i9to/pGlBsh45SR5fEzlqE89sIQsbPEH0Wo7THkl1U265+mzJYvp8CgYBmaognuEL62krt+T+Mi1qexIynOmmeBxK3Rn0uEqN8u8DGaRekaEPQ8Xn+aJaV9L3bgkyqTQ3M/YDL87/jz+RTtZr1De+nC6eak6/nS3cqC9LQZwZs9Q/dI/mLPKQcca6XXxm5scs0iZjoAaigSnl4VePBmrcaYMRNrei7wD9lEw=="
const fuiou2TestPub = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAnrm6oa1VRz0oVjKPgUu7i1J+XeDevb4PQKGz1RXIIlt5aB1Zp0vgJfrvZqsPmdovt357oArOOJLBN+fNFkWt3ub6oGQ/4GtXTYj/QJGS9lFrm5arABfV0QyCp6ebFyam/6zMxgnPwDYQqH94O0Qhx6Rbj2CHHwm57rFBG1Ups8agpkNV7nPmfXOjPocaodImXltdYX2a0x3rwNym+8r032hxr2Nhei4gVBJCq19r/5UpE9xg2qusuaUqNYE4Ellfkc1ilGxWpDDwdkMwzGMFitVRfM4sfGtiGrwKqX0X6q0xUFwrvmis60IPWaZpHHrLHhxNDPtHZMAprZ93OUW/pQIDAQAB"

func fuiou2Channel(payType string, apptypes ...string) PluginChannel {
	return PluginChannel{
		ID: 11, Plugin: "fuiou2", PayType: payType, AppTypes: apptypes,
		Config: map[string]string{"appid": "INS01", "appmchid": "M01", "appsecret": fuiou2TestPriv, "appkey": fuiou2TestPub, "appurl": "PFX", "appswitch": "0"},
	}
}

func TestPluginFuiou2RegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("fuiou2")
	if !ok || spec.Title != "富友支付(合作方)" || !spec.Serves("alipay") || !spec.Serves("bank") || spec.Serves("paypal") {
		t.Fatal("fuiou2 spec", spec, ok)
	}
	if _, e := BuildPluginChannel(fuiou2Channel("alipay")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{
		fuiou2Channel("paypal"), // unserved pay type
	}
	missing := fuiou2Channel("alipay")
	delete(missing.Config, "appsecret")
	badkey := fuiou2Channel("alipay")
	badkey.Config["appkey"] = "not-a-key"
	bad = append(bad, missing, badkey)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginFuiou2SignatureMatchesOriginal(t *testing.T) {
	p, e := BuildPluginChannel(fuiou2Channel("alipay"))
	if e != nil {
		t.Fatal(e)
	}
	fp := p.(*fuiou2Plugin)
	// Independently computed with node c.sign('md5', ..., priv) over the exact
	// sorted sign-content (reserved* and empties excluded, sign excluded).
	req := map[string]string{
		"version": "1.0", "ins_cd": "INS01", "mchnt_cd": "M01", "term_id": "88888888",
		"random_str": "abc", "order_amt": "100", "order_type": "ALIPAY", "mchnt_order_no": "PFXT1",
		"txn_begin_ts": "20260101000000", "goods_des": "VIP", "term_ip": "1.2.3.4",
		"notify_url": "https://pay.example/n", "reserved_x": "ignored", "empty": "",
	}
	got, e := fp.fuiou2Sign(req)
	if e != nil {
		t.Fatal(e)
	}
	const want = "nRsCiMBzXQ3DnVBGDJlPVayxrNMmdkp5TrdHF21CQLXtqqfTe06DAZBkg8ZMNJWffzRRDa2MvG+ddZpb4/9k2ApcA+xkgz8KCbmjbJpt1cukh6+2Ai0i2zFK/P7OMlsY41cEiihjln50peaLanaDQPaqcagD0HjGd/UffC21sLA+E5ZbbLABlkNxxb5aolTTz7pa6Ee0iynhZBtfoZPEffanDJ30dCO4wqUt+0IgzJ0F+H5T5d+Zzue4NkRQaLgyR205ljiEiOgynS4kGmq0rTwOHp1vQs6jr6NQCgqoJglj3yhQJ48EZ3CC+1sWs4FEscHL1//0X2+k4tWFJKhyxg=="
	if got != want {
		t.Fatalf("fuiou2 request sign mismatch\n got=%s\nwant=%s", got, want)
	}
	if !fp.fuiou2Verify(map[string]string{"version": "1.0", "ins_cd": "INS01", "mchnt_cd": "M01", "term_id": "88888888", "random_str": "abc", "order_amt": "100", "order_type": "ALIPAY", "mchnt_order_no": "PFXT1", "txn_begin_ts": "20260101000000", "goods_des": "VIP", "term_ip": "1.2.3.4", "notify_url": "https://pay.example/n", "reserved_x": "ignored", "sign": want}) {
		t.Fatal("self-verify failed")
	}
}

func TestPluginFuiou2CreateNative(t *testing.T) {
	var gotBody string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotPath = string(b), r.URL.Path
		// Build a signed GBK-less (ascii) XML reply.
		reply := fuiou2TestReply(t, map[string]string{"result_code": "000000", "qr_code": "https://qr.fuiou/abc", "transaction_id": "TR1", "mchnt_order_no": "PFXT1"})
		io.WriteString(w, url.QueryEscape(reply))
	}))
	defer srv.Close()
	p := fuiou2WithHost(t, "alipay", srv.URL)
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ClientIP: "1.2.3.4", Amount: 100}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.fuiou/abc" || res.ProviderTradeNo != "TR1" {
		t.Fatal(res, e)
	}
	if gotPath != "/preCreate" || !strings.HasPrefix(gotBody, "req=") {
		t.Fatal("request", gotPath, gotBody)
	}
	// The posted body is double-urlencoded XML carrying mchnt_order_no with the prefix.
	xmlStr := fuiou2DecodeReq(t, gotBody)
	if !strings.Contains(xmlStr, "<mchnt_order_no>PFXT1</mchnt_order_no>") || !strings.Contains(xmlStr, "<order_type>ALIPAY</order_type>") || !strings.Contains(xmlStr, "<sign>") {
		t.Fatal("xml", xmlStr)
	}
}

func TestPluginFuiou2CreateOutcomes(t *testing.T) {
	var status int
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := fuiou2WithHost(t, "alipay", srv.URL)
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", Amount: 100}
	// Business rejection (signed, non-success code).
	status, reply = 200, url.QueryEscape(fuiou2TestReply(t, map[string]string{"result_code": "030014", "result_msg": "fail"}))
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business refusal must be definitive", e)
	}
	// Unknown outcome: unreadable body and 5xx.
	for _, c := range []struct {
		s int
		b string
	}{{200, "not%20xml"}, {502, "bad"}} {
		status, reply = c.s, c.b
		var unknown *UnknownOutcome
		if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
			t.Fatal("ambiguous reply must be unknown", c, e)
		}
	}
}

func TestPluginFuiou2Notification(t *testing.T) {
	p, _ := BuildPluginChannel(fuiou2Channel("alipay"))
	v := p.(NotificationVerifier)
	// Signed callback over GBK-less ascii values.
	fields := map[string]string{"result_code": "000000", "mchnt_order_no": "PFXT1", "order_amt": "100", "user_id": "2088xx"}
	xmlStr := fuiou2TestReply(t, fields)
	raw := []byte("req=" + url.QueryEscape(xmlStr))
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := v.VerifyNotification(context.Background(), nil, raw, exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "PFXT1" || ev.BuyerID != "2088xx" || ev.Amount != 100 {
		t.Fatal(ev, e)
	}
	// bad signature
	bad := strings.Replace(xmlStr, "<sign>", "<sign>0", 1)
	if _, e := v.VerifyNotification(context.Background(), nil, []byte("req="+url.QueryEscape(bad)), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// wrong amount / wrong trade
	if _, e := v.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e := v.VerifyNotification(context.Background(), nil, raw, ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// non-success status
	fail := fuiou2TestReply(t, map[string]string{"result_code": "030099", "mchnt_order_no": "PFXT1", "order_amt": "100"})
	if _, e := v.VerifyNotification(context.Background(), nil, []byte("req="+url.QueryEscape(fail)), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success", e)
	}
	// ack bodies
	if s, _, b := p.(NotificationAcker).NotificationAck(nil); s != 200 || b != "1" {
		t.Fatal(s, b)
	}
	if _, _, b := p.(NotificationAcker).NotificationAck(ErrInvalid); b != "0" {
		t.Fatal(b)
	}
	if m := p.(NotificationMethodsProvider).NotificationMethods(); len(m) != 1 || m[0] != http.MethodPost {
		// fuiou2 callback is POST only (req body)
		t.Fatal(m)
	}
}

func TestPluginFuiou2Refund(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		reply := fuiou2TestReply(t, map[string]string{"result_code": "000000", "mchnt_order_no": "TR1"})
		io.WriteString(w, url.QueryEscape(reply))
	}))
	defer srv.Close()
	p := fuiou2WithHost(t, "alipay", srv.URL)
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "TR1", RefundNo: "R1", Amount: 50, TotalAmount: 100})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 50 || gotPath != "/commonRefund" {
		t.Fatal(rr, e, gotPath)
	}
}

// ---- helpers ----

func fuiou2WithHost(t *testing.T, payType, host string) *fuiou2Plugin {
	t.Helper()
	p, e := BuildPluginChannel(fuiou2Channel(payType))
	if e != nil {
		t.Fatal(e)
	}
	fp := p.(*fuiou2Plugin)
	fp.host = host // redirect to the httptest server
	return fp
}

// fuiou2TestReply signs fields with the test private key (acting as the 富友
// platform key, which equals our public key here) and renders the flat XML.
func fuiou2TestReply(t *testing.T, fields map[string]string) string {
	t.Helper()
	p, _ := BuildPluginChannel(fuiou2Channel("alipay"))
	fp := p.(*fuiou2Plugin)
	sign, e := fp.fuiou2Sign(fields)
	if e != nil {
		t.Fatal(e)
	}
	all := map[string]string{}
	for k, v := range fields {
		all[k] = v
	}
	all["sign"] = sign
	var sb strings.Builder
	sb.WriteString("<xml>")
	for k, v := range all {
		sb.WriteString("<" + k + ">" + v + "</" + k + ">")
	}
	sb.WriteString("</xml>")
	return sb.String()
}

func fuiou2DecodeReq(t *testing.T, body string) string {
	t.Helper()
	v, e := url.ParseQuery(body)
	if e != nil {
		t.Fatal(e)
	}
	once := v.Get("req")
	dec, e := url.QueryUnescape(once)
	if e != nil {
		t.Fatal(e)
	}
	return dec
}
