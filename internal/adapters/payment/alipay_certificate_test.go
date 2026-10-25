package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func alipayCertificateFixture(t *testing.T) (AlipayConfig, *rsa.PrivateKey, *x509.Certificate, *x509.Certificate) {
	t.Helper()
	now := time.Now()
	rootKey, appKey, platformKey := testKey(t), testKey(t), testKey(t)
	root := &x509.Certificate{SerialNumber: big.NewInt(42), Subject: pkix.Name{Country: []string{"CN"}, Organization: []string{"Synthetic CA"}, OrganizationalUnit: []string{"Test Only"}, CommonName: "Synthetic Root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	rootDER, e := x509.CreateCertificate(rand.Reader, root, root, &rootKey.PublicKey, rootKey)
	if e != nil {
		t.Fatal(e)
	}
	root, e = x509.ParseCertificate(rootDER)
	if e != nil {
		t.Fatal(e)
	}
	var leaves []*x509.Certificate
	var pems []string
	for i, key := range []*rsa.PrivateKey{appKey, platformKey} {
		serial := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 80), big.NewInt(int64(99+i)))
		leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: []string{"Synthetic App", "Synthetic Platform"}[i]}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(30 * time.Minute), KeyUsage: x509.KeyUsageDigitalSignature}
		der, e := x509.CreateCertificate(rand.Reader, leaf, root, &key.PublicKey, rootKey)
		if e != nil {
			t.Fatal(e)
		}
		cert, e := x509.ParseCertificate(der)
		if e != nil {
			t.Fatal(e)
		}
		leaves = append(leaves, cert)
		pems = append(pems, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
	}
	return AlipayConfig{AppID: "app", PrivateKey: appKey, CertificateMode: &AlipayCertificateConfig{ApplicationPEM: pems[0], PlatformPEM: pems[1], RootPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}))}, Now: func() time.Time { return now }}, platformKey, leaves[1], root
}

func TestAlipayCertificateModePinsResponseAndNotificationKey(t *testing.T) {
	cfg, platformKey, platformCert, _ := alipayCertificateFixture(t)
	sn, _ := alipayCertSN(platformCert)
	var mode atomic.Value
	mode.Store("valid")
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		r.ParseForm()
		p := map[string]string{}
		for k, v := range r.PostForm {
			p[k] = v[0]
		}
		wantSign, _ := SignAlipayRSA2(p, cfg.PrivateKey)
		if p["sign"] != wantSign || p["app_cert_sn"] == "" || p["alipay_root_cert_sn"] == "" {
			t.Error("certificate request fields absent from signature")
		}
		node := []byte(`{"code":"10000","out_trade_no":"42","trade_no":"ali42","trade_status":"TRADE_SUCCESS","total_amount":"1.00"}`)
		sign, _ := signRSA(node, platformKey)
		serial := sn
		switch mode.Load().(string) {
		case "unknown":
			serial = strings.Repeat("0", 32)
		case "missing":
			serial = ""
		case "tamper":
			node = []byte(strings.ReplaceAll(string(node), "ali42", "ali99"))
		}
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_query_response": json.RawMessage(node), "sign": sign, "alipay_cert_sn": serial})
	}))
	defer upstream.Close()
	cfg.GatewayURL = upstream.URL
	cfg.HTTPClient = upstream.Client()
	a, e := NewAlipay(cfg)
	if e != nil {
		t.Fatal(e)
	}
	r := QueryRequest{TradeNo: "42"}
	if out, e := a.Query(context.Background(), r); e != nil || out.ProviderTradeNo != "ali42" || out.State != "TRADE_SUCCESS" {
		t.Fatal(out, e)
	}
	for _, bad := range []string{"unknown", "missing", "tamper"} {
		mode.Store(bad)
		if _, e := a.Query(context.Background(), r); !errors.Is(e, ErrAuthentication) {
			t.Fatal("untrusted response accepted", bad, e)
		}
	}
	p := map[string]string{"app_id": "app", "out_trade_no": "42", "trade_no": "ali42", "total_amount": "1.00", "trade_status": "TRADE_SUCCESS", "sign_type": "RSA2"}
	verify := func() error {
		p["sign"], _ = signRSA([]byte(alipayCanonical(p, true)), platformKey)
		values := url.Values{}
		for k, v := range p {
			values.Set(k, v)
		}
		_, e := a.VerifyNotification(context.Background(), nil, []byte(values.Encode()), ExpectedPayment{TradeNo: "42", Amount: 100})
		return e
	}
	if e := verify(); e != nil {
		t.Fatal("pinned legacy notification without serial rejected", e)
	}
	p["alipay_cert_sn"] = sn
	if e := verify(); e != nil {
		t.Fatal(e)
	}
	p["alipay_cert_sn"] = strings.Repeat("0", 32)
	if e := verify(); !errors.Is(e, ErrAuthentication) {
		t.Fatal("notification unknown serial accepted", e)
	}
	// Revalidate pinned certificates after startup, including key material
	// embedded into offline APP/page/preauthorization launch parameters.
	cfg.Now = func() time.Time { return platformCert.NotAfter.Add(time.Second) }
	expired, e := NewAlipay(cfg)
	if expired != nil || e == nil {
		t.Fatal("expired configuration accepted")
	}
	a.config.Now = cfg.Now
	if e = a.ValidateCreate(CreateRequest{TradeNo: "42", Subject: "test", Method: "page", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("expired certificate passed order preflight", e)
	}
	if e = a.ValidateTransfer(TransferRequest{TransferNo: "TF0", Type: "alipay", Account: "synthetic", Amount: 100}); !errors.Is(e, ErrAuthentication) {
		t.Fatal("expired certificate passed funds preflight", e)
	}
	if _, e = a.Query(context.Background(), r); !errors.Is(e, ErrAuthentication) || calls.Load() != 4 {
		t.Fatal("expired app called upstream", e, calls.Load())
	}
}

