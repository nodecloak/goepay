package contract_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAlipayCertificateHTTPMaterialAndVerifiedSettlement(t *testing.T) {
	s, pool, merchantKey := fixture(t)
	ctx := context.Background()
	rootKey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	platformKey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	root := &x509.Certificate{SerialNumber: big.NewInt(42), Subject: pkix.Name{CommonName: "SyntheticRoot"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	rootDER, e := x509.CreateCertificate(rand.Reader, root, root, &rootKey.PublicKey, rootKey)
	if e != nil {
		t.Fatal(e)
	}
	root, e = x509.ParseCertificate(rootDER)
	if e != nil {
		t.Fatal(e)
	}
	certs := []string{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}))}
	for i, key := range []*rsa.PrivateKey{merchantKey, platformKey} {
		leaf := &x509.Certificate{SerialNumber: big.NewInt(int64(i + 2)), Subject: pkix.Name{CommonName: "SyntheticLeaf"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(30 * time.Minute), KeyUsage: x509.KeyUsageDigitalSignature}
		der, e := x509.CreateCertificate(rand.Reader, leaf, root, &key.PublicKey, rootKey)
		if e != nil {
			t.Fatal(e)
		}
		certs = append(certs, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
	}
	sn := fmt.Sprintf("%x", md5.Sum([]byte("CN=SyntheticRoot3")))
	var trusted atomic.Bool
	var calls atomic.Int32
	sign := func(message []byte) string {
		digest := sha256.Sum256(message)
		sig, e := rsa.SignPKCS1v15(rand.Reader, platformKey, crypto.SHA256, digest[:])
		if e != nil {
			t.Fatal(e)
		}
		return base64.StdEncoding.EncodeToString(sig)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		r.ParseForm()
		if r.Form.Get("method") != "alipay.trade.query" || r.Form.Get("app_cert_sn") == "" {
			t.Error("bad certificate request")
		}
		var biz map[string]string
		json.Unmarshal([]byte(r.Form.Get("biz_content")), &biz)
		node, _ := json.Marshal(map[string]string{"code": "10000", "out_trade_no": biz["out_trade_no"], "trade_no": "cert-" + biz["out_trade_no"], "total_amount": "1.00", "trade_status": "TRADE_SUCCESS"})
		serial := sn
		if !trusted.Load() {
			serial = strings.Repeat("0", 32)
		}
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_query_response": json.RawMessage(node), "sign": sign(node), "alipay_cert_sn": serial})
	}))
	defer upstream.Close()
	clock := atomic.Int64{}
	clock.Store(now.Unix())
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: merchantKey, CertificateMode: &adapter.AlipayCertificateConfig{ApplicationPEM: certs[1], PlatformPEM: certs[2], RootPEM: certs[0]}, Methods: []string{"app"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client(), Now: func() time.Time { return time.Unix(clock.Load(), 0) }})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{9: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{9}, 32)
	s.MerchantChannels = map[int64][]int64{2: {9}}
	create := func(no string) map[string]any {
		p := compat.Params{"pid": "2", "type": "alipay", "method": "app", "channel_id": "9", "out_trade_no": no, "name": "test", "money": "1.00", "timestamp": fmt.Sprint(time.Now().Unix()), "sign_type": "RSA"}
		p["sign"], _ = compat.SignRSA(p, merchantKey)
		values := url.Values{}
		for k, v := range p {
			values.Set(k, v)
		}
		_, out := call(t, s.Handler(), "POST", "/api/pay/create", values)
		return out
	}
	first := create("certificate-app")
	if first["code"] != float64(0) || first["pay_type"] != "app" || calls.Load() != 0 {
		t.Fatal("offline material called upstream", first, calls.Load())
	}
	material, e := url.ParseQuery(first["pay_info"].(string))
	if e != nil || material.Get("app_cert_sn") == "" || material.Get("alipay_root_cert_sn") == "" {
		t.Fatal("certificate SDK fields missing", e)
	}
	trade := first["trade_no"].(string)
	if _, e = s.ProviderFlow.Query(ctx, 2, trade); e == nil {
		t.Fatal("unknown serial settled payment")
	}
	b, _ := s.Store.Balance(ctx, 2)
	if b.Available != 0 {
		t.Fatal("unknown certificate credited", b)
	}
	trusted.Store(true)
	for i := 0; i < 3; i++ {
		if _, e = s.ProviderFlow.Query(ctx, 2, trade); e != nil {
			t.Fatal(e)
		}
	}
	b, _ = s.Store.Balance(ctx, 2)
	if b.Available != 100 {
		t.Fatal("certificate settlement not idempotent", b)
	}
	clock.Store(now.Add(2 * time.Hour).Unix())
	if out := create("expired-cert"); out["code"] != float64(-1) {
		t.Fatal("expired certificate accepted", out)
	}
	var attempts int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE o.out_trade_no='expired-cert'").Scan(&attempts); e != nil || attempts != 0 {
		t.Fatal("expired certificate allocated external intent", attempts, e)
	}
	if calls.Load() != 4 {
		t.Fatal("expired certificate called upstream", calls.Load())
	}
}
