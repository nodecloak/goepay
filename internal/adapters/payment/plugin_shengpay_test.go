package payment

import (
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const shengpayMerchPriv = `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQCLyDMH3RWpCj2N
h0SKcl81wCxqElbjYQCvPvXgsM/vFr4QMp2MsSthUetbk+Bz08dJ3d9PZ1+sBiGj
0YKzX6R9C5kV1qRWgQBC1EjNNuFLOOowaeZ6pZTpZlboLSo+gdsB5CXntpVB42QO
YJ2RqJHIU7V1DxNJNULkRNdFyF/NyxbMdiTapxPg+bvJHnqsDeB1e92q9uD/vxek
GbkmIT6GcKxZivqoO3C27/QaM13U5+TBqCOlxOverubZkZ3gZym5phKzh4kc9OL0
WCU/FAwK8ygFzCUkrj0gyBz5Gdpj6IsYz1c0/sI6Wtbyc4DpEucfhVeHJIUAHIMt
hqPRv5nzAgMBAAECggEAEzpTwrYnv5v4AZrk08KN1D/+HA0vxJFYXcLkYacQw0u2
k1XTFEJevEFfhbZevrAYDtIlYK/QmkIxqG4uc2wpnUXWJgnjiz6tgYPgC9KLxSQs
GsLGgIJ8bGq2c7OaxwRn0ufGSwxzlZ+o6M1jf2DXxN227UZImd5ouCu8DQ5WF2Gv
T/f0ba1FB+PmCyjmgbi2W7g3LQ1o5vqixzx5qggDSIwRHoxlhcwJ8YFhiynYZ7HD
66l7NvzaKGVw6eDBnubw9Wla0rPFs8j/Mi4AGXNch6iPwRDbhzvVVZZcSgPGForj
VJlfOY/2jmlS97SKoZyqfYIvtkO83F/Mo/Jvu9cCcQKBgQDBXs0uKrHWpxEpcxTK
vBeDciafNy7XLzPDVfDdxFdPEe2aPWvJZEi2iIokIQ6Se0BzdhqCtbKlAgJgQr88
mhniTN6ewvUNLP3Vu4IZYJqmr4SJMT67AyC4bA8y06myEMVzbnjYHdQ64BU8pZWg
4VdnCPks1tVYOGgMg8fw4ItauwKBgQC5DicmLYKbui4lfQAR8lAnGmAJrs1oHEOa
53fq2BJsnRwkAs4sZ224+R2RmQIpf+21PL82qZZ9WVlBsE2qhT4IoP4VEDfUPI+E
LCpZdy5JPlKNZ7BaxACfyOvc0T0nQ9wwPlLd2oEqOlQaV47WHYErda/iUe7R5/lE
8mk0T8YWKQKBgQCug2pVQf7hpEFe0RdwqDPoUzr78gd5+IWUwpjw6+OH3rw6uTfv
t2vQIIXbeWH3XPibu3jNgkHJnXibe2GvxbuaeplnWrdhvrsPC3ysfnzT9g986U6d
5ILBLfTBgDjkFTK9rHOvR6xjvhwscZwKZ5RDzttiVjiQt8dflAb8IwphRQKBgDmC
g3oi5Jce2h5+ZVoucuhkEvqPX3h+aCgozSSnt7oXbVpnoMk972RvztejtH9PKYGM
emQzefhEny+B6fEXbXrjBXNoUeFcXMGXtCWPm0vZ2W/mjHD4vrTpjRtXz2h3d2Nq
yvf1YvTLob15/oLeMnleHqX4Dn8fV2Es8LL4I7hZAoGBAL4fQDS4PAsPSH/I4IrL
GDMfNqoV1LNGbHNMyhNRSRNbAC3N1jU0eQjayakvsNd7roWMQ9t+vHcmWAtCv+N4
UGP3FoZAAIz0J70jKo/wTKaSLRmFAYwauHOtsTifFFUuWILjmDX6Q7cQD3g+Z0+d
5yLdOcqViuTpuWgGMgbNZ+u/
-----END PRIVATE KEY-----`

const shengpaySdpPub = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAk8/uIds7au7Ett4JJPL6
uBkKbCakgKcHFdg4FvoxqmPYGUr6jsR3Yrd7bkBjOs3Y1e9Rjn4eczNJJezVLoRw
2kTDiT8mYvFsPms3kAECgM4nU3gyqp21eS5pULByklGF+m6QG3UcMrGiSFzqlVwz
40rlrQ8yrqTE3YfEkjEzTI9/CJUxOWYBCf3BtJOIoZpLnB7mqyDDtos9gCWVTc2X
CCBATZDlvAlICML5jvohjFLnaWDfxBbs/8KZY5HE3jGmqhM8n4010xxeBRErFGSl
nEsNov4dYKPpQ40bf/YYyykkeJgnLa7eRKJhgdbV9aP0M963IFCUVmKFWCwrhnzz
VwIDAQAB
-----END PUBLIC KEY-----`

const shengpaySdpPriv = `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQCTz+4h2ztq7sS2
3gkk8vq4GQpsJqSApwcV2DgW+jGqY9gZSvqOxHdit3tuQGM6zdjV71GOfh5zM0kl
7NUuhHDaRMOJPyZi8Ww+azeQAQKAzidTeDKqnbV5LmlQsHKSUYX6bpAbdRwysaJI
XOqVXDPjSuWtDzKupMTdh8SSMTNMj38IlTE5ZgEJ/cG0k4ihmkucHuarIMO2iz2A
JZVNzZcIIEBNkOW8CUgIwvmO+iGMUudpYN/EFuz/wpljkcTeMaaqEzyfjTXTHF4F
ESsUZKWcSw2i/h1go+lDjRt/9hjLKSR4mCctrt5EomGB1tX1o/Qz3rcgUJRWYoVY
LCuGfPNXAgMBAAECggEAHb48AVUPnkNgkxmXxnccS1pCliOqe3bgvp0NIFaFEu/X
ZrOHOp4/Fb6tGDDdptIQHXB0PVOUhgLeZ8LB3QPqf4lRvXbXHxbMfLzvZ8tEOS4C
jNHFhD6K61ETARkYXOgJqSyBU/0bSPokmSIVVCvDBywljk7eGHVwZdFxlSdQVsnE
7QJmrEwzM40bQNxujt/DLMQw3HwtM95QAE7NJjLHEp6VDt7Jan1VlnLXnh1Flguz
4JpR7l/QMMBkpg9FMyIs+dQOKHXglgaWt32qEYrjc7w2xLjrN601eKAJ13pHqzS4
ZFToiE6tTgIXa7HGg2usrprAlZQ8yT7jVYtGYDb/DQKBgQDGmQXdJeIWHr26ezqf
qwjwKwIPl8Q1RQ9kdBequc/02/6AqOjS9TLgDdXRJmTlLkRmqCESL8MA8rCTHBEV
7aartE4qfUUwkU1m5BSz6hBs+KtAf8pHT6zyfHvrvXGxvH0HpvVYsML+6UpvMLqp
v0wLIeij8zMesUGHbKGKl2F+awKBgQC+iRmjPtsUOTa2zyEScPxCiQf/n+L5MNcn
/qJzVoDu5FKYpQ64W8OVnPFoaWt53Ab962st4VJyCxkzBo9w/ywooKBzIxEcpB5n
01bWoZHsZRWOb9utIi9TJQhEagsNpPlwjY0WO1K1A9M504cmZPzQ7O753ad5XOHt
7PdZUnjBxQKBgEnSTdyznUPNr4bnEZaSdCMaSYzUREns3tjfUoDSyxtgv0zB/LGW
FZA1dFpnZC/Qr/pc2tYwzNLIY5x7XVqCWWcQAdJHeHWiPVxWwd9SzUJEeRvjpKQ3
WLlWTpYFeIKx5uP2StLp+cHZpCKKwYghpLmqpBh1aXYqO4qEddPXi5T7AoGBALf3
zGdQk1tNesS77WTO7uKXSL/Kcu7D95pIRDY0+byvfAiipjnfcFMO/BAiaYF5rs6Q
9iWQf3hZuvUHioxB9TMtFH7XBviuhiUda6VEz45+c3faDTN35FXVVcwoz49XEPlZ
0zX6BXt9u/8DfGTX8TsQ6hUbrU6DHioKOTFnbe05AoGBALzoYsVxnZ8TlXVzUJ25
eMC0FBWZ34QqH9VFd6vIJSOk4hQeVW039y4OT1Bd/Al/P1+F2a08EAKgqx+SfrbM
qxZROquL22zmpfYuI7g/E0XV21DMEfV9EMNdgX+BX6yEKDw3OO6HffoGBU7Uulgb
6Z4TQvY9Ge53bn9C3drnYECa
-----END PRIVATE KEY-----`

func shengpayChannel(payType string) PluginChannel {
	return PluginChannel{ID: 40, Plugin: "shengpay", PayType: payType,
		Config:       map[string]string{"appid": "MCH1"},
		Certificates: map[string]string{"appkey": shengpayMerchPriv, "appsecret": shengpaySdpPub},
	}
}

func TestPluginShengpayRegistry(t *testing.T) {
	spec, ok := LookupPlugin("shengpay")
	if !ok || spec.Title != "盛付通" || !spec.Serves("alipay") || spec.Serves("qqpay") {
		t.Fatal("spec", spec, ok)
	}
	if _, e := BuildPluginChannel(shengpayChannel("wxpay")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{shengpayChannel("qqpay")}
	missing := shengpayChannel("alipay")
	delete(missing.Certificates, "appkey")
	badkey := shengpayChannel("alipay")
	badkey.Certificates["appsecret"] = "bad"
	bad = append(bad, missing, badkey)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
	// Methods per pay type.
	ap, _ := BuildPluginChannel(shengpayChannel("alipay"))
	if got := ap.Capabilities().Methods; strings.Join(got, ",") != "page,wap,native" {
		t.Fatal("alipay methods", got)
	}
}

// shengpaySignReply signs a flat JSON reply with the sdp private key over
// shengpaySignContent and inserts the sign field — verifying our sign base is
// ShengPayClient::getSignContent (flat ksort).
func shengpaySignReply(t *testing.T, fields map[string]string) string {
	t.Helper()
	sdp, e := pluginPrivateKey(shengpaySdpPriv)
	if e != nil {
		t.Fatal(e)
	}
	content := shengpaySignContent(fields)
	sig, e := rsaSign(sdp, crypto.SHA1, content)
	if e != nil {
		t.Fatal(e)
	}
	cp := map[string]string{}
	for k, v := range fields {
		cp[k] = v
	}
	cp["sign"] = sig
	raw, _ := json.Marshal(cp)
	return string(raw)
}

func TestPluginShengpaySignVector(t *testing.T) {
	// Independent: sign with the sdp key, verify the plugin accepts it, and
	// confirm the sign content string is exactly ksort "k=v&…".
	p, _ := BuildPluginChannel(shengpayChannel("wxpay"))
	sp := p.(*shengpayPlugin)
	fields := map[string]string{"mchId": "MCH1", "outTradeNo": "T1", "totalFee": "100", "resultCode": "SUCCESS", "empty": ""}
	content := shengpaySignContent(fields)
	if content != "mchId=MCH1&outTradeNo=T1&resultCode=SUCCESS&totalFee=100" {
		t.Fatal("content", content)
	}
	signed := shengpaySignReply(t, fields)
	m, _ := shengpayFlatten([]byte(signed))
	if !sp.verify(m) {
		t.Fatal("sdp signature did not verify")
	}
}

func TestPluginShengpayCreate(t *testing.T) {
	var gotBody string
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, reply)
	}))
	defer srv.Close()

	// wxpay native -> qrcode
	p, _ := BuildPluginChannel(shengpayChannel("wxpay"))
	sp := p.(*shengpayPlugin)
	sp.hostOverride(srv.URL)
	reply = shengpaySignReply(t, map[string]string{"returnCode": "SUCCESS", "resultCode": "SUCCESS", "payInfo": "weixin://wxpay/bizpayurl?pr=aa", "transactionId": "SP9"})
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "T100", Subject: "商品", Method: "native", NotifyURL: "https://pay.example/n", ClientIP: "203.0.113.2", Amount: 100})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "weixin://wxpay/bizpayurl?pr=aa" || res.ProviderTradeNo != "SP9" {
		t.Fatal(res, e)
	}
	if !strings.Contains(gotBody, `"tradeType":"wx_native"`) || !strings.Contains(gotBody, `"outTradeNo":"T100"`) {
		t.Fatal("request body", gotBody)
	}

	// alipay page -> jump
	p2, _ := BuildPluginChannel(shengpayChannel("alipay"))
	sp2 := p2.(*shengpayPlugin)
	sp2.hostOverride(srv.URL)
	reply = shengpaySignReply(t, map[string]string{"returnCode": "SUCCESS", "resultCode": "SUCCESS", "payInfo": "https://openapi.alipay.com/gateway?x=1", "transactionId": "AP1"})
	res, e = p2.Create(context.Background(), CreateRequest{TradeNo: "T200", Subject: "s", Method: "page", NotifyURL: "https://pay.example/n", Amount: 100})
	if e != nil || res.MaterialType != "jump" || res.Material != "https://openapi.alipay.com/gateway?x=1" {
		t.Fatal(res, e)
	}
	if !strings.Contains(gotBody, `"tradeType":"alipay_pc"`) {
		t.Fatal("alipay pc", gotBody)
	}
}

func TestPluginShengpayCreateOutcomes(t *testing.T) {
	var reply string
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(shengpayChannel("wxpay"))
	sp := p.(*shengpayPlugin)
	sp.hostOverride(srv.URL)
	r := CreateRequest{TradeNo: "T2", Subject: "x", Method: "native", NotifyURL: "https://pay.example/n", Amount: 50}

	// resultCode != SUCCESS -> business refusal.
	status, reply = 200, shengpaySignReply(t, map[string]string{"returnCode": "SUCCESS", "resultCode": "FAIL", "errorCode": "PARAM"})
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business refusal must be definitive", e)
	}
	// returnCode != SUCCESS with returnMsg -> refusal.
	status, reply = 200, `{"returnCode":"FAIL","returnMsg":"签名错误"}`
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("returnCode refusal", e)
	}
	// 5xx -> unknown.
	var unknown *UnknownOutcome
	status, reply = 502, `{}`
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("5xx unknown", e)
	}
	// Signed success but with a tampered (unsigned) payInfo set -> sign fail -> unknown.
	status, reply = 200, `{"returnCode":"SUCCESS","resultCode":"SUCCESS","payInfo":"x","sign":"AAAA"}`
	if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("bad sign unknown", e)
	}
}

func TestPluginShengpayNotification(t *testing.T) {
	p, _ := BuildPluginChannel(shengpayChannel("wxpay"))
	sp := p.(*shengpayPlugin)
	good := shengpaySignReply(t, map[string]string{"status": "PAY_SUCCESS", "outTradeNo": "T777", "totalFee": "100", "transactionId": "SP77", "payerInfo": `{"openid":"o-xyz"}`})
	exp := ExpectedPayment{TradeNo: "T777", Amount: 100}
	ev, e := sp.VerifyNotification(context.Background(), nil, []byte(good), exp)
	if e != nil || ev.State != "SUCCESS" || ev.Amount != 100 || ev.ProviderTradeNo != "SP77" || ev.BuyerID != "o-xyz" {
		t.Fatal(ev, e)
	}
	// Tampered totalFee breaks signature.
	bad := strings.Replace(good, `"totalFee":"100"`, `"totalFee":"200"`, 1)
	if _, e := sp.VerifyNotification(context.Background(), nil, []byte(bad), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("tamper", e)
	}
	// Wrong expected amount.
	if _, e := sp.VerifyNotification(context.Background(), nil, []byte(good), ExpectedPayment{TradeNo: "T777", Amount: 999}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	// Wrong trade number.
	if _, e := sp.VerifyNotification(context.Background(), nil, []byte(good), ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// Non-success status.
	ns := shengpaySignReply(t, map[string]string{"status": "PAY_FAIL", "outTradeNo": "T777", "totalFee": "100"})
	if _, e := sp.VerifyNotification(context.Background(), nil, []byte(ns), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success", e)
	}
	// Ack bodies.
	if _, _, body := sp.NotificationAck(nil); body != "SUCCESS" {
		t.Fatal(body)
	}
	if _, _, body := sp.NotificationAck(ErrAuthentication); body != "SIGN FAIL" {
		t.Fatal(body)
	}
	if _, _, body := sp.NotificationAck(ErrInvalid); body != "FAIL" {
		t.Fatal(body)
	}
}

func TestPluginShengpayRefund(t *testing.T) {
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(shengpayChannel("wxpay"))
	sp := p.(*shengpayPlugin)
	sp.hostOverride(srv.URL)
	reply = shengpaySignReply(t, map[string]string{"returnCode": "SUCCESS", "resultCode": "SUCCESS", "refundId": "RF1", "refundFee": "30"})
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "T9", RefundNo: "R9", Amount: 30, TotalAmount: 100})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 30 || rr.ProviderRefundNo != "RF1" {
		t.Fatal(rr, e)
	}
}
