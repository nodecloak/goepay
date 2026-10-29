package server

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type parent180TLSFactory struct {
	*channelconfig.ParentChannelFactory
	endpoint      string
	roots         *x509.CertPool
	localProvider func(channelconfig.Channel) (adapter.Provider, error)
}

func (f *parent180TLSFactory) local(raw []byte) (adapter.ParentChannelExecution, error) {
	var source struct {
		Channel channelconfig.Channel `json:"channel"`
	}
	if !strings.HasPrefix(f.endpoint, "https://127.0.0.1:") || json.Unmarshal(raw, &source) != nil {
		return adapter.ParentChannelExecution{}, adapter.ErrInvalid
	}
	c := source.Channel
	if f.localProvider != nil {
		p, e := f.localProvider(c)
		if e != nil {
			return adapter.ParentChannelExecution{}, e
		}
		built, e := f.ParentChannelFactory.Restore(c.ID, raw)
		if e != nil {
			return adapter.ParentChannelExecution{}, e
		}
		built.Payment = p
		return built, nil
	}
	pair, e := tls.X509KeyPair([]byte(c.Certificates["sslcert_path"]), []byte(c.Certificates["sslkey_path"]))
	if e != nil {
		return adapter.ParentChannelExecution{}, e
	}
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: f.roots, Certificates: []tls.Certificate{pair}}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	methods := []string{"native"}
	if c.Config["enable_scan_pay"] == "true" {
		methods = append(methods, "scan")
	}
	var bankKey *rsa.PublicKey
	if c.Config["enable_bank_transfer"] == "true" {
		bankKey, e = compat.ParsePublicKey(c.Config["bank_public_key"])
		if e != nil {
			return adapter.ParentChannelExecution{}, e
		}
	}
	p, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: c.Config["appid"], MerchantID: c.Config["appmchid"], APIKey: c.Config["appkey"], Methods: methods, BaseURL: f.endpoint, HTTPClient: client, RefundClient: client, EnableChannelSharing: c.Config["enable_channel_sharing"] == "true", BankPublicKey: bankKey})
	if e != nil {
		return adapter.ParentChannelExecution{}, e
	}
	built, e := f.ParentChannelFactory.Restore(c.ID, raw)
	if e != nil {
		return adapter.ParentChannelExecution{}, e
	}
	built.Payment = p
	return built, nil
}
func (f *parent180TLSFactory) BootstrapSnapshot(id int64) (adapter.ParentChannelExecution, []byte, error) {
	_, raw, e := f.ParentChannelFactory.BootstrapSnapshot(id)
	if e != nil {
		return adapter.ParentChannelExecution{}, nil, e
	}
	out, e := f.local(raw)
	return out, raw, e
}
func (f *parent180TLSFactory) NewSnapshot(id int64, in adapter.ParentChannelAccountChange) (adapter.ParentChannelExecution, []byte, error) {
	_, raw, e := f.ParentChannelFactory.NewSnapshot(id, in)
	if e != nil {
		return adapter.ParentChannelExecution{}, nil, e
	}
	out, e := f.local(raw)
	return out, raw, e
}
func (f *parent180TLSFactory) ChangeSnapshot(id int64, raw []byte, in adapter.ParentChannelAccountChange) (adapter.ParentChannelExecution, []byte, error) {
	_, next, e := f.ParentChannelFactory.ChangeSnapshot(id, raw, in)
	if e != nil {
		return adapter.ParentChannelExecution{}, nil, e
	}
	out, e := f.local(next)
	return out, next, e
}
func (f *parent180TLSFactory) CopySnapshot(from, id int64, raw []byte, in adapter.ParentChannelAccountChange) (adapter.ParentChannelExecution, []byte, error) {
	_, next, e := f.ParentChannelFactory.CopySnapshot(from, id, raw, in)
	if e != nil {
		return adapter.ParentChannelExecution{}, nil, e
	}
	out, e := f.local(next)
	return out, next, e
}
func (f *parent180TLSFactory) Restore(id int64, raw []byte) (adapter.ParentChannelExecution, error) {
	if _, e := f.ParentChannelFactory.Restore(id, raw); e != nil {
		return adapter.ParentChannelExecution{}, e
	}
	return f.local(raw)
}
func parent180ClientCertificate(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, serial int64, name string) (string, string) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if e != nil {
		t.Fatal(e)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}
