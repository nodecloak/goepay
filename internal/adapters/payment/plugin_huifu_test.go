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

// Fixed RSA-2048 keypair (bare base64 DER, as the channel config stores it) and
// vectors computed independently with node's crypto module.
const huifuTestPriv = "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDcKcEU2cNc+uzNipoJSNjhpmpB/7Sb439Q9XFP+C46P7bwC/28zcXWaY8uZ5uIH5pDXvC47RoOn4FzfhhjFC9tN5+SNsOenf2bsrtay7JN7ZN3IAkF8lBeC+lAt9Bo1knzuMnx4sDzR8kZtICOnTGVqnCP3dL4lJr3flHz8qpoRrD7JMe+tK0EDzBYLb5VQwFAxGyRLXPHqHc7M4DBHE38+dmlqD/r3DQ6fo7eXdA15ADl380KgBeqEe6P1RRP/nNh+jn7MnqY80Y9YZFj6Nns1drL60YVdbJKwMc+qyQS9KXDMnSd/OroCNxbbn8SNBiqpyV29j4340mU3GDwVnUtAgMBAAECggEAE+MxrdB99Ug1W9vJTk1mSfveXVfb2LkUJxwEwFSK2Hgo7kYnBYcQ9+1RIb/6LLGuHafV1t6UG/4Mk4IwccLBBfygTXTk9s8OuaHLisgbsy5iw8ABEKhy2gv5Zf80tEOnKnzmwRLamx9A6zGC618Hu51JzbUb+Zhy5yhh/sdM3V5adzqeyVvlre9gL6tqDnWfSVEpzjr+oYsywXqDqUy4O9t+9gpgkJpNj8TZeYGXYqhXHVbJFnx/5Ulxw9R6TbL2brMx/6nxVtvK9I/sprjZGBJn1r7CjOl0I/ChNUk65i3Zdy/XONIkuN+7X5Ql4tt2Lxer4LtpbzrF3mV2a5K7gQKBgQDzFHMItyMyQ4tcLvMN8KKcv+Sal9TFBjIqzQc5R7BRhbu6ujUUPa6ALwS64jWTWNt9pQp7oKmPgyjfZ5Lf7QKaWfrYGj5PCg7dzDcURZhE08nzSLOVDaqw3cTzaG9XDZ4U/zQrTZck1PUfzXW/O9ELgrVQIBjilr/QKKCUKx9K1wKBgQDn3XrTnT8vmj4HZv91J8ECTPaEgiIoraoBKGjxg8iiJiFJDwZCCbrtumuXK2tQDXpAlgbbN8e55BW6LCPXlDnwe9Gv8FnVupr9Lvc8fCycqzr09GcHtD8yqan2LPXpafneCinxwti76V74FkuKQkOGeP0fAiN9w40dATE2lcVjmwKBgQCdQ32pBdMfkdN3sGhPfH+4Ke4cll5xmbCbQEqMJHVOSNdL12y/y6g6BjFpNzCyQ6q9fylpEmihEJpA7yF2/j/Q3KYNae0l9bvog1afJud8oAjn/QEyTcVA6DmjwM1H5OeBbhqyaWB/08dC7AphzrFQYSAs1pdDAYTXh3FcawmzwQKBgB9/h/sNZpMJ6hlJVbTNpLuyRt7zar6J8QldOvxsQao9cdIh92CrP7ih1ZHgtSBX5F63wtCvADPxlz4GVwwnN8PCnla/WQTe58UzRyTxXYIfBNBYD1MNmnRAdjVaIEIIQoh2awJLzPOiHWOoa7SpkdDHyOMYgEfKc/YXvl7C8+C3AoGACk0q64lCtVE3Q3E5Y39PAiWnuuwiv8WAZSwx7IZe525Bnfnas65iJ0wts6fRZqwgw8gUrMQbOBwzZyhjJuK56Hmfy2JEu3C6CPxGl/TGWFCSQch+prIzi7wr75F5QiyN72um3/7rgaT2zg87mSmazpD/9GK7CVow5hqeDW1B44A="
const huifuTestPub = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA3CnBFNnDXPrszYqaCUjY4aZqQf+0m+N/UPVxT/guOj+28Av9vM3F1mmPLmebiB+aQ17wuO0aDp+Bc34YYxQvbTefkjbDnp39m7K7WsuyTe2TdyAJBfJQXgvpQLfQaNZJ87jJ8eLA80fJGbSAjp0xlapwj93S+JSa935R8/KqaEaw+yTHvrStBA8wWC2+VUMBQMRskS1zx6h3OzOAwRxN/PnZpag/69w0On6O3l3QNeQA5d/NCoAXqhHuj9UUT/5zYfo5+zJ6mPNGPWGRY+jZ7NXay+tGFXWySsDHPqskEvSlwzJ0nfzq6AjcW25/EjQYqqcldvY+N+NJlNxg8FZ1LQIDAQAB"

