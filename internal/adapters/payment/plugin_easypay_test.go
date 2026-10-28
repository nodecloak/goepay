package payment

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Keys generated once (2048-bit) with Go crypto/x509 for these tests.
const easypayMchPrivB64 = "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDIzmdNj+pskyjrN3GCAgVhnBSlAqRg1uIGIEY/cJSDRZnmS+R9yWeIhhoxlCP63oHjQA0OoFgZV/HjOpwr0kpt7AbqZGjbcai5w63gMTGy5oQ+6W/GRVLMJsWwLggdRjR1DrD5C3qM2Eak5WnETYwLwt040mAxmn1bxoD5gsmeKdqR/6CLR8UnKhRl8B3kyVWuxh/AlUDJzCJo6l4LCDMVqbukraEZaTRaaREaUgHuSrK6yq2ZrdQqGhV0C3Dl4TrpukheR8Y5AcU+IUqFBsZZrixQNoblYTyQ0sC5iVUWA1HxayNkwILSXwpNj+RVRcy4mJ+GNKCl32jhvJxw9T8BAgMBAAECggEAYG/P6M3+9VnPQveDib5Su0fsGjskmmVYczy4v+rfSBP7D0DPYSGCmWxd1AWjKe4GTw8JtkaHSNSVlF0nd1ws5pF92L+UJB93uo8hSBQYHCTcyiHtzjioxOax9iSFPRnkQ3tkvpYmqtGLS9Hpc58SbOuOtqVCEVByd8rjUfDfxxbUxBZrhn5xyzPQY4N0Nm6CY2lTmgaT5K5BBoTHqOo782pRMwyqNaxrfboQvMbXvnDoAgI5zFKosSJRYKnqH2DoMWvuAvArstKwecY0tCsT9m4/F6luI8hcnTkR2hDbaR57nBZIA0WOuBA22+LSj4vWGrShq6W9UUfa+aZ764KQSQKBgQDpjtiJsNf1b1sOqAr9z38Oi/e3SUkZS/5k2NfbHmtRYadw66A2XjRIBi73bZNac22InMg5E7VCMxTgm7/jMUsXqNagnSUcRMcTUBtva7OcS0b0x/BuG/picIHkGTr7XAW4vEe2LP6W4AVSF1jQsgQu6UC38zbYgfWtYT8PZ1j+xwKBgQDcGerasGLDOj5sxMcEQCYb2Vx+2u8OfpVT2di2QBkTgCFK5AQFOFBr/ozZSBXkW702MksKtzmVK9k/bU9rNUDRlqZlmqlaTP9O+LBRaJ0p4Z4Ku4+HiqNY5sOvQ3Is8Uz2DqaOqs3UWIS08PN/kaKv1G3Yo3FFv7JCLwcYYlQr9wKBgC42FEQSGHi4YrTgrOQRNRuEuCustWjq71e2y+bP+v3sK3wdMuLu+rsxdgOAy8H+yrJYCwm4C4ogn0XdqzbhnvD8eBLpZNlWBmmc1QDTt4L6JfzyvnCa2oy1pA2krFXlnEA0CKBUAL+LbaITeoA4aLl7b1G2Lqjh9eErN5Xtpd8rAoGBAMUGhk+hWad9eegP9j991R9U4hHElf6Znb7j4bSziD8Gkcbieh7aHFOwGBFhEAfNf4EGYFqFTgyUpfK7PT6w+wcjHm0Rl8v35QuiXuNhDF8zUjA1WfRxTGwGluFLNOe6UbRpxPstNveBDbqTfsWRbtqLBJHLLVjMkxvf0TAoxURdAoGAFGEv+pSZEkzU3DHYR2DB0SxUEOxwvBdTrXI6HiGLxanyM0nViMDPYbOCAcfU5HVp/TXQADt60v9ki8PXZPuJnfpczYR/NLE5pDUJgv6QaUHXWQz4I2Hkidxo7pFeNmOaDxJ81J1F8Fsm4+XXANx8n6RFGS+bWuYstPHukzoo++8="

const easypayPlatformPubB64 = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA2Df5tckH2ERZ+h79c9/JiZhj0apTs1Z17QO18EFd7ACUfmvDMon9WRPuvPhAxZ+lpSZsw7gRWLqln0BkMk1LVpQrMspDQu1CEwVgK9tNX/p6JwnUQVgJ6HeEj2txbIfVJbCBnAJOCpkegzZAbZWzl3hF5/AR4efQK7Y4LYFycn6Q1mnXGVbYBv9HiHQFk8/UFPgbUlGoeTntgCQuPnVhZyOrr/XF74dVxUUv30qqMEheCSlQpTIuvPIgc3B00cei14H3Kx+ZeuGGJQOgU0geEYt+mR6KMkaHAhdZUCsUn11G2U9yjxB1tyNQb1rMAaqG4OzHBvl4AyWsFzSp4cQq0QIDAQAB"