func parent180SignedXML(fields map[string]string, key string) []byte {
	algorithm := fields["sign_type"]
	if algorithm == "" {
		algorithm = "MD5"
	}
	fields["sign"], _ = adapter.SignWechatV2(fields, key, algorithm)
	var b bytes.Buffer
	b.WriteString("<xml>")
	for name, value := range fields {
		fmt.Fprintf(&b, "<%s>", name)
		xml.EscapeText(&b, []byte(value))
		fmt.Fprintf(&b, "</%s>", name)
	}
	b.WriteString("</xml>")
	return b.Bytes()
}
func parent180WxPay(t *testing.T, f channel168Fixture, id, no string) map[string]any {
	return parent180WxPayment(t, f, id, no, "native", "")
}
func parent180WxPayment(t *testing.T, f channel168Fixture, id, no, method, authCode string) map[string]any {
	t.Helper()
	p := compat.Params{"pid": "1", "out_trade_no": no, "type": "wxpay", "name": "synthetic parent180 receiving", "money": "100.00", "channel_id": id, "method": method, "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
	if authCode != "" {
		p["auth_code"] = authCode
	}
	p["sign"] = compat.SignMD5(p, "test-secret")
	v := url.Values{}
	for key, value := range p {
		v.Set(key, value)
	}
	r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var out map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(w.Code, e)
	}
	return out
}

type parent180Runtime struct {
	f                                                                                   channel168Fixture
	id                                                                                  string
	state                                                                               map[string]any
	keyB, certB, pemB                                                                   string
	expectedCreate                                                                      *atomic.Value
	creates, queries, refunds, bad                                                      *atomic.Int64
	transfers, transferQueries                                                          *atomic.Int64
	sharingReceiverCalls                                                                *atomic.Int64
	sharingSends, sharingQueries, sharingReturns, sharingReturnQueries, sharingFinishes *atomic.Int64
	wireCharge                                                                          func(string) string
	loseScanResponse                                                                    *atomic.Bool
}

