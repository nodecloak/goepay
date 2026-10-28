package payment

import (
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func alipaycodeChannel(appswitch string) PluginChannel {
	return PluginChannel{ID: 13, Plugin: "alipaycode", PayType: "alipay", Config: map[string]string{
		"appid": "2021app", "appsecret": alipaydTestPriv, "appkey": alipaydTestPub, "appmchid": "2088123456789012", "appswitch": appswitch,
	}}
}

func TestPluginAlipaycodeRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("alipaycode")
	if !ok || spec.Title != "支付宝免签约码支付" || !spec.Serves("alipay") {
		t.Fatal("spec", ok)
	}
	if _, e := BuildPluginChannel(alipaycodeChannel("1")); e != nil {
		t.Fatal(e)
	}
	// Bad UID (not 2088 / wrong length), missing key, missing UID.
	badUID := alipaycodeChannel("1")
	badUID.Config["appmchid"] = "123"
	missingKey := alipaycodeChannel("1")
	delete(missingKey.Config, "appsecret")
	missingUID := alipaycodeChannel("1")
	delete(missingUID.Config, "appmchid")
	for i, c := range []PluginChannel{badUID, missingKey, missingUID} {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginAlipaycodeMethods(t *testing.T) {
	p1, _ := BuildPluginChannel(alipaycodeChannel("1"))
	if m := p1.Capabilities().Methods; len(m) != 1 || m[0] != "native" {
		t.Fatal("confirm methods", m)
	}
	p0, _ := BuildPluginChannel(alipaycodeChannel("0"))
	if m := p0.Capabilities().Methods; len(m) != 0 {
		t.Fatal("plain-transfer must expose no method", m)
	}
}

func TestPluginAlipaycodeCreateRenderURL(t *testing.T) {
	p, _ := BuildPluginChannel(alipaycodeChannel("1"))
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "C100", Subject: "会员", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1500})
	if e != nil || res.MaterialType != "qrcode" {
		t.Fatal(res, e)
	}
	if !strings.HasPrefix(res.Material, alipaycodeRenderBase+"?formData=") {
		t.Fatal("render base", res.Material)
	}
	u, _ := url.Parse(res.Material)
	var form map[string]any
	if json.Unmarshal([]byte(u.Query().Get("formData")), &form) != nil {
		t.Fatal("formData not JSON")
	}
	if form["remark"] != "C100" || form["transAmount"] != "15.00" || form["productCode"] != "TRANSFER_TO_ALIPAY_ACCOUNT" {
		t.Fatal("formData fields", form)
	}
	payee, _ := form["payeeInfo"].(map[string]any)
	if payee["identity"] != "2088123456789012" || payee["identityType"] != "ALIPAY_USER_ID" {
		t.Fatal("payee", form["payeeInfo"])
	}
}

func TestPluginAlipaycodePlainTransferUnsupported(t *testing.T) {
	p, _ := BuildPluginChannel(alipaycodeChannel("0"))
	// Plain-transfer offers no method, so Create is refused by validation.
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "C1", Subject: "x", Method: "native", NotifyURL: "https://pay.example/n", Amount: 1}); e == nil {
		t.Fatal("plain-transfer create must fail")
	}
	// No signed callback exists for this plugin.
	v := p.(NotificationVerifier)
	if _, e := v.VerifyNotification(context.Background(), nil, []byte("a=1"), ExpectedPayment{TradeNo: "C1", Amount: 1}); !errors.Is(e, ErrUnsupported) {
		t.Fatal("notify must be unsupported", e)
	}
}

func alipaycodeWithGateway(t *testing.T, serverURL string) Provider {
	t.Helper()
	c := alipaycodeChannel("1")
	c.HTTPClient = &http.Client{Transport: alipaydRewrite{target: serverURL}}
	p, e := BuildPluginChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	return p
}

// Independent vector: Go's accountlog.query sign content equals node's, and the
// node-produced RSA2 signature verifies in Go.
func TestPluginAlipaycodeSignatureVector(t *testing.T) {
	const reqContent = `app_id=2021app&biz_content={"end_time":"2026-10-29 10:01:00","page_no":1,"page_size":2000,"start_time":"2026-10-29 09:57:00"}&charset=UTF-8&format=json&method=alipay.data.bill.accountlog.query&sign_type=RSA2&timestamp=2026-10-29 10:00:00&version=1.0`
	const reqSig = "St5tq6HZehtB0F3ffAfQNG4SImV9te8zIqPSCjybtHAWn3ktX/nrXuimK25jN3qQT5XwnPA2X1xZiBMkO2cSv0NoEJT3hczX38qRyeROVR0P6dTLcnDQzA2xVfmx/SrctWvZxx+KPmJMIvtx4Dpy/4GFnGxuMOwuS0vtIiwxwagKsEfQMfZ118uzwbVJ+SlVzB3/OxeyC4A2IGM4kmlR/Xtlpn4OJCHtNrn0oY7F2lPbWFtt3mgEDS3UGR+/gR6nP5CaoxqBg8m1+9Aoow2XP0XuDdAIiuDwhcTGq63+r1Vu681f//7GCcKtHLV1xRn0R4t53I7LaMUo/qhSGr1YTw=="

	biz := map[string]any{
		"start_time": "2026-10-29 09:57:00", "end_time": "2026-10-29 10:01:00",
		"page_no": 1, "page_size": 2000,
	}
	bizJSON, _ := json.Marshal(biz)
	params := map[string]string{
		"app_id": "2021app", "method": "alipay.data.bill.accountlog.query", "format": "json",
		"charset": "UTF-8", "sign_type": "RSA2", "version": "1.0",
		"timestamp": "2026-10-29 10:00:00", "biz_content": string(bizJSON),
	}
	if got := alipaycodeSignContent(params); got != reqContent {
		t.Fatalf("sign content mismatch\n got: %s\nwant: %s", got, reqContent)
	}
	pub, _ := pluginPublicKey(alipaydTestPub)
	if !rsaVerify(pub, crypto.SHA256, reqContent, reqSig) {
		t.Fatal("node signature must verify in Go")
	}
}