func TestAlipayCertificateConfigurationRejectsKeyMismatchAndUntrustedChain(t *testing.T) {
	cfg, _, _, _ := alipayCertificateFixture(t)
	wrong := testKey(t)
	bad := cfg
	bad.PrivateKey = wrong
	if _, e := NewAlipay(bad); e == nil {
		t.Fatal("application certificate mismatch accepted")
	}
	bad = cfg
	bad.PublicKey = &wrong.PublicKey
	if _, e := NewAlipay(bad); e == nil {
		t.Fatal("public-key fallback accepted")
	}
	other, _, _, _ := alipayCertificateFixture(t)
	for _, mutate := range []func(*AlipayCertificateConfig){func(c *AlipayCertificateConfig) { c.RootPEM = other.CertificateMode.RootPEM }, func(c *AlipayCertificateConfig) { c.PlatformPEM += "garbage" }, func(c *AlipayCertificateConfig) { c.ApplicationPEM = "" }, func(c *AlipayCertificateConfig) { c.RootPEM = "https://untrusted.invalid/root.pem" }} {
		copy := *cfg.CertificateMode
		bad = cfg
		bad.CertificateMode = &copy
		mutate(&copy)
		if _, e := NewAlipay(bad); e == nil {
			t.Fatal("invalid certificate trust accepted")
		}
	}
}

func TestAlipayCertificateTransferUsesUniTransferAndQuery(t *testing.T) {
	cfg, platformKey, cert, _ := alipayCertificateFixture(t)
	sn, _ := alipayCertSN(cert)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		var biz map[string]any
		json.Unmarshal([]byte(r.PostForm.Get("biz_content")), &biz)
		method := r.PostForm.Get("method")
		if method == "alipay.fund.trans.uni.transfer" {
			payee, ok := biz["payee_info"].(map[string]any)
			if !ok || payee["identity_type"] != "ALIPAY_OPEN_ID" || payee["identity"] != "synthetic-open-id" || biz["trans_amount"] != "1.00" || biz["product_code"] != "TRANS_ACCOUNT_NO_PWD" || biz["biz_scene"] != "DIRECT_TRANSFER" || biz["amount"] != nil {
				t.Error("wrong certificate transfer fields", biz)
			}
		} else if method != "alipay.fund.trans.common.query" {
			t.Error("certificate used key-mode transfer endpoint", method)
		}
		node := []byte(`{"code":"10000","out_biz_no":"TF42","order_id":"transfer42","trans_amount":"1.00","status":"SUCCESS","trans_date":"2026-10-23 12:00:00","pay_date":"2026-10-23 12:00:00"}`)
		sign, _ := signRSA(node, platformKey)
		json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(method, ".", "_") + "_response": json.RawMessage(node), "sign": sign, "alipay_cert_sn": sn})
	}))
	defer upstream.Close()
	cfg.HTTPClient = upstream.Client()
	cfg.GatewayURL = upstream.URL
	a, e := NewAlipay(cfg)
	if e != nil {
		t.Fatal(e)
	}
	r := TransferRequest{Type: "alipay", TransferNo: "TF42", Account: "synthetic-open-id", Name: "Test Person", Title: "Test", Amount: 100}
	if out, e := a.Transfer(context.Background(), r); e != nil || out.State != "SUCCESS" || out.ProviderTransferNo != "transfer42" {
		t.Fatal(out, e)
	}
	if out, e := a.QueryTransfer(context.Background(), r); e != nil || out.Amount != 100 {
		t.Fatal(out, e)
	}
}

func TestAlipayCertificateBalanceAndProofUsePinnedResponses(t *testing.T) {
	cfg, key, cert, _ := alipayCertificateFixture(t)
	sn, _ := alipayCertSN(cert)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		method := r.PostForm.Get("method")
		var biz map[string]any
		json.Unmarshal([]byte(r.PostForm.Get("biz_content")), &biz)
		out := map[string]any{"code": "10000"}
		switch method {
		case "alipay.fund.account.query":
			out["available_amount"] = "12.34"
			out["freeze_amount"] = "0.00"
		case "alipay.fund.quota.query":
			if biz["product_code"] != "DEFAULT" || biz["biz_scene"] != "DEFAULT" {
				t.Error("certificate quota did not follow original plugin", biz)
			}
			out["new_quota_single_max"] = "100.00"
		case "alipay.data.bill.ereceipt.apply":
			out["file_id"] = "file42"
		case "alipay.data.bill.ereceipt.query":
			out["file_id"] = "file42"
			out["status"] = "SUCCESS"
			out["download_url"] = "https://receipt.example.invalid/file42"
		default:
			t.Error(method)
		}
		node, _ := json.Marshal(out)
		sign, _ := signRSA(node, key)
		json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(method, ".", "_") + "_response": json.RawMessage(node), "sign": sign, "alipay_cert_sn": sn})
	}))
	defer upstream.Close()
	cfg.GatewayURL = upstream.URL
	cfg.HTTPClient = upstream.Client()
	a, e := NewAlipay(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if b, e := a.ChannelBalance(context.Background(), "2088000000000000"); e != nil || b.Available != 1234 {
		t.Fatal(b, e)
	}
	p, e := a.ApplyTransferProof(context.Background(), "transfer42")
	if e != nil {
		t.Fatal(e)
	}
	if p, e = a.QueryTransferProof(context.Background(), p.FileID); e != nil || p.State != "SUCCESS" {
		t.Fatal(p, e)
	}
}
