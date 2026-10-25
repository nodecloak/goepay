package contract_test

import (
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
	"goepay/internal/identity"
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

func TestAlipayBankCertificateHTTPActualAdapterLateRefund(t *testing.T) {
	s, pool, appKey := fixture(t)
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
	root := &x509.Certificate{SerialNumber: big.NewInt(41), Subject: pkix.Name{CommonName: "SyntheticRoot"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, e := x509.CreateCertificate(rand.Reader, root, root, &rootKey.PublicKey, rootKey)
	if e != nil {
		t.Fatal(e)
	}
	root, e = x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	certs := []string{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
	for i, k := range []*rsa.PrivateKey{appKey, platformKey} {
		leaf := &x509.Certificate{SerialNumber: big.NewInt(int64(i + 2)), Subject: pkix.Name{CommonName: "SyntheticLeaf"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(30 * time.Minute), KeyUsage: x509.KeyUsageDigitalSignature}
		d, e := x509.CreateCertificate(rand.Reader, leaf, root, &k.PublicKey, rootKey)
		if e != nil {
			t.Fatal(e)
		}
		certs = append(certs, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: d})))
	}
	serial := fmt.Sprintf("%x", md5.Sum([]byte("CN=SyntheticRoot3")))
	var writes atomic.Int32
	var zeroFee atomic.Bool
	zeroFee.Store(true)
	var state atomic.Value
	state.Store("SUCCESS")
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		var biz map[string]any
		json.Unmarshal([]byte(r.Form.Get("biz_content")), &biz)
		if biz["product_code"] != "TRANS_BANKCARD_NO_PWD" || biz["biz_scene"] != "DIRECT_TRANSFER" {
			t.Error("wrong bank product", biz)
		}
		out := map[string]any{"code": "10000", "out_biz_no": biz["out_biz_no"], "order_id": "actual-ali-bank-1", "status": state.Load().(string)}
		if r.Form.Get("method") == "alipay.fund.trans.uni.transfer" {
			writes.Add(1)
			if biz["payee_info"].(map[string]any)["identity_type"] != "BANKCARD_ACCOUNT" {
				t.Error("wrong identity")
			}
			out["status"] = "DEALING"
		} else {
			out["order_fee"] = "0.12"
			if zeroFee.Load() {
				out["order_fee"] = "0.00"
			}
			if state.Load().(string) == "SUCCESS" || state.Load().(string) == "WRONG_AMOUNT" {
				out["status"] = "SUCCESS"
				out["trans_amount"] = "1.00"
				if state.Load().(string) == "WRONG_AMOUNT" {
					out["trans_amount"] = "2.00"
				}
				out["pay_date"] = "2026-10-23 12:00:00"
			}
		}
		node, _ := json.Marshal(out)
		digest := sha256.Sum256(node)
		sig, e := rsa.SignPKCS1v15(rand.Reader, platformKey, crypto.SHA256, digest[:])
		if e != nil {
			t.Error(e)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(r.Form.Get("method"), ".", "_") + "_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(sig), "alipay_cert_sn": serial})
	}))
	defer up.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: appKey, CertificateMode: &adapter.AlipayCertificateConfig{ApplicationPEM: certs[1], PlatformPEM: certs[2], RootPEM: certs[0]}, EnableBankTransfer: true, BankTransferScene: "企业退款", BankTransferReports: []adapter.AlipayTransferSceneReport{{InfoType: "退款原因", InfoContent: "本地测试"}}, GatewayURL: up.URL, HTTPClient: up.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: ali}, AllowLive: true}
	s.MerchantChannels = map[int64][]int64{2: {7}}
	if _, e = pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"3"}' WHERE id=2;INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "bank-funds", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "bank-funds"); e != nil {
		t.Fatal(e)
	}
	invoke := func(path string, p compat.Params) map[string]any {
		t.Helper()
		p["pid"] = "2"
		p["sign_type"] = "RSA"
		p["timestamp"] = fmt.Sprint(time.Now().Unix())
		p["sign"], _ = compat.SignRSA(p, appKey)
		v := url.Values{}
		for k, x := range p {
			v.Set(k, x)
		}
		_, out := call(t, s.Handler(), "POST", path, v)
		return out
	}
	first := invoke("/api/transfer/submit", compat.Params{"type": "bank", "channel_id": "7", "out_biz_no": "actual-bank", "account": "6222020000000000", "name": "Synthetic Person", "money": "1.00", "remark": "local test"})
	if first["code"] != float64(0) || first["status"] != float64(0) || first["bank_product"] != adapter.BankProductAlipay {
		t.Fatal(first)
	}
	confirmed := invoke("/api/transfer/query", compat.Params{"out_biz_no": "actual-bank"})
	if confirmed["status"] != float64(1) {
		t.Fatal(confirmed)
	}
	if confirmed["bank_estimated_fee_known"] != true || confirmed["bank_estimated_fee_amount"] != "0.00" {
		t.Fatal("zero estimate lost", confirmed)
	}
	var expense int64
	if e = pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=0 AND kind='bank_fee_expense'").Scan(&expense); e != nil || expense != 0 {
		t.Fatal("zero estimate altered actual provider fee", expense, e)
	}
	zeroFee.Store(false)
	state.Store("WRONG_AMOUNT")
	unverified := invoke("/api/transfer/query", compat.Params{"out_biz_no": "actual-bank"})
	if unverified["status"] != float64(0) || unverified["bank_lifecycle"] != "reversal_pending" {
		t.Fatal("contradictory signed bank query reported final success", unverified)
	}
	state.Store("REFUND")
	returned := invoke("/api/transfer/query", compat.Params{"out_biz_no": "actual-bank"})
	if returned["status"] != float64(0) || returned["bank_lifecycle"] != "reversal_pending" || returned["query_required"] != true || returned["ledger_status"] != "succeeded" {
		t.Fatal("late return misreported success", returned)
	}
	b, _ := s.Store.Balance(ctx, 2)
	if b.Available != 897 || b.Reserved != 0 || writes.Load() != 1 {
		t.Fatal("unproven refund changed ledger or resubmitted", b, writes.Load())
	}
	token, _, e := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/console/v1/admin/transfers/capabilities?merchant_id=2", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	var envelope struct {
		Data struct {
			Channels []map[string]any `json:"channels"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &envelope)
	if w.Code != 200 || len(envelope.Data.Channels) != 1 || envelope.Data.Channels[0]["bank_product"] != adapter.BankProductAlipay || envelope.Data.Channels[0]["bank_code_required"] != false {
		t.Fatal("bank UI cannot distinguish Ali/Wx contract", w.Code, w.Body.String())
	}
}