func huifuChannel(payType string) PluginChannel {
	return PluginChannel{ID: 92, Plugin: "huifu", PayType: payType, Config: map[string]string{
		"appid": "H1", "appurl": "P1", "appsecret": huifuTestPriv, "appkey": huifuTestPub,
	}}
}

func TestPluginHuifuRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("huifu")
	if !ok || spec.Title != "汇付斗拱平台" || !spec.Serves("ecny") || !spec.Serves("douyinpay") || spec.Serves("qqpay") {
		t.Fatal("huifu spec", ok)
	}
	if _, e := BuildPluginChannel(huifuChannel("alipay")); e != nil {
		t.Fatal(e)
	}
	bad := huifuChannel("alipay")
	bad.Config["appsecret"] = "not-a-key"
	if _, e := BuildPluginChannel(bad); e == nil {
		t.Fatal("accepted bad private key")
	}
	missing := huifuChannel("alipay")
	delete(missing.Config, "appkey")
	if _, e := BuildPluginChannel(missing); e == nil {
		t.Fatal("accepted missing public key")
	}
	if _, e := BuildPluginChannel(huifuChannel("qqpay")); e == nil {
		t.Fatal("accepted unserved type")
	}
}

func TestPluginHuifuSignatureMatchesOriginal(t *testing.T) {
	built, _ := BuildPluginChannel(huifuChannel("alipay"))
	p := built.(*huifuPlugin)
	data := map[string]string{
		"req_date": "20261029", "req_seq_id": "T1", "huifu_id": "H1", "trade_type": "A_NATIVE",
		"trans_amt": "1.00", "goods_desc": "商品", "notify_url": "https://pay.ex/n",
		"risk_check_data": `{"ip_addr":"1.2.3.4"}`, "alipay_data": `{"subject":"商品"}`,
	}
	got, e := p.sign(data)
	if e != nil {
		t.Fatal(e)
	}
	const want = "ihlVwzcUB+TEApyBfvu9ucRX7e+x5+nlQXZWJ2LGmIjkP7dUo4su1NhloCDzZhLgBpCpI/iPY4VgL9arRCuHWQ1MUy5jsmJGARtV9TQRztPfRrL797gx/KIH+sAkgQOyxBhLfJjtRb3duFZRWpDIXjN7M+7Gi+4S6w50O8w1AwYEtlMdW5g6hh2kgZqtZ9uA5fPdBOHcaeoeLxoiTg9ET7hy///dyA4tsFw8+wt2DNIP/o0U8fSEwMdcSDajQmVQP7saRlSIMJp0KjXW1OKsIbrJt+O0m9Lmtgw3hU88zIOIh+0dv/i0KrVF+cbBYktRleVl2lC+1rIIEGSKOd4IxA=="
	if got != want {
		t.Fatalf("huifu sign mismatch\n got=%s", got)
	}
}