func alipaycodeAccountlogReply(t *testing.T, detailList string) string {
	t.Helper()
	node := `{"code":"10000","msg":"Success","page_no":"1","page_size":"2000","detail_list":[` + detailList + `]}`
	sig, e := alipaydSignNodeForTest(node)
	if e != nil {
		t.Fatal(e)
	}
	return `{"alipay_data_bill_accountlog_query_response":` + node + `,"sign":"` + sig + `"}`
}

func TestPluginAlipaycodeQuery(t *testing.T) {
	var reply string
	var lastMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f, _ := url.ParseQuery(string(b))
		lastMethod = f.Get("method")
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := alipaycodeWithGateway(t, srv.URL)

	// Match: incoming transfer, memo == trade_no; the account-log amount is
	// returned for the caller to validate against the order.
	reply = alipaycodeAccountlogReply(t, `{"trans_memo":"请勿添加备注-C100","trans_amount":"15.00","alipay_order_no":"2026999","other_account":"other@x.com"}`)
	q, e := p.Query(context.Background(), QueryRequest{TradeNo: "C100"})
	if e != nil || q.State != "SUCCESS" || q.Amount != 1500 || q.ProviderTradeNo != "2026999" || q.BuyerID != "other@x.com" {
		t.Fatal("match", q, e)
	}
	if lastMethod != "alipay.data.bill.accountlog.query" {
		t.Fatal("method", lastMethod)
	}

	// No matching memo in the window -> NOTPAY.
	reply = alipaycodeAccountlogReply(t, `{"trans_memo":"SOMETHINGELSE","trans_amount":"15.00","alipay_order_no":"X"}`)
	if q, e = p.Query(context.Background(), QueryRequest{TradeNo: "C100"}); e != nil || q.State != "NOTPAY" {
		t.Fatal("no memo match", q, e)
	}

	// Memo matches, different amount: SUCCESS but the RETURNED amount reflects
	// the transfer (1000), which the payment core rejects against a 1500 order.
	reply = alipaycodeAccountlogReply(t, `{"trans_memo":"C100","trans_amount":"10.00","alipay_order_no":"X"}`)
	if q, e = p.Query(context.Background(), QueryRequest{TradeNo: "C100"}); e != nil || q.State != "SUCCESS" || q.Amount != 1000 {
		t.Fatal("amount returned for caller validation", q, e)
	}

	// Empty list -> NOTPAY.
	reply = alipaycodeAccountlogReply(t, ``)
	if q, e = p.Query(context.Background(), QueryRequest{TradeNo: "C100"}); e != nil || q.State != "NOTPAY" {
		t.Fatal("empty", q, e)
	}

	// Signed business error -> definitive rejection.
	errNode := `{"code":"40004","sub_code":"isv.invalid-app-id"}`
	esig, _ := alipaydSignNodeForTest(errNode)
	reply = `{"error_response":` + errNode + `,"sign":"` + esig + `"}`
	var pe *ProviderError
	if _, e = p.Query(context.Background(), QueryRequest{TradeNo: "C100"}); !errors.As(e, &pe) {
		t.Fatal("business error must be definitive", e)
	}
}

func TestPluginAlipaycodeQueryUnknownOutcomes(t *testing.T) {
	var status int
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p := alipaycodeWithGateway(t, srv.URL)
	var unknown *UnknownOutcome
	for _, c := range []struct {
		status int
		reply  string
	}{
		{200, `not json`},
		{200, `{"alipay_data_bill_accountlog_query_response":{"code":"10000","detail_list":[]},"sign":"AAAA"}`}, // bad signature
		{502, `bad gateway`},
	} {
		status, reply = c.status, c.reply
		if _, e := p.Query(context.Background(), QueryRequest{TradeNo: "C100"}); !errors.As(e, &unknown) {
			t.Fatal("must be unknown", c.status, e)
		}
	}
	// Query without 支付宝公钥 is unsupported (verification is mandatory).
	c := alipaycodeChannel("1")
	delete(c.Config, "appkey")
	np, _ := BuildPluginChannel(c)
	if _, e := np.Query(context.Background(), QueryRequest{TradeNo: "C100"}); !errors.Is(e, ErrUnsupported) {
		t.Fatal("query without public key must be unsupported", e)
	}
}