func parent180RuntimeReady(t *testing.T) parent180Runtime {
	f, body := parent180ReadyCreate(t)
	const keyA = "abcdef0123456789abcdef0123456789"
	const keyB = "fedcba9876543210fedcba9876543210"
	caKey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "parent180 synthetic CA"}, IsCA: true, BasicConstraintsValid: true, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, e := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if e != nil {
		t.Fatal(e)
	}
	ca, e := x509.ParseCertificate(caDER)
	if e != nil {
		t.Fatal(e)
	}
	certA, pemA := parent180ClientCertificate(t, ca, caKey, 2, "parent180-client-A")
	certB, pemB := parent180ClientCertificate(t, ca, caKey, 3, "parent180-client-B")
	clientRoots := x509.NewCertPool()
	clientRoots.AddCert(ca)
	var creates, queries, refunds, bad atomic.Int64
	var transfers, transferQueries atomic.Int64
	var sharingReceiverCalls atomic.Int64
	var sharingSends, sharingQueries, sharingReturns, sharingReturnQueries, sharingFinishes atomic.Int64
	var expectedCreate atomic.Value
	expectedCreate.Store("A")
	var mu sync.Mutex
	versions := map[string]string{}
	wireCharges := map[string]string{}
	sharingRows := map[string][]map[string]any{}
	sharingTrades := map[string]string{}
	sharingReturnRows := map[string]map[string]string{}
	scanTrades := map[string]bool{}
	var loseScanResponse atomic.Bool
	up := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var doc struct {
			Fields []struct {
				XMLName xml.Name
				Value   string `xml:",chardata"`
			} `xml:",any"`
		}
		if xml.Unmarshal(raw, &doc) != nil {
			bad.Add(1)
			http.Error(w, "bad local XML", 400)
			return
		}
		fields := map[string]string{}
		for _, field := range doc.Fields {
			fields[field.XMLName.Local] = field.Value
		}
		trade := fields["out_trade_no"]
		if fields["transaction_id"] != "" {
			trade = strings.TrimPrefix(fields["transaction_id"], "WX180_")
		}
		if fields["partner_trade_no"] != "" {
			trade = fields["partner_trade_no"]
		}
		mu.Lock()
		if trade == "" && fields["out_order_no"] != "" {
			trade = sharingTrades[fields["out_order_no"]]
		}
		version := versions[trade]
		if r.URL.Path == "/pay/unifiedorder" || r.URL.Path == "/pay/micropay" {
			if r.URL.Path == "/pay/micropay" {
				scanTrades[trade] = true
			}
			creates.Add(1)
			version = expectedCreate.Load().(string)
			versions[trade] = version
			wireCharges[trade] = fields["total_fee"]
		}
		if r.URL.Path == "/mmpaymkttransfers/promotion/transfers" || r.URL.Path == "/mmpaysptrans/pay_bank" {
			transfers.Add(1)
			version = expectedCreate.Load().(string)
			versions[trade] = version
		}
		if r.URL.Path == "/pay/profitsharingaddreceiver" || r.URL.Path == "/pay/profitsharingremovereceiver" {
			sharingReceiverCalls.Add(1)
			version = expectedCreate.Load().(string)
			versions[trade] = version
		}
		if r.URL.Path == "/secapi/pay/profitsharing" {
			sharingSends.Add(1)
			var rows []map[string]any
			json.Unmarshal([]byte(fields["receivers"]), &rows)
			sharingRows[fields["out_order_no"]] = rows
			sharingTrades[fields["out_order_no"]] = trade
		}
		if r.URL.Path == "/secapi/pay/profitsharingfinish" {
			sharingFinishes.Add(1)
			sharingTrades[fields["out_order_no"]] = trade
			sharingRows[fields["out_order_no"]] = []map[string]any{{"type": "MERCHANT_ID", "account": fields["mch_id"], "amount": 10000}}
		}
		if r.URL.Path == "/secapi/pay/profitsharingreturn" {
			sharingReturns.Add(1)
			sharingReturnRows[fields["out_return_no"]] = fields
		}
		if r.URL.Path == "/pay/profitsharingreturnquery" {
			sharingReturnQueries.Add(1)
		}
		charge := wireCharges[trade]
		mu.Unlock()
		if r.URL.Path == "/pay/orderquery" {
			queries.Add(1)
		}
		if r.URL.Path == "/secapi/pay/refund" {
			refunds.Add(1)
		}
		if r.URL.Path == "/mmpaymkttransfers/gettransferinfo" || r.URL.Path == "/mmpaysptrans/query_bank" {
			transferQueries.Add(1)
		}
		if r.URL.Path == "/pay/profitsharingquery" {
			sharingQueries.Add(1)
		}
		key := keyA
		if version == "B" {
			key = keyB
		}
		algorithm := "MD5"
		if strings.Contains(r.URL.Path, "profitsharing") {
			algorithm = "HMAC-SHA256"
		}
		signature, _ := adapter.SignWechatV2(fields, key, algorithm)
		if version == "" || signature != fields["sign"] || len(r.TLS.PeerCertificates) != 1 || r.TLS.PeerCertificates[0].Subject.CommonName != "parent180-client-"+version {
			bad.Add(1)
			http.Error(w, "original account key/certificate mismatch", 403)
			return
		}
		// Explicit test fault, after real signature and client-certificate checks
		// and durable peer-side acceptance. Never alter the SDK or its identity.
		if r.URL.Path == "/pay/micropay" && loseScanResponse.CompareAndSwap(true, false) {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				bad.Add(1)
				return
			}
			connection.Close()
			return
		}
		reply := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": fields["appid"], "mch_id": fields["mch_id"], "nonce_str": "synthetic180-local-nonce"}
		if algorithm != "MD5" {
			reply["sign_type"] = algorithm
		}
		switch r.URL.Path {
		case "/pay/profitsharingaddreceiver", "/pay/profitsharingremovereceiver":
			reply["receiver"] = fields["receiver"]
		case "/secapi/pay/profitsharingreturn", "/pay/profitsharingreturnquery":
			delete(reply, "result_code")
			mu.Lock()
			original := sharingReturnRows[fields["out_return_no"]]
			mu.Unlock()
			for _, field := range []string{"out_order_no", "out_return_no", "return_account_type", "return_account", "return_amount"} {
				reply[field] = original[field]
			}
			reply["order_id"] = "WX_" + fields["out_order_no"]
			reply["return_no"] = "WX_" + fields["out_return_no"]
			reply["result"] = "PROCESSING"
			if r.URL.Path == "/pay/profitsharingreturnquery" {
				reply["result"] = "SUCCESS"
			}
		case "/secapi/pay/profitsharing", "/pay/profitsharingquery", "/secapi/pay/profitsharingfinish":
			reply["out_order_no"] = fields["out_order_no"]
			reply["transaction_id"] = fields["transaction_id"]
			reply["order_id"] = "WX_" + fields["out_order_no"]
			reply["status"] = "PROCESSING"
			mu.Lock()
			rows := sharingRows[fields["out_order_no"]]
			copyRows := make([]map[string]any, len(rows))
			for i, row := range rows {
				copyRows[i] = map[string]any{"type": row["type"], "account": row["account"], "amount": row["amount"], "result": "PENDING"}
				if r.URL.Path == "/pay/profitsharingquery" {
					copyRows[i]["result"] = "SUCCESS"
					copyRows[i]["detail_id"] = "DETAIL180"
				}
			}
			mu.Unlock()
			if r.URL.Path == "/pay/profitsharingquery" {
				reply["status"] = "FINISHED"
			}
			encoded, _ := json.Marshal(copyRows)
			reply["receivers"] = string(encoded)
		case "/pay/profitsharingorderamountquery":
			reply["transaction_id"] = fields["transaction_id"]
			reply["unsplit_amount"] = "0"
		case "/mmpaysptrans/pay_bank", "/mmpaysptrans/query_bank":
			delete(reply, "appid")
			reply["partner_trade_no"] = trade
			reply["payment_no"] = "BANK180"
			reply["amount"] = "100"
			reply["cmms_amt"] = "0"
			if r.URL.Path == "/mmpaysptrans/query_bank" {
				reply["status"] = "SUCCESS"
				reply["pay_succ_time"] = "2026-10-26 12:00:00"
				reply["bank_no_md5"] = fmt.Sprintf("%X", md5.Sum([]byte("6222020000000000000")))
				reply["true_name_md5"] = fmt.Sprintf("%X", md5.Sum([]byte("Synthetic Receiver")))
			} else if fields["enc_bank_no"] == "" || fields["enc_true_name"] == "" || bytes.Contains(raw, []byte("6222020000000000000")) {
				bad.Add(1)
				w.WriteHeader(400)
				return
			}
		case "/mmpaymkttransfers/promotion/transfers":
			http.Error(w, "synthetic lost transfer response", 500)
			return
		case "/mmpaymkttransfers/gettransferinfo":
			reply["partner_trade_no"] = trade
			reply["openid"] = "synthetic180-recipient"
			reply["detail_id"] = "transfer180-" + trade
			reply["payment_amount"] = "100"
			reply["status"] = "SUCCESS"
			reply["payment_time"] = "2026-10-26 12:00:00"
		case "/pay/micropay":
			reply["out_trade_no"] = trade
			reply["transaction_id"] = "WX180_" + trade
			reply["trade_type"] = "MICROPAY"
			reply["total_fee"] = charge
			reply["openid"] = "synthetic180-buyer"
		case "/pay/unifiedorder":
			reply["prepay_id"] = "prepay180-" + version
			reply["code_url"] = "weixin://wxpay/bizpayurl?pr=synthetic180"
			reply["trade_type"] = "NATIVE"
		case "/pay/orderquery":
			reply["trade_state"] = "SUCCESS"
			mu.Lock()
			scan := scanTrades[trade]
			mu.Unlock()
			if scan {
				reply["trade_type"] = "MICROPAY"
			}
			reply["out_trade_no"] = trade
			reply["transaction_id"] = "WX180_" + trade
			reply["total_fee"] = charge
			reply["fee_type"] = "CNY"
		case "/secapi/pay/refund":
			reply["out_trade_no"] = trade
			reply["out_refund_no"] = fields["out_refund_no"]
			reply["refund_id"] = "refund180-" + fields["out_refund_no"]
			reply["refund_fee"] = fields["refund_fee"]
		default:
			bad.Add(1)
			http.Error(w, "unexpected synthetic path", 404)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write(parent180SignedXML(reply, key))
	}))
	up.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientRoots, MinVersion: tls.VersionTLS12}
	up.StartTLS()
	t.Cleanup(up.Close)
	origin := channelconfig.Channel{ID: 8, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "synthetic180-origin", "appmchid": "synthetic180-origin-mch", "appkey": keyA}, Certificates: map[string]string{"sslcert_path": certA, "sslkey_path": pemA}}
	base, e := channelconfig.NewParentChannelFactory(origin, channelconfig.ParentBuildOptions{BuildOptions: channelconfig.BuildOptions{AllowLive: true}})
	if e != nil {
		t.Fatal(e)
	}
	factory := &parent180TLSFactory{ParentChannelFactory: base, endpoint: up.URL, roots: up.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs}
	bootstrap, _, e := factory.BootstrapSnapshot(8)
	if e != nil {
		t.Fatal(e)
	}
	f.s.ProviderFlow.Providers[8] = bootstrap.Payment
	f.s.ProviderFlow.ParentFactories[8] = factory
	templates := parent180HTTP(f, "GET", "/templates", "")
	var choices struct {
		Data struct{ Items []map[string]any }
	}
	json.Unmarshal(templates.Body.Bytes(), &choices)
	body["expected_source_fingerprint"] = choices.Data.Items[0]["fingerprint"]
	body["account"].(map[string]any)["certificates"] = map[string]any{"sslcert_path": map[string]string{"operation": "replace", "value": certA}, "sslkey_path": map[string]string{"operation": "replace", "value": pemA}}
	created := parent180Post(t, f, body)
	id := created["id"].(string)
	enable, _ := json.Marshal(map[string]any{"expected_fingerprint": created["fingerprint"], "enabled": true})
	enabled := parent180HTTP(f, "PATCH", "/"+id, string(enable))
	if enabled.Code != 200 {
		t.Fatal(enabled.Code, enabled.Body.String())
	}
	var state struct{ Data map[string]any }
	json.Unmarshal(enabled.Body.Bytes(), &state)
	return parent180Runtime{loseScanResponse: &loseScanResponse, f: f, id: id, state: state.Data, keyB: keyB, certB: certB, pemB: pemB, expectedCreate: &expectedCreate, creates: &creates, queries: &queries, refunds: &refunds, bad: &bad, transfers: &transfers, transferQueries: &transferQueries, sharingReceiverCalls: &sharingReceiverCalls, sharingSends: &sharingSends, sharingQueries: &sharingQueries, sharingReturns: &sharingReturns, sharingReturnQueries: &sharingReturnQueries, sharingFinishes: &sharingFinishes, wireCharge: func(trade string) string { mu.Lock(); defer mu.Unlock(); return wireCharges[trade] }}
}
func TestParentChannel180ActualMutualTLSNewIntentUsesCurrentAndOriginalQueryRefundKeepOriginalKeyAndCertificate(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f, id := setup.f, setup.id
	state := struct{ Data map[string]any }{setup.state}
	keyB, certB, pemB := setup.keyB, setup.certB, setup.pemB
	expectedCreate := setup.expectedCreate
	creates, queries, refunds, bad := setup.creates, setup.queries, setup.refunds, setup.bad
	var e error
	paid := parent180WxPay(t, f, id, "parent180-before-rotation")
	if paid["code"] != float64(1) {
		t.Fatalf("managed receiving parent never reached actual adapter: %v (creates=%d)", paid, creates.Load())
	}
	order, e := f.s.Store.Order(context.Background(), 1, "parent180-before-rotation")
	if e != nil {
		t.Fatal(e)
	}
	change, _ := json.Marshal(map[string]any{"expected_fingerprint": state.Data["fingerprint"], "account": map[string]any{"config": map[string]any{"appkey": map[string]string{"operation": "replace", "value": keyB}}, "certificates": map[string]any{"sslcert_path": map[string]string{"operation": "replace", "value": certB}, "sslkey_path": map[string]string{"operation": "replace", "value": pemB}}}})
	changed := parent180HTTP(f, "PATCH", "/"+id, string(change))
	if changed.Code != 200 {
		t.Fatal(changed.Code, changed.Body.String())
	}
	expectedCreate.Store("B")
	if out := parent180WxPay(t, f, id, "parent180-after-rotation"); out["code"] != float64(1) {
		t.Fatal(out)
	}
	if _, e = f.s.ProviderFlow.Query(context.Background(), 1, order.TradeNo); e != nil {
		t.Fatal("original query switched key/certificate", e)
	}
	if _, e = f.s.ProviderFlow.Refund(context.Background(), 1, order.TradeNo, "parent180-refund-original", 1000); e != nil {
		t.Fatal("original refund switched key/certificate", e)
	}
	if creates.Load() != 2 || queries.Load() != 1 || refunds.Load() != 1 || bad.Load() != 0 {
		t.Fatal("wrong original provider effects", creates.Load(), queries.Load(), refunds.Load(), bad.Load())
	}
}