func TestPluginHuifuCreateNativeAndVerifyReply(t *testing.T) {
	const respData = `{"resp_code":"00000000","hf_seq_id":"HF1","req_seq_id":"T1","trans_stat":"S","trans_amt":"1.00"}`
	const respSign = "iLCRzLxOnwcQq5UGYso2T4Smmr+BePxE54k3tg1ynasEqcNnKUsnfePX1mZx/++DWhuOQ1mCZTz+SYnONgRL2jul3qXUFnnVYhNI0zOdCfE+/+0I+Xlg2kUKzALkg0YLDo6aq/vmwZpaQ/GmU/meMwapYqXrfDcjGcOMC4/CvTK1CZ57Vun7a0cdel9smoic+42I/j5cUvS6qmD5Dq6fMW+NADztb8KqxeGDU/0B3MpGjyjrEmQD2IYOEyyrokjM6OwoYKxVorj5bkVFQjPUCG48Q0Wf5LsbT8gRlv0ynAKzLJFbHLHpMqpMWp1vTPFvq8ziXJMw9107Sc8tGHftEg=="
	var body map[string]any
	var replyWith func() string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)
		io.WriteString(w, replyWith())
	}))
	defer srv.Close()
	built, _ := BuildPluginChannel(huifuChannel("alipay"))
	p := built.(*huifuPlugin)
	p.gateway = srv.URL

	// The reply carries a real signature; create returns the qr_code material.
	replyWith = func() string {
		rd := map[string]any{"resp_code": "00000000", "hf_seq_id": "HF1", "req_seq_id": "T1", "trans_stat": "S", "trans_amt": "1.00", "qr_code": "https://qr.ex/HF1"}
		rdJSON, _ := json.Marshal(rd)
		// Re-sign the returned object canonically using the test key.
		return `{"data":` + string(rdJSON) + `,"sign":"` + huifuResign(t, rd) + `"}`
	}
	res, e := p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "商品", Method: "native", NotifyURL: "https://pay.ex/n", Amount: 100, ClientIP: "1.2.3.4"})
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.ex/HF1" || res.ProviderTradeNo != "HF1" {
		t.Fatal(res, e)
	}
	if body["sys_id"] != "H1" || body["product_id"] != "P1" || body["sign"] == nil {
		t.Fatal("envelope", body)
	}

	// A reply with a bad signature is rejected as authentication failure.
	replyWith = func() string { return `{"data":` + respData + `,"sign":"` + strings.Repeat("A", 344) + `"}` }
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "native", NotifyURL: "https://pay.ex/n", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad reply sign accepted", e)
	}
	_ = respSign

	// A business refusal (bad resp_code, correctly signed) is definitive.
	replyWith = func() string {
		rd := map[string]any{"resp_code": "20000000", "req_seq_id": "T1"}
		rdJSON, _ := json.Marshal(rd)
		return `{"data":` + string(rdJSON) + `,"sign":"` + huifuResign(t, rd) + `"}`
	}
	var pe *ProviderError
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "native", NotifyURL: "https://pay.ex/n", Amount: 100}); !errors.As(e, &pe) {
		t.Fatal("refusal", e)
	}
	// jsapi requires openid.
	if _, e = p.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "jsapi", NotifyURL: "https://pay.ex/n", Amount: 100}); !errors.Is(e, ErrInvalid) {
		t.Fatal("jsapi needs openid", e)
	}
}

func TestPluginHuifuNotification(t *testing.T) {
	const nraw = `{"trans_stat":"S","trans_amt":"1.00","req_seq_id":"T1","hf_seq_id":"HF1","wx_response":{"sub_openid":"o-1"}}`
	const nsig = "qLi03J0L+Dthy7VoIyBsRmRXYXq/5lTB7InZPULnfhFEE4XgwrciVvSSU2xHZv8u8e2+Kk5BCtdRwKxLP/PnYm3ZMWf3qKieSF17MzcSu2X/heXeL4cQnbU9fd+5zxPnGqQnxaVMabbWnJx+5qhRI1Xmn1obtkf4T/BMdFtNsUkhsC5bKeK20nlOT/eAZvHf9E5EOU8E08N63+4Kipe+XTMdW+rexBFNtTdzn5Cw3rC/2rewVy+j280b208ztMtKKtBzwkIdhKVBuE+PINnP2ZASYujRzHjIjK6R/a0idvX7X2fRgy9EYPnxDvpS2gYKyXTY++jeqMdJ4aEqNDdUng=="
	built, _ := BuildPluginChannel(huifuChannel("wxpay"))
	v := built.(NotificationVerifier)
	form := huifuEncodeForm(map[string]string{"resp_data": nraw, "sign": nsig})
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := v.VerifyNotification(context.Background(), nil, form, exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "HF1" || ev.BuyerID != "o-1" || ev.Amount != 100 {
		t.Fatal(ev, e)
	}
	// bad sign
	badForm := huifuEncodeForm(map[string]string{"resp_data": nraw, "sign": strings.Repeat("A", 344)})
	if _, e = v.VerifyNotification(context.Background(), nil, badForm, exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// wrong amount / trade
	if _, e = v.VerifyNotification(context.Background(), nil, form, ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	if _, e = v.VerifyNotification(context.Background(), nil, form, ExpectedPayment{TradeNo: "X", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
}

// huifuResign re-signs a data object the way the response verifier expects
// (canonical ksort json_encode), using the shared test private key.
func huifuResign(t *testing.T, data map[string]any) string {
	t.Helper()
	k, e := pluginPrivateKey(huifuTestPriv)
	if e != nil {
		t.Fatal(e)
	}
	strs := map[string]string{}
	for key := range data {
		strs[key] = jsonText(data, key)
	}
	// Reuse the plugin canonicalizer for scalar fields.
	sig, e := rsaSign(k, crypto.SHA256, huifuCanonical(strs))
	if e != nil {
		t.Fatal(e)
	}
	return sig
}

func huifuEncodeForm(m map[string]string) []byte {
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+"="+url.QueryEscape(v))
	}
	return []byte(strings.Join(parts, "&"))
}
