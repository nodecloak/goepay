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

func chinaumsChannel(payType string, appTypes []string) PluginChannel {
	return PluginChannel{ID: 13, Plugin: "chinaums", PayType: payType, AppTypes: appTypes, Config: map[string]string{
		"appid": "app1", "appkey": "appkeyX", "appmchid": "M1", "appurl": "T1", "appsecret": "commkey", "msgsrcid": "src1", "appswitch": "0",
	}}
}

func TestPluginChinaumsRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("chinaums")
	if !ok || spec.Title != "银联商务" || !spec.Serves("wxpay") || spec.Serves("qqpay") {
		t.Fatal("chinaums spec", ok)
	}
	if _, e := BuildPluginChannel(chinaumsChannel("wxpay", nil)); e != nil {
		t.Fatal(e)
	}
	// msgsrcid must be 4 chars
	badsrc := chinaumsChannel("wxpay", nil)
	badsrc.Config["msgsrcid"] = "abc"
	if _, e := BuildPluginChannel(badsrc); e == nil {
		t.Fatal("accepted 3-char msgsrcid")
	}
	missing := chinaumsChannel("wxpay", nil)
	delete(missing.Config, "appsecret")
	if _, e := BuildPluginChannel(missing); e == nil {
		t.Fatal("accepted missing secret")
	}
}

func TestPluginChinaumsSignatureVector(t *testing.T) {
	p, _ := BuildPluginChannel(chinaumsChannel("wxpay", nil))
	// base64(HMAC-SHA256(appId+ts+nonce+sha256hex('{"a":1}'), appKey)), node vector.
	if got := p.(*chinaumsPlugin).signature("20260101120000", "nc", `{"a":1}`); got != "TurLRcGNeBIgv4XZ/d0inEXO6V+OEDYJpP/TDOKIsVM=" {
		t.Fatal(got)
	}
}

func TestPluginChinaumsCreateQRAndH5(t *testing.T) {
	var reply string
	var authHdr, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHdr = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, e := BuildPluginChannel(chinaumsChannel("wxpay", []string{"scan"}))
	if e != nil {
		t.Fatal(e)
	}
	cp := p.(*chinaumsPlugin)
	cp.gateway = srv.URL // redirect to test server

	r := CreateRequest{TradeNo: "T1", Subject: "商品", Method: "native", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 100, ClientIP: "1.1.1.1"}
	reply = `{"errCode":"SUCCESS","billQRCode":"https://qr.chinaums/x","billNo":"src1T1"}`
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.chinaums/x" || res.ProviderTradeNo != "src1T1" {
		t.Fatal(res, e)
	}
	if !strings.HasPrefix(authHdr, "OPEN-BODY-SIG AppId=\"app1\"") || !strings.Contains(gotBody, `"billNo":"src1T1"`) {
		t.Fatal("auth/body", authHdr, gotBody)
	}
	// business failure
	reply = `{"errCode":"FAIL","errInfo":"PARAM"}`
	var pe *ProviderError
	if _, e = p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business failure", e)
	}

	// H5: pure offline URL build (no network), assert signed GET URL.
	ph, _ := BuildPluginChannel(chinaumsChannel("alipay", []string{"h5"}))
	rh := CreateRequest{TradeNo: "T2", Subject: "s", Method: "h5", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 50}
	resh, e := ph.Create(context.Background(), rh)
	if e != nil || resh.MaterialType != "jump" {
		t.Fatal(resh, e)
	}
	u, _ := url.Parse(resh.Material)
	if u.Path != "/v1/netpay/trade/h5-pay" || u.Query().Get("authorization") != "OPEN-FORM-PARAM" || u.Query().Get("signature") == "" || u.Query().Get("content") == "" {
		t.Fatal("h5 url", resh.Material)
	}
}

func TestPluginChinaumsNotificationChecks(t *testing.T) {
	p, _ := BuildPluginChannel(chinaumsChannel("wxpay", nil))
	cp := p.(*chinaumsPlugin)
	encode := func(m map[string]string) []byte {
		q := url.Values{}
		for k, v := range m {
			q.Set(k, v)
		}
		return []byte(q.Encode())
	}
	// H5DEFAULT branch
	base := map[string]string{"instMid": "H5DEFAULT", "merOrderId": "src1T1", "status": "TRADE_SUCCESS", "totalAmount": "100", "buyerId": "b1", "targetOrderId": "u1", "signType": "MD5"}
	base["sign"] = strings.ToUpper(md5Hex(chinaumsSignBase(base) + "commkey"))
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := cp.VerifyNotification(context.Background(), nil, encode(base), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "u1" || ev.Amount != 100 || ev.BuyerID != "b1" {
		t.Fatal(ev, e)
	}
	// verify the independent vector matches our verify path
	if base["sign"] != "BEEEC65BE44588F63978DD44C8070E49" {
		t.Fatal("callback sign vector drift", base["sign"])
	}
	// wrong amount
	if _, e = cp.VerifyNotification(context.Background(), nil, encode(base), ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	// wrong trade
	if _, e = cp.VerifyNotification(context.Background(), nil, encode(base), ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// bad sign
	tampered := map[string]string{}
	for k, v := range base {
		tampered[k] = v
	}
	tampered["sign"] = strings.Repeat("A", 32)
	if _, e = cp.VerifyNotification(context.Background(), nil, encode(tampered), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// non-success status
	ns := map[string]string{"instMid": "H5DEFAULT", "merOrderId": "src1T1", "status": "FAILED", "totalAmount": "100", "signType": "MD5"}
	ns["sign"] = strings.ToUpper(md5Hex(chinaumsSignBase(ns) + "commkey"))
	if _, e = cp.VerifyNotification(context.Background(), nil, encode(ns), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("status", e)
	}
	// QRPAYDEFAULT branch
	qr := map[string]string{"instMid": "QRPAYDEFAULT", "billNo": "src1T1", "billStatus": "PAID", "totalAmount": "100", "billPayment": `{"buyerId":"qb","targetOrderId":"qt"}`, "signType": "SHA256"}
	qr["sign"] = strings.ToUpper(sha256Hex(chinaumsSignBase(qr) + "commkey"))
	ev2, e := cp.VerifyNotification(context.Background(), nil, encode(qr), exp)
	if e != nil || ev2.ProviderTradeNo != "qt" || ev2.BuyerID != "qb" {
		t.Fatal(ev2, e)
	}
	if st, _, bdy := cp.NotificationAck(nil); st != 200 || bdy != "SUCCESS" {
		t.Fatal(st, bdy)
	}
	if _, _, bdy := cp.NotificationAck(ErrInvalid); bdy != "FAILED" {
		t.Fatal(bdy)
	}
}

func TestPluginChinaumsRefund(t *testing.T) {
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"instMid":"QRPAYDEFAULT"`) {
			t.Errorf("refund body %s", b)
		}
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(chinaumsChannel("wxpay", nil))
	cp := p.(*chinaumsPlugin)
	cp.gateway = srv.URL
	reply = `{"errCode":"SUCCESS","billNo":"src1R1","refundAmount":50}`
	rr, e := p.Refund(context.Background(), RefundRequest{ProviderTradeNo: "src1T1", RefundNo: "R1", Amount: 50, TotalAmount: 100})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 50 {
		t.Fatal(rr, e)
	}
}