const easypayPlatformPrivB64 = "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDYN/m1yQfYRFn6Hv1z38mJmGPRqlOzVnXtA7XwQV3sAJR+a8Myif1ZE+68+EDFn6WlJmzDuBFYuqWfQGQyTUtWlCsyykNC7UITBWAr201f+nonCdRBWAnod4SPa3Fsh9UlsIGcAk4KmR6DNkBtlbOXeEXn8BHh59ArtjgtgXJyfpDWadcZVtgG/0eIdAWTz9QU+BtSUah5Oe2AJC4+dWFnI6uv9cXvh1XFRS/fSqowSF4JKVClMi688iBzcHTRx6LXgfcrH5l64YYlA6BTSB4Ri36ZHooyRocCF1lQKxSfXUbZT3KPEHW3I1BvWswBqobg7McG+XgDJawXNKnhxCrRAgMBAAECggEACvR3FfNMDvSTnH0ieVL+2pZahF59ZCo+ylX0S2p8cD8tRuR93Nsp92/vmip32at3kJEMVLRISLcYOiPgj73j0cyrDaU62DK+NMmCcGZpmrHNq5mvY+oqglk/4aZfdlDBh2nUf0uMmbPdXGeMxyCdG1EQNaqvtULyBKz0VrXmzw8ruyum6pWizGSe22xAmqOfxy+gNCJTSB0/7/r36R6PTDMlUGfAqq587+99bnGR4al0jnZ9FkZoy3QgR1GqfKcscufbksm8knlkIYMd1WOn/nkVdRZFXB1GUX38pPOG8o0b3FLfzXzftVk0O50S9pPNIP+wxDnY5sq/H+xBo3tvswKBgQDi1ap1NNNY0ptcQSACy+VidMiIlEJGiDePMEjPt+iRoSMRmeUWwGdlt8YAwW98lw7Cfm3GKCCkRRQdnfxfgQTkLSgBy+oI8OWF2wMWutEYwtGTvXj15FAkio47uLd+9W9QQVJbpAr0uXpKK0Ecerw15fvKFX3UdXZqaQfxMbtXbwKBgQD0BOGbT8BV1whFK5CbXShb5iiavY6FkJjlXipm4kJNzoaiN0nsU7H9DKeEFRsHG/5rP0BWHpYuO4GrcuOyJPUUxsbIQlAFBBvLo69nrCn4sTYgMiG/cvVjN6Mp7o7E4SpCgC+nwUxq9mt+tI+ZbVpaBBdcoHvmHabIR0J1P4GBvwKBgQDMbjRJnWo8StKpuifXXVtF+GoibTutxWYNLj7wtZZsaQTqAABvGMgW7CEO3UZUyu8KalcUl2WTpo4SgyfICscVvXyo4dXyaOjyUgfenE+GOU4YEFLLiRCRXi4BtN/rE9dWRQisfpNBK0gDQChWOmcVTEg4QFY++/3UuQPnZCKrEQKBgF7LIqVF9u6hySmtwdBJASuqHm32Y05JnMvFp+0tyhoVt8F+fu4amVM3PkAWPtilLFGXUbPiz+6H2Vi0GfYTW03F5mTXNeSs8utJ3BMHE4rztJYPQ4Lu8pqne3AHp1ieK9+Hknu1oK0QqzOeiwZML8Q2QKkQAvGi5S56WDvZ/sJbAoGAU48ZChlTPHhHcFRAcmAOO63kGT7zvbjIAoSsFczefQBvK8iXQKmti35a7cdkOqhd+joFeCEPr7X1OwHTAFnMidnZ2r0t/z0MdfbL77AbvRfa9L8ZMLgKRY11ho4oxXd3T8qm/kosW0fMTLPqsb/fdyudrgQAB5pwEeDIcMQFNPk="

func easypayPlatformPriv(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	der, _ := base64.StdEncoding.DecodeString(easypayPlatformPrivB64)
	k, e := x509.ParsePKCS8PrivateKey(der)
	if e != nil {
		t.Fatal(e)
	}
	return k.(*rsa.PrivateKey)
}

func easypayChannel(payType, reqtype string, apps ...string) PluginChannel {
	return PluginChannel{ID: 14, Plugin: "easypay", PayType: payType, AppTypes: apps,
		Config:       map[string]string{"reqtype": reqtype, "appid": "R1", "appmchid": "SUB1", "appswitch": "0"},
		Certificates: map[string]string{"appkey": easypayPlatformPubB64, "appsecret": easypayMchPrivB64}}
}

func TestPluginEasypayRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("easypay")
	if !ok || spec.Title != "易生易企通" || !spec.Serves("bank") || spec.Serves("qqpay") {
		t.Fatal("easypay spec", ok)
	}
	if _, e := BuildPluginChannel(easypayChannel("alipay", "1")); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{easypayChannel("qqpay", "1")}
	for _, miss := range []string{"appid"} {
		c := easypayChannel("alipay", "1")
		delete(c.Config, miss)
		bad = append(bad, c)
	}
	missPriv := easypayChannel("alipay", "1")
	delete(missPriv.Certificates, "appsecret")
	bad = append(bad, missPriv)
	// 机构模式 without sub-merchant.
	instNoSub := easypayChannel("alipay", "2")
	delete(instNoSub.Config, "appmchid")
	instNoSub.Config["reqtype"] = "2"
	bad = append(bad, instNoSub)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginEasypayCanonicalSignContent(t *testing.T) {
	p, _ := BuildPluginChannel(easypayChannel("alipay", "1"))
	ep := p.(*easypayPlugin)
	header := map[string]any{"transTime": "20261029010101", "reqId": "R1", "reqType": "1"}
	body := map[string]any{
		"reqInfo":         map[string]any{"mchtCode": "M1"},
		"reqOrderInfo":    map[string]any{"orgTrace": "T1", "transAmount": 123, "orderSub": "VIP会员", "backUrl": "https://pay.example/n"},
		"payInfo":         map[string]any{"payType": "AliPayNative", "transDate": "20261029"},
		"settleParamInfo": map[string]any{"delaySettleFlag": "0", "patnerSettleFlag": "0", "splitSettleFlag": "0"},
		"riskData":        map[string]any{"customerIp": "1.2.3.4"},
	}
	// signContent computed independently with a node PHP-equivalent canonicaliser.
	const want = `{"reqId":"R1","reqType":"1","transTime":"20261029010101"}052748EC1CC9D4B3A8505C7018161C3D`
	if got := ep.signContent(header, body); got != want {
		t.Fatalf("got %q", got)
	}
}

// easypayWrap signs an upstream reply envelope with the platform private key.
func easypayWrap(t *testing.T, header, body map[string]any) string {
	t.Helper()
	sc := easypayCanonicalJSON(header) + strings.ToUpper(md5Hex(easypayCanonicalJSON(body)))
	s, err := rsaSign(easypayPlatformPriv(t), crypto.SHA256, sc)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPluginEasypayCreateNative(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		rspHeader := map[string]any{"rspCode": "000000", "transTime": "20261029010102"}
		rspBody := map[string]any{"respStateInfo": map[string]any{"respCode": "000000", "transState": "0"}, "respOrderInfo": map[string]any{"qrCode": "https://qr.ey/U1", "outTrace": "E9"}}
		sign := easypayWrap(t, rspHeader, rspBody)
		out := map[string]any{"rspHeader": rspHeader, "rspBody": rspBody, "rspSign": sign}
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	p := mustEasypay(t, srv.URL, "alipay", "1")
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", Amount: 123, ClientIP: "1.2.3.4"}
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.ey/U1" || res.ProviderTradeNo != "E9" {
		t.Fatal(res, e)
	}
	if !strings.Contains(gotPath, "/trade/native") || !strings.Contains(gotBody, "AliPayNative") || !strings.Contains(gotBody, "reqSign") {
		t.Fatal("request", gotPath, gotBody)
	}
}

