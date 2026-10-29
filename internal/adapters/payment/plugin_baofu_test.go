package payment

import (
	"context"
	"crypto"
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// baofuTestPFX is a 2048-bit PKCS#12 generated for these tests, password
// "pfxpwd" (merchant private key; not a real 宝付 credential).
const baofuTestPFX = "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"

const baofuPFXPassword = "pfxpwd"

// baofuTestServer swaps the embedded platform cert for a test key pair so the
// test can both verify (plugin side) and sign (server/callback side) replies.
func baofuBuild(t *testing.T, payType string, apps ...string) *baofuPlugin {
	t.Helper()
	c := PluginChannel{ID: 10, Plugin: "baofu", PayType: payType, AppTypes: apps,
		Config:       map[string]string{"appid": "MER1", "appurl": "TER1", "appkey": baofuPFXPassword, "appmchid": "SUB1"},
		Certificates: map[string]string{"pfx": baofuTestPFX}}
	p, e := BuildPluginChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	bp := p.(*baofuPlugin)
	// Replace the platform key with the shared test platform key pair so the
	// test harness can produce valid platform signatures.
	bp.platform = &easypayPlatformPriv(t).PublicKey
	return bp
}

// baofuPlatformSignHex signs data with the test platform private key, hex.
func baofuPlatformSignHex(t *testing.T, data string) string {
	t.Helper()
	priv := easypayPlatformPriv(t)
	h := crypto.SHA256.New()
	h.Write([]byte(data))
	sig, e := rsa.SignPKCS1v15(nil, priv, crypto.SHA256, h.Sum(nil))
	if e != nil {
		t.Fatal(e)
	}
	return hex.EncodeToString(sig)
}

func TestPluginBaofuRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("baofu")
	if !ok || spec.Title != "宝付支付" || !spec.Serves("bank") || spec.Serves("qqpay") {
		t.Fatal("baofu spec", ok)
	}
	ok2 := PluginChannel{ID: 10, Plugin: "baofu", PayType: "alipay",
		Config:       map[string]string{"appid": "MER1", "appurl": "TER1", "appkey": baofuPFXPassword},
		Certificates: map[string]string{"pfx": baofuTestPFX}}
	if _, e := BuildPluginChannel(ok2); e != nil {
		t.Fatal(e)
	}
	bad := []PluginChannel{}
	// unserved pay type
	np := ok2
	np.PayType = "qqpay"
	bad = append(bad, np)
	// missing pfx
	noPfx := PluginChannel{ID: 10, Plugin: "baofu", PayType: "alipay", Config: map[string]string{"appid": "MER1", "appurl": "TER1", "appkey": baofuPFXPassword}}
	bad = append(bad, noPfx)
	// wrong PFX password
	wrongPwd := PluginChannel{ID: 10, Plugin: "baofu", PayType: "alipay",
		Config:       map[string]string{"appid": "MER1", "appurl": "TER1", "appkey": "wrong"},
		Certificates: map[string]string{"pfx": baofuTestPFX}}
	bad = append(bad, wrongPwd)
	for i, c := range bad {
		if _, e := BuildPluginChannel(c); e == nil {
			t.Fatal("accepted bad channel", i)
		}
	}
}

func TestPluginBaofuSignRoundTrip(t *testing.T) {
	bp := baofuBuild(t, "alipay")
	biz := `{"merId":"MER1","outTradeNo":"T1"}`
	sig, e := bp.sign(biz)
	if e != nil {
		t.Fatal(e)
	}
	// Verify with the merchant public key that the hex signature is valid.
	raw, _ := hex.DecodeString(sig)
	h := crypto.SHA256.New()
	h.Write([]byte(biz))
	if rsa.VerifyPKCS1v15(&bp.priv.PublicKey, crypto.SHA256, h.Sum(nil), raw) != nil {
		t.Fatal("merchant signature invalid")
	}
	// bp.verify validates a platform signature.
	data := `{"txnState":"SUCCESS"}`
	if !bp.verify(data, baofuPlatformSignHex(t, data)) {
		t.Fatal("platform verify failed")
	}
	if bp.verify(data, baofuPlatformSignHex(t, "other")) {
		t.Fatal("platform verify accepted wrong signature")
	}
}

func TestPluginBaofuCreateNativeAlipay(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(b))
		dataContent := `{"qr_code":"https://qr.baofu/U1","trade_no":"B9"}`
		out := map[string]any{"returnCode": "SUCCESS", "dataContent": dataContent, "signStr": baofuPlatformSignHex(t, dataContent)}
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	bp := baofuBuild(t, "alipay")
	bp.gateway = srv.URL
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 123, ClientIP: "1.2.3.4"}
	res, e := bp.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.baofu/U1" || res.ProviderTradeNo != "B9" {
		t.Fatal(res, e)
	}
	// request assertions: form carries bizContent + signStr + merId.
	if gotForm.Get("merId") != "MER1" || gotForm.Get("method") != "unified_order" || gotForm.Get("signStr") == "" {
		t.Fatal("request", gotForm)
	}
	var biz map[string]any
	json.Unmarshal([]byte(gotForm.Get("bizContent")), &biz)
	if biz["payCode"] != "ALIPAY_NATIVE" || biz["outTradeNo"] != "T1" {
		t.Fatal("bizContent", biz)
	}
}

