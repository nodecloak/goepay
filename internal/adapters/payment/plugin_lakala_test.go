package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// lakalaKeyPEM returns a PKCS#8 private key PEM.
func lakalaKeyPEM(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	der, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// lakalaCertPEM returns a self-signed certificate PEM for the key with the
// given serial.
func lakalaCertPEM(t *testing.T, key *rsa.PrivateKey, serial int64) string {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, e := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func lakalaChannel(t *testing.T, payType, appselect string) (PluginChannel, *rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	mkey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	pkey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	c := PluginChannel{
		ID: 11, Plugin: "lakala", PayType: payType,
		Config: map[string]string{"appid": "APP1", "appmchid": "M1", "appkey": "T1", "appselect": appselect, "appswitch": "0"},
		Certificates: map[string]string{
			"merchant_private_key": lakalaKeyPEM(t, mkey),
			"merchant_cert":        lakalaCertPEM(t, mkey, 123456789),
			"platform_cert":        lakalaCertPEM(t, pkey, 2),
		},
	}
	return c, mkey, pkey
}

func TestPluginLakalaRegistryRules(t *testing.T) {
	spec, ok := LookupPlugin("lakala")
	if !ok || spec.Title != "拉卡拉" || !spec.Serves("alipay") || spec.Serves("qqpay") {
		t.Fatal("lakala spec", spec, ok)
	}
	c, _, _ := lakalaChannel(t, "alipay", "0")
	if _, e := BuildPluginChannel(c); e != nil {
		t.Fatal(e)
	}
	// Missing required merchant key.
	miss, _, _ := lakalaChannel(t, "alipay", "0")
	delete(miss.Certificates, "merchant_private_key")
	if _, e := BuildPluginChannel(miss); e == nil {
		t.Fatal("accepted missing key")
	}
	// Unserved pay type.
	bad, _, _ := lakalaChannel(t, "qqpay", "0")
	if _, e := BuildPluginChannel(bad); e == nil {
		t.Fatal("accepted unserved type")
	}
}

func TestPluginLakalaSerialAndAuth(t *testing.T) {
	c, mkey, pkey := lakalaChannel(t, "alipay", "0")
	p, e := BuildPluginChannel(c)
	if e != nil {
		t.Fatal(e)
	}
	lp := p.(*lakalaPlugin)
	// Serial from the merchant cert matches the decimal big.Int value.
	if lp.serialNo != "123456789" {
		t.Fatal("serial", lp.serialNo)
	}
	// authorization signs appid\nserial\nts\nnonce\nbody\n with the merchant
	// key; verify independently with the merchant public key.
	body := []byte(`{"x":1}`)
	auth, e := lp.authorization(body)
	if e != nil {
		t.Fatal(e)
	}
	// Parse out fields and rebuild the signed message.
	fields := map[string]string{}
	rest := strings.TrimPrefix(auth, lakalaSchema+" ")
	for _, part := range strings.Split(rest, ",") {
		kv := strings.SplitN(part, "=", 2)
		fields[strings.TrimSpace(kv[0])] = strings.Trim(kv[1], `"`)
	}
	msg := lp.appid + "\n" + lp.serialNo + "\n" + fields["timestamp"] + "\n" + fields["nonce_str"] + "\n" + string(body) + "\n"
	if !rsaVerify(&mkey.PublicKey, crypto.SHA256, msg, fields["signature"]) {
		t.Fatal("auth signature not verifiable with merchant key")
	}
	_ = pkey
}

func TestPluginLakalaCreateScan(t *testing.T) {
	c, _, _ := lakalaChannel(t, "alipay", "0")
	var reply string
	var status int
	var gotBody map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(c)
	lp := p.(*lakalaPlugin)
	lp.gateway = srv.URL

	if got := lp.Capabilities().Methods; len(got) != 2 {
		t.Fatal(got)
	}
	status, reply = 200, `{"code":"BBS00000","resp_data":{"trade_no":"U1","acc_resp_fields":{"code":"https://qr.example/x"}}}`
	r := CreateRequest{TradeNo: "T1", Subject: "商品", Method: "native", NotifyURL: "https://pay.example/n", ClientIP: "1.2.3.4", Amount: 1000}
	res, e := lp.Create(context.Background(), r)
	if e != nil || res.MaterialType != "qrcode" || res.Material != "https://qr.example/x" || res.ProviderTradeNo != "U1" {
		t.Fatal(res, e)
	}
	// Request envelope: version 3.0, req_data carries merchant/term/amount(fen).
	if gotBody["version"] != "3.0" {
		t.Fatal("version", gotBody)
	}
	rd, _ := gotBody["req_data"].(map[string]any)
	if rd["merchant_no"] != "M1" || rd["term_no"] != "T1" || rd["total_amount"] != "1000" || rd["account_type"] != "ALIPAY" || rd["trans_type"] != "41" {
		t.Fatal("req_data", rd)
	}
	if !strings.HasPrefix(gotAuth, lakalaSchema) {
		t.Fatal("auth header", gotAuth)
	}

	// jsapi needs openid; produces jsapi material.
	status, reply = 200, `{"code":"BBS00000","resp_data":{"trade_no":"U2","acc_resp_fields":{"app_id":"wx","package":"p"}}}`
	res, e = lp.Create(context.Background(), CreateRequest{TradeNo: "T2", Subject: "x", Method: "jsapi", OpenID: "o-1", NotifyURL: "https://pay.example/n", Amount: 50})
	if e != nil || res.MaterialType != "jsapi" || !strings.Contains(res.Material, `"app_id":"wx"`) {
		t.Fatal(res, e)
	}
	rd, _ = gotBody["req_data"].(map[string]any)
	if rd["trans_type"] != "51" {
		t.Fatal("jsapi trans_type", rd)
	}

	// business refusal.
	status, reply = 200, `{"code":"BBS99999","msg":"x"}`
	var pe *ProviderError
	if _, e = lp.Create(context.Background(), r); !errors.As(e, &pe) {
		t.Fatal("business refusal", e)
	}
	// unknown outcome.
	status, reply = 502, "bad"
	var unknown *UnknownOutcome
	if _, e = lp.Create(context.Background(), r); !errors.As(e, &unknown) {
		t.Fatal("unknown outcome", e)
	}
}

func TestPluginLakalaCashier(t *testing.T) {
	c, _, _ := lakalaChannel(t, "wxpay", "1")
	var reply string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(c)
	lp := p.(*lakalaPlugin)
	lp.gateway = srv.URL
	if got := lp.Capabilities().Methods; len(got) != 1 || got[0] != "h5" {
		t.Fatal(got)
	}
	reply = `{"code":"000000","resp_data":{"pay_order_no":"P1","counter_url":"https://cashier.example/x"}}`
	res, e := lp.Create(context.Background(), CreateRequest{TradeNo: "T1", Subject: "x", Method: "h5", NotifyURL: "https://pay.example/n", ReturnURL: "https://pay.example/r", Amount: 100})
	if e != nil || res.MaterialType != "jump" || res.Material != "https://cashier.example/x" || res.ProviderTradeNo != "P1" {
		t.Fatal(res, e)
	}
}

func TestPluginLakalaQueryRefund(t *testing.T) {
	c, _, _ := lakalaChannel(t, "alipay", "0")
	var reply string
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	p, _ := BuildPluginChannel(c)
	lp := p.(*lakalaPlugin)
	lp.gateway = srv.URL

	reply = `{"code":"BBS00000","resp_data":{"out_trade_no":"T9","trade_no":"U9","trade_state":"SUCCESS","total_amount":"990","user_id2":"o-x"}}`
	q, e := lp.Query(context.Background(), QueryRequest{TradeNo: "T9"})
	if e != nil || q.State != "SUCCESS" || q.Amount != 990 || q.ProviderTradeNo != "U9" || path != "/api/v3/labs/query/tradequery" {
		t.Fatal(q, e, path)
	}
	reply = `{"code":"BBS00000","resp_data":{"out_trade_no":"OTHER","trade_state":"SUCCESS","total_amount":"990"}}`
	if _, e = lp.Query(context.Background(), QueryRequest{TradeNo: "T9"}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("foreign order", e)
	}
	reply = `{"code":"BBS00000","resp_data":{"trade_no":"R9","refund_amount":"500"}}`
	rr, e := lp.Refund(context.Background(), RefundRequest{TradeNo: "T9", ProviderTradeNo: "U9", RefundNo: "RF9", Amount: 500, TotalAmount: 990})
	if e != nil || rr.State != "SUCCESS" || rr.Amount != 500 || path != "/api/v3/labs/relation/refund" {
		t.Fatal(rr, e, path)
	}
}

func TestPluginLakalaNotification(t *testing.T) {
	c, _, pkey := lakalaChannel(t, "alipay", "0")
	p, _ := BuildPluginChannel(c)
	lp := p.(*lakalaPlugin)
	// Re-point the platform key to the one we hold so we can sign callbacks.
	lp.platformKey = &pkey.PublicKey

	body := []byte(`{"out_trade_no":"T1","trade_no":"U1","trade_status":"SUCCESS","total_amount":"100","user_id2":"o-x"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "abcd"
	msg := ts + "\n" + nonce + "\n" + string(body) + "\n"
	sig, e := rsaSign(pkey, crypto.SHA256, msg)
	if e != nil {
		t.Fatal(e)
	}
	header := http.Header{}
	header.Set("Authorization", lakalaSchema+` timestamp="`+ts+`",nonce_str="`+nonce+`",signature="`+sig+`"`)
	exp := ExpectedPayment{TradeNo: "T1", Amount: 100}
	ev, e := lp.VerifyNotification(context.Background(), header, body, exp)
	if e != nil || ev.State != "SUCCESS" || ev.ProviderTradeNo != "U1" || ev.Amount != 100 {
		t.Fatal(ev, e)
	}
	// bad signature
	badHeader := http.Header{}
	badHeader.Set("Authorization", lakalaSchema+` timestamp="`+ts+`",nonce_str="`+nonce+`",signature="AAAA"`)
	if _, e = lp.VerifyNotification(context.Background(), badHeader, body, exp); !errors.Is(e, ErrAuthentication) {
		t.Fatal("bad sign", e)
	}
	// wrong amount
	if _, e = lp.VerifyNotification(context.Background(), header, body, ExpectedPayment{TradeNo: "T1", Amount: 200}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("amount", e)
	}
	// wrong trade number
	if _, e = lp.VerifyNotification(context.Background(), header, body, ExpectedPayment{TradeNo: "OTHER", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("trade", e)
	}
	// non-success status
	nsBody := []byte(`{"out_trade_no":"T1","trade_status":"FAIL","total_amount":"100"}`)
	nsMsg := ts + "\n" + nonce + "\n" + string(nsBody) + "\n"
	nsSig, _ := rsaSign(pkey, crypto.SHA256, nsMsg)
	nsHeader := http.Header{}
	nsHeader.Set("Authorization", lakalaSchema+` timestamp="`+ts+`",nonce_str="`+nonce+`",signature="`+nsSig+`"`)
	if _, e = lp.VerifyNotification(context.Background(), nsHeader, nsBody, exp); !errors.Is(e, ErrInvalid) {
		t.Fatal("status", e)
	}
	// ack is the default success/fail
	if status, _, body := lp.NotificationAck(nil); status != 200 || body != "success" {
		t.Fatal(status, body)
	}
}