func TestPluginEasypayCreateJSAPIAndOutcomes(t *testing.T) {
	var scenario int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch scenario {
		case 0: // wechat jsapi success
			rspHeader := map[string]any{"rspCode": "000000"}
			rspBody := map[string]any{"respStateInfo": map[string]any{"respCode": "000000", "transState": "1"}, "wxRespParamInfo": map[string]any{"wcPayData": `{"appId":"wx1"}`}, "respOrderInfo": map[string]any{"outTrace": "E9"}}
			json.NewEncoder(w).Encode(map[string]any{"rspHeader": rspHeader, "rspBody": rspBody, "rspSign": easypayWrap(t, rspHeader, rspBody)})
		case 1: // business refusal via respStateInfo
			rspHeader := map[string]any{"rspCode": "000000"}
			rspBody := map[string]any{"respStateInfo": map[string]any{"respCode": "900001"}}
			json.NewEncoder(w).Encode(map[string]any{"rspHeader": rspHeader, "rspBody": rspBody, "rspSign": easypayWrap(t, rspHeader, rspBody)})
		case 2: // bad response signature -> unknown
			rspHeader := map[string]any{"rspCode": "000000"}
			rspBody := map[string]any{"respStateInfo": map[string]any{"respCode": "000000", "transState": "1"}}
			json.NewEncoder(w).Encode(map[string]any{"rspHeader": rspHeader, "rspBody": rspBody, "rspSign": "AAAA"})
		case 3:
			io.WriteString(w, "not json")
		}
	}))
	defer srv.Close()
	p := mustEasypay(t, srv.URL, "wxpay", "2")
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "jsapi", NotifyURL: "https://pay.example/n", Amount: 123, OpenID: "o-x"}
	scenario = 0
	res, e := p.Create(context.Background(), r)
	if e != nil || res.MaterialType != "jsapi" || !strings.Contains(res.Material, "wx1") {
		t.Fatal(res, e)
	}
	scenario = 1
	var pe *ProviderError
	if _, e := p.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business refusal must be definitive", e)
	}
	var unknown *UnknownOutcome
	for _, sc := range []int{2, 3} {
		scenario = sc
		if _, e := p.Create(context.Background(), r); !errors.As(e, &unknown) {
			t.Fatal("must be unknown", sc, e)
		}
	}
	// jsapi needs openid.
	if _, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "jsapi", NotifyURL: "https://pay.example/n", Amount: 1}); !errors.Is(e, ErrInvalid) {
		t.Fatal("jsapi needs openid", e)
	}
}

func TestPluginEasypayNotification(t *testing.T) {
	p, _ := BuildPluginChannel(easypayChannel("alipay", "1"))
	v := p.(NotificationVerifier)
	header := map[string]any{"transTime": "20261029010103"}
	body := map[string]any{"respStateInfo": map[string]any{"transState": "1"}, "respOrderInfo": map[string]any{"orgTrace": "T1", "outTrace": "E9", "transAmount": "123", "userId": "o-x"}}
	wrap := func(h, b map[string]any) []byte {
		out := map[string]any{"reqHeader": h, "reqBody": b, "reqSign": easypayWrap(t, h, b)}
		raw, _ := json.Marshal(out)
		return raw
	}
	exp := ExpectedPayment{TradeNo: "T1", Amount: 123}
	ev, e := v.VerifyNotification(context.Background(), nil, wrap(header, body), exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "E9" || ev.Amount != 123 || ev.BuyerID != "o-x" {
		t.Fatal(ev, e)
	}
	// bad signature
	badSig := map[string]any{"reqHeader": header, "reqBody": body, "reqSign": "AAAA"}
	bs, _ := json.Marshal(badSig)
	if _, e := v.VerifyNotification(context.Background(), nil, bs, exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign accepted")
	}
	// wrong amount
	if _, e := v.VerifyNotification(context.Background(), nil, wrap(header, body), ExpectedPayment{TradeNo: "T1", Amount: 999}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong amount accepted")
	}
	// wrong trade
	if _, e := v.VerifyNotification(context.Background(), nil, wrap(header, body), ExpectedPayment{TradeNo: "OTHER", Amount: 123}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong trade accepted")
	}
	// non-success state
	bad := map[string]any{"respStateInfo": map[string]any{"transState": "9"}, "respOrderInfo": map[string]any{"orgTrace": "T1", "transAmount": "123"}}
	if _, e := v.VerifyNotification(context.Background(), nil, wrap(header, bad), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success accepted", e)
	}
	if s, ct, b := p.(NotificationAcker).NotificationAck(nil); s != 200 || !strings.Contains(ct, "json") || !strings.Contains(b, "000000") {
		t.Fatal(s, b)
	}
}

func TestPluginEasypayRefund(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rspHeader := map[string]any{"rspCode": "000000"}
		rspBody := map[string]any{"respStateInfo": map[string]any{"respCode": "000000", "transState": "0"}, "outTrace": "RE9"}
		json.NewEncoder(w).Encode(map[string]any{"rspHeader": rspHeader, "rspBody": rspBody, "rspSign": easypayWrap(t, rspHeader, rspBody)})
	}))
	defer srv.Close()
	p := mustEasypay(t, srv.URL, "alipay", "1")
	rr, e := p.Refund(context.Background(), RefundRequest{TradeNo: "20261029T1", ProviderTradeNo: "E9", RefundNo: "R1", Amount: 100, TotalAmount: 123})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 100 || rr.ProviderRefundNo != "RE9" {
		t.Fatal(rr, e)
	}
}

func mustEasypay(t *testing.T, gateway, payType, reqtype string, apps ...string) Provider {
	t.Helper()
	p, e := BuildPluginChannel(easypayChannel(payType, reqtype, apps...))
	if e != nil {
		t.Fatal(e)
	}
	p.(*easypayPlugin).gateway = gateway
	return p
}