func TestPluginBaofuCreateOutcomes(t *testing.T) {
	var scenario int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch scenario {
		case 0: // explicit business refusal
			json.NewEncoder(w).Encode(map[string]any{"returnCode": "FAIL", "returnMsg": "x"})
		case 1: // bad response signature -> unknown
			dc := `{"qr_code":"https://q/x"}`
			json.NewEncoder(w).Encode(map[string]any{"returnCode": "SUCCESS", "dataContent": dc, "signStr": "00"})
		case 2:
			io.WriteString(w, "not json")
		}
	}))
	defer srv.Close()
	bp := baofuBuild(t, "alipay")
	bp.gateway = srv.URL
	r := CreateRequest{TradeNo: "T1", Subject: "VIP", Method: "native", NotifyURL: "https://pay.example/n", Amount: 123}
	scenario = 0
	var pe *ProviderError
	if _, e := bp.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business refusal must be definitive", e)
	}
	var unknown *UnknownOutcome
	for _, sc := range []int{1, 2} {
		scenario = sc
		if _, e := bp.Create(context.Background(), r); !errors.As(e, &unknown) {
			t.Fatal("must be unknown", sc, e)
		}
	}
}

func TestPluginBaofuNotification(t *testing.T) {
	bp := baofuBuild(t, "alipay")
	v := Provider(bp).(NotificationVerifier)
	dataContent := `{"txnState":"SUCCESS","outTradeNo":"T1","txnAmt":"123","payCode":"ALIPAY_NATIVE","tradeNo":"B9","chlRetParam":{"buyer_id":"2088xx","trade_no":"A9"}}`
	form := func(dc, sign string) []byte {
		q := url.Values{"dataContent": {dc}, "signStr": {sign}}
		return []byte(q.Encode())
	}
	exp := ExpectedPayment{TradeNo: "T1", Amount: 123}
	good := form(dataContent, baofuPlatformSignHex(t, dataContent))
	ev, e := v.VerifyNotification(context.Background(), nil, good, exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "A9" || ev.Amount != 123 || ev.BuyerID != "2088xx" {
		t.Fatal(ev, e)
	}
	// bad signature
	if _, e := v.VerifyNotification(context.Background(), nil, form(dataContent, "00"), exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign accepted")
	}
	// wrong amount
	if _, e := v.VerifyNotification(context.Background(), nil, good, ExpectedPayment{TradeNo: "T1", Amount: 999}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong amount accepted")
	}
	// wrong trade
	if _, e := v.VerifyNotification(context.Background(), nil, good, ExpectedPayment{TradeNo: "OTHER", Amount: 123}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("wrong trade accepted")
	}
	// non-success
	bad := `{"txnState":"FAIL","outTradeNo":"T1","txnAmt":"123"}`
	if _, e := v.VerifyNotification(context.Background(), nil, form(bad, baofuPlatformSignHex(t, bad)), exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("non-success accepted", e)
	}
	if s, _, b := bp.NotificationAck(nil); s != 200 || b != "OK" {
		t.Fatal(s, b)
	}
	if s, _, b := bp.NotificationAck(ErrInvalid); s != 200 || b != "FAIL" {
		t.Fatal(s, b)
	}
}

func TestPluginBaofuRefund(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dc := `{"tradeNo":"BR9","refundAmt":"100"}`
		json.NewEncoder(w).Encode(map[string]any{"returnCode": "SUCCESS", "dataContent": dc, "signStr": baofuPlatformSignHex(t, dc)})
	}))
	defer srv.Close()
	bp := baofuBuild(t, "alipay")
	bp.gateway = srv.URL
	rr, e := bp.Refund(context.Background(), RefundRequest{TradeNo: "T1", ProviderTradeNo: "B9", RefundNo: "R1", Amount: 100, TotalAmount: 123})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 100 || rr.ProviderRefundNo != "BR9" {
		t.Fatal(rr, e)
	}
}

func TestPluginBaofuMethods(t *testing.T) {
	// JS-only app type yields jsapi only; wxpay maps to WECHAT_JSAPI.
	bp := baofuBuild(t, "wxpay", "2")
	if m := bp.methods(); len(m) != 1 || m[0] != "jsapi" {
		t.Fatal(m)
	}
	_ = strings.TrimSpace("")
}
