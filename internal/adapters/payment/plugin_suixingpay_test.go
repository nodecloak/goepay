package payment

import (
	"context"
	"crypto"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const suixingpayMerchPriv = `-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDlB99GbSmrV53y
j18t5g0RiVa/XUztM/5EHKDYcUcUftIK2i3BPW/9P7kjBXOXntfYBU/dBqgph+Uk
GjjuIej6ffVyXRZRXMRFsqLzO2lQek8CxhGFST+NzyEVuNKiteuTw1wxOqjXuApp
YU7LrYFDyEgLgsHRR0Nv2DZvih6Muv3uD5YmVcBTWM0wDBhoRqkBNroREoVYgyNZ
URb4cE0moAE5MkuAgHsd8MwJuIPEKoaXz74V01pMEReodebHtL3xVoCFMX+Yk0Pd
9+Cf2wMPt27WbHcgFsyCqtu7aNY7KP3cxZTKB0LuGmMTIG0IrR+/IGwAcuJUUZYC
rLH+NhENAgMBAAECggEAHdX4Mqr+3ijUVShzPD33Xtz21YgYgOAglAvssjWTjmp0
/1/rzPwtbh7ShersY4iYUrZicIcw+fQPmDPoTbFSMeudwW+5/BBrKy85xhjrv8Wn
LFPULXpc0qL0iulgWzz6uu2cwXcETtHVXWQFSUhJiiR5qjI3AIXZlaTjdoVWNHva
iENPdqVEwNh7pOMYZWZXmzZyrC1kwB4GFOWwlX8KFXylaipPU03aWCZnjt3g91SL
guwVcLKG33aEBRtHDEj1Q+0K8yeDqticHnVQVrYdgPuD4ffEPuh0Cv8TaSrB/UMK
FRutezxMV2y0HAgGdqsJc+WHXBkMbabNsry93zuy8QKBgQD0ilfQT5ioOGlIn4Qt
une4AVuEhniUqjwoFUuHDpjz4aGreH4KyFHxJOudwEXW2ZKs7LYpR8Q5Kc8UjhJL
FsJ71W2Su5dyVjigq16X2I3GShzAc5y4IgTxuGsLlW4JKdprJuLquAOP6IUqiryw
sxLQDSzw4K7R8nRh6whx+1oiKQKBgQDvw3dApSp4AozpSg7jJxtjjo4TQ68Y1LH+
SBdy8SdoPxRY4HtbGmiSN/ePq9TxRtfA+fVkoACozYmeH6wu31k2IiM5X+tgn2fB
014GOarQs6R14Que0/MH+r89F8bNPzaxxVmdEwaLrKFGH8RCOmvJEBc3P5kb81xv
lQMe1Ud8RQKBgGGwppYERkLljkHB3+b4t/zdJfdSVPdm7nNA1pRUo4vCFnjOwOOD
1SBVlH7Hod7Lukm0oqzpC52ecyiN/QbcR8N6prRYlo7JnQOqkjZs/244izXiubfh
d0ItXy4YewXUH3pw5XPJITsrM4+kLu8UCq7spIhx6cTba9z2LMCEHiTJAoGAEqh3
Y0R3JZetN4pX1j142CqDy+P8gEYtA8jAamVx157xCmmNr+1CLf7E+R06GPyWVaIe
ApKg+oHkL7XvREaFPr0xWImqW36MKH02V+2s6UjRh4rXyKhdxUUyFKMBCX/d1b+x
1ZNhcwUgF8DpGO5K7P6fj+EXHSG3E8fY0lFumrECgYEAvUbrH/5fy3ziuQkNHjFb
6rE9dwCqkfOCYmCFizHakCIIP4GqOUFsQFzxwRt5RURfZNk8b3frBDgYAb8PU5LQ
DdOzi/k4gWbwJkE09WDYQ3cPBbrwuZ0ruTJc3G78i+TtWFNRkq+fIxuN6ndkh+vj
VqqBRQANSwLKZLl2gCPqcl0=
-----END PRIVATE KEY-----`

const suixingpayPlatPub = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAy7K5O9rVHT2B80LxJA5z
pPUaS158o5rnBm5VD8HpBjBHtLVR/8QmnuZv+YRux+r309wlOnm7fwsNT38vUi93
IQ21wS0K9JVrkN7zxmHmZVRH30tQNKs0p7FHsZn6SgPScL2hyDeVxXoYofhDweCC
FgQXNICj0iwQS8Bc4mBF2Dn8Nhqx3ZroHvF6q5nVufohhwnA32M3Jx5kv0hKNOn2
/cW7zGp3DM8eW04wsmatPOCUGGpV4ZLqy6bfEZ1EJVArQmmNY3kpXetWErMat5f6
qOMl931Qwh9qZVQnR3+MtRTZOQpLgC88EK5L0Kh8pjWAF0e9/OWV5OQyyZnAfgRg
awIDAQAB
-----END PUBLIC KEY-----`

const suixingpayPlatPriv = `-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDLsrk72tUdPYHz
QvEkDnOk9RpLXnyjmucGblUPwekGMEe0tVH/xCae5m/5hG7H6vfT3CU6ebt/Cw1P
fy9SL3chDbXBLQr0lWuQ3vPGYeZlVEffS1A0qzSnsUexmfpKA9JwvaHIN5XFehih
+EPB4IIWBBc0gKPSLBBLwFziYEXYOfw2GrHdmuge8XqrmdW5+iGHCcDfYzcnHmS/
SEo06fb9xbvMancMzx5bTjCyZq084JQYalXhkurLpt8RnUQlUCtCaY1jeSld61YS
sxq3l/qo4yX3fVDCH2plVCdHf4y1FNk5CkuALzwQrkvQqHymNYAXR7385ZXk5DLJ
mcB+BGBrAgMBAAECggEAG2/D72yho+ydokxz+oKlJPo2mSGYRtkdy7SfaciZQHzO
9meDLKDDqvIoRu2FlmaZtu7svMbvcESRfbxd0q4TV7URtCatq8AB2w43Zj3YMJx8
3yC1JEL4jAB0QwHAaqkVIUZe537v13It+zMGErPQvHJp8GfF/mXHqXcMNm735BBo
dK1A+woY+YhcxURYlSEuEVrTxjB+ncsCo9DI/VC+YrEceuLKsUx9DbDVM59wsMly
4OjshYmYiKHcf/upkDGcpAHh9MMQMcKbNJlkYj4tLe154zzgecgsJQa7FCKAugjM
aT8Kx5TzdWwLdBFFP/ev/vb0xICknGO/i3OhztoS0QKBgQDu4ldn/e4vHufrWtWj
/LLpzRvcCqiAkatTd5ygJaMPTOcnzy8ls2V5nUDi1Ay7q87yihTcejDU0oqbiAlL
8ePisP0w7LZ9gLr7aiuNB1kglRMetH2/zrYsEQPzjiFs/NgPI71wpwSB6jJX7yla
cQGoE1myZkNf1AGaZbzkNOkwzwKBgQDaSv5iA2RMKTzc+JwyZID9xGHdTMj1UFxq
OwYG4PXxgnGsy23tQU7HTmH65MVahhVLfY7L3mshzH7kTZ5xPDSFCUJFuldq+ZYj
bNQWT/JOce4n6k+Y9I1mZKACYNSijkv0HSCAj29tzy/PTEG12BociPo2y8e+oUKb
+IGWXDYlpQKBgFEGeqkuKxzWecmNjMtLig/dXya6gr7hKeBswLZdLbqh1gHdt1st
Ry0B1qFZVVxOgSopBbxmwateIl9wu96DlE0uxSJUF+3nvQRL1avHI0b26X5PsBtf
7NBzkQiKW/IQrOQttjLAo7nBCwRifX9Mt0ggEBt/rjiRu114RqKNA89nAoGAED9w
u77g4kJXg2MFzUJpz3mkhyJF+uxmVIncIvrvk3BQIbpqjjRENtWAuXn5rok4YIwR
6t69PqDH+RpF5svCZPyRcDs0coPtRum8x4Rf1rLoNRRdpFbfh3WJ62NlsEvDH4Jj
fu4uLxAy0M0uPui82M7hQN7eGTRLjuxqSNTIOvkCgYEAnGOSLTydIeS3B/tXnZXP
IWDqwVJMNSOp0ANnk72w193dySJviJRQXi6VJdMhRrXg1TbenMJtwCSyz5gb6x+n
mfP5OP+8mSQIBm+Jv3Om9xZOz0q7eyM2vVTqfk3WsuTjcffNmJg+ihWpZWwohSRh
UVSvx6Z84QWfLh4hI4YIGak=
-----END PRIVATE KEY-----`

// suixingpaySigVec is node's RSA-SHA1 signature, made with the PLATFORM private
// key, of the envelope sign content "orgId=ORG1&reqData={"amt":"10.00","mno":
// "M1","ordNo":"T1"}&reqId=R1&signType=RSA&timestamp=20260101000000&version=1.0".
// The plugin verifies replies with the platform public key (appkey), so this is
// the counterpart that proves our sign base + compact reqData match the SDK.
const suixingpaySigVec = "T+3O0A5owEwgYwyCBEvzfnwhA93C23fVFxO+dcG2oCzHMiD3/ygPFEV047/XeKrZ3/BVPzvIibsxBIE1WBu73w+VGt23zMFTcmaL4nWgTh6Dc6ztNmWhv9v2+qb04+hOdRthosi/na4x/Dllz7TDgpCPhSd7GjGhVIxHJeCs69n5DmnDCmPa06MxCndaHK0tX57rP/6NuCX1m8Zi5pmmbAYJS76GE3wj+RkKYaOgu8qvWKNlNIfizLBABxmC7ImAUwOAlfvUKCXMIyBdmI7yi6ngbPFcwVA7C2KIRzDm86CcULFBbIuFbPTktKgOnA2icaequ493BNT7Hb3YDRY8Iw=="

func suixingpayChannel(payType string) PluginChannel {
	return PluginChannel{ID: 30, Plugin: "suixingpay", PayType: payType,
		Config:       map[string]string{"appid": "ORG1", "appmchid": "M1"},
		Certificates: map[string]string{"appkey": suixingpayPlatPub, "appsecret": suixingpayMerchPriv},
	}
}

func TestPluginSuixingpayRegistry(t *testing.T) {
	spec, ok := LookupPlugin("suixingpay")
	if !ok || spec.Title != "随行付" || !spec.Serves("bank") || spec.Serves("qqpay") {
		t.Fatal("spec", spec, ok)
	}
	if _, e := BuildPluginChannel(suixingpayChannel("alipay")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{suixingpayChannel("qqpay")}
	missing := suixingpayChannel("alipay")
	delete(missing.Config, "appmchid")
	badkey := suixingpayChannel("alipay")
	badkey.Certificates["appsecret"] = "nope"
	bad = append(bad, missing, badkey)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

// Independent vector: node's RSA-SHA1 signature over the envelope sign content
// must verify against suixingpaySignContent (proving the envelope sign base and
// the compact reqData serialization match the PHP SDK).
func TestPluginSuixingpaySignVector(t *testing.T) {
	p, e := BuildPluginChannel(suixingpayChannel("alipay"))
	if e != nil {
		t.Fatal(e)
	}
	sp := p.(*suixingpayPlugin)
	reqData := suixingpayCompactJSON(map[string]string{"mno": "M1", "ordNo": "T1", "amt": "10.00"})
	if reqData != `{"amt":"10.00","mno":"M1","ordNo":"T1"}` {
		t.Fatal("reqData", reqData)
	}
	env := map[string]string{"orgId": "ORG1", "reqId": "R1", "reqData": reqData, "timestamp": "20260101000000", "version": "1.0", "signType": "RSA"}
	content := suixingpaySignContent(env)
	if content != `orgId=ORG1&reqData={"amt":"10.00","mno":"M1","ordNo":"T1"}&reqId=R1&signType=RSA&timestamp=20260101000000&version=1.0` {
		t.Fatal("content", content)
	}
	if !rsaVerify(sp.publicKey, crypto.SHA1, content, suixingpaySigVec) {
		t.Fatal("node signature did not verify against envelope sign content")
	}
}

// suixingpaySignReply signs a JSON reply/notify body with the platform private
// key over suixingpayEnvelopeSignContent, then injects the sign field.
func suixingpaySignReply(t *testing.T, body string) string {
	t.Helper()
	plat, e := pluginPrivateKey(suixingpayPlatPriv)
	if e != nil {
		t.Fatal(e)
	}
	content := suixingpayEnvelopeSignContent([]byte(body), nil)
	sig, e := rsaSign(plat, crypto.SHA1, content)
	if e != nil {
		t.Fatal(e)
	}
	// Insert "sign":"..." before the closing brace.
	return body[:len(body)-1] + `,"sign":` + suixingpayQuote(sig) + "}"
}

func TestPluginSuixingpayCreateNative(t *testing.T) {
	var gotBody string
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, e := BuildPluginChannel(suixingpayChannel("wxpay"))
	if e != nil {
		t.Fatal(e)
	}
	sp := p.(*suixingpayPlugin)
	sp.hostOverride(srv.URL)

	reply = suixingpaySignReply(t, `{"code":"0000","msg":"ok","respData":{"bizCode":"0000","payUrl":"weixin://wxpay/bizpayurl?pr=zz","transactionId":"SX9"}}`)
	r := CreateRequest{TradeNo: "T100", Subject: "商品", Method: "native", NotifyURL: "https://pay.example/n", ClientIP: "203.0.113.1", Amount: 1000}
	res, err := p.Create(context.Background(), r)
	if err != nil || res.MaterialType != "qrcode" || res.Material != "weixin://wxpay/bizpayurl?pr=zz" || res.ProviderTradeNo != "SX9" {
		t.Fatal(res, err)
	}
	if !strings.Contains(gotBody, `"payType":"WECHAT"`) || !strings.Contains(gotBody, `"ordNo":"T100"`) || !strings.Contains(gotBody, `"sign":`) {
		t.Fatal("request body", gotBody)
	}
}

func TestPluginSuixingpayCreateOutcomes(t *testing.T) {
	var reply string
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(suixingpayChannel("alipay"))
	sp := p.(*suixingpayPlugin)
	sp.hostOverride(srv.URL)
	r := CreateRequest{TradeNo: "T2", Subject: "x", Method: "native", NotifyURL: "https://pay.example/n", Amount: 50}

	// Business refusal: signed reply with code != 0000.
	status, reply = 200, suixingpaySignReply(t, `{"code":"9999","msg":"系统异常","respData":{}}`)
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business refusal must be definitive", e)
	}
	// bizCode refusal.
	status, reply = 200, suixingpaySignReply(t, `{"code":"0000","msg":"ok","respData":{"bizCode":"3001","bizMsg":"no"}}`)
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("bizCode refusal must be definitive", e)
	}
	// Unsigned 2xx and 5xx -> unknown.
	var unknown *UnknownOutcome
	status, reply = 200, `{"code":"0000","respData":{"bizCode":"0000","payUrl":"x"}}`
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("unsigned reply must be unknown", e)
	}
	status, reply = 503, `{}`
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("5xx must be unknown", e)
	}
}

func TestPluginSuixingpayNotification(t *testing.T) {
	p, _ := BuildPluginChannel(suixingpayChannel("alipay"))
	sp := p.(*suixingpayPlugin)
	raw := suixingpaySignReply(t, `{"bizCode":"0000","ordNo":"T777","amt":"10.00","sxfUuid":"U77","buyerId":"2088x"}`)
	exp := ExpectedPayment{TradeNo: "T777", Amount: 1000}
	ev, e := sp.VerifyNotification(context.Background(), nil, []byte(raw), exp)
	if e != nil || ev.State != "SUCCESS" || ev.Amount != 1000 || ev.ProviderTradeNo != "U77" || ev.BuyerID != "2088x" {
		t.Fatal(ev, e)
	}
	// Bad signature.
	bad := strings.Replace(raw, `"amt":"10.00"`, `"amt":"20.00"`, 1)
	if _, e := sp.VerifyNotification(context.Background(), nil, []byte(bad), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("tampered amount must fail sign", e)
	}
	// Wrong amount with a correctly-signed-but-mismatched body.
	raw2 := suixingpaySignReply(t, `{"bizCode":"0000","ordNo":"T777","amt":"99.00","sxfUuid":"U77"}`)
	if _, e := sp.VerifyNotification(context.Background(), nil, []byte(raw2), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	// Wrong trade number.
	if _, e := sp.VerifyNotification(context.Background(), nil, []byte(raw), ExpectedPayment{TradeNo: "OTHER", Amount: 1000}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// Non-success bizCode.
	ns := suixingpaySignReply(t, `{"bizCode":"3001","ordNo":"T777","amt":"10.00"}`)
	if _, e := sp.VerifyNotification(context.Background(), nil, []byte(ns), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success", e)
	}
	// Ack bodies.
	if st, ct, body := sp.NotificationAck(nil); st != 200 || !strings.Contains(body, "success") || !strings.Contains(ct, "json") {
		t.Fatal(st, body)
	}
	if _, _, body := sp.NotificationAck(ErrAuthentication); !strings.Contains(body, "fail") {
		t.Fatal(body)
	}
}

func TestPluginSuixingpayRefund(t *testing.T) {
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(suixingpayChannel("wxpay"))
	sp := p.(*suixingpayPlugin)
	sp.hostOverride(srv.URL)
	reply = suixingpaySignReply(t, `{"code":"0000","respData":{"bizCode":"0000","origOrderNo":"T9","amt":"3.00"}}`)
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T9", RefundNo: "R9", Amount: 300, TotalAmount: 1000})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 300 || rr.ProviderRefundNo != "T9" {
		t.Fatal(rr, e)
	}
}
