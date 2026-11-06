package server

import (
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
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
	"goepay/internal/payment"
)

func parent180Private(k *rsa.PrivateKey) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}))
}
func parent180Public(k *rsa.PrivateKey) string {
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}
func parent180ResponseSignature(k *rsa.PrivateKey, raw []byte) string {
	h := sha256.Sum256(raw)
	sig, _ := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, h[:])
	return base64.StdEncoding.EncodeToString(sig)
}
func parent180AlipayCertificates(t *testing.T, app, platform *rsa.PrivateKey, serial int64) (map[string]string, string) {
	t.Helper()
	rootKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	now := time.Now()
	root := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "parent180 synthetic root"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, e := x509.CreateCertificate(rand.Reader, root, root, &rootKey.PublicKey, rootKey)
	if e != nil {
		t.Fatal(e)
	}
	root, e = x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	out := map[string]string{"root_cert_path": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
	for i, key := range []*rsa.PrivateKey{app, platform} {
		leaf := &x509.Certificate{SerialNumber: big.NewInt(serial + int64(i) + 1), Subject: pkix.Name{CommonName: "parent180 synthetic leaf"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
		der, e = x509.CreateCertificate(rand.Reader, leaf, root, &key.PublicKey, rootKey)
		if e != nil {
			t.Fatal(e)
		}
		out[[]string{"app_cert_path", "alipay_cert_path"}[i]] = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	}
	return out, fmt.Sprintf("%x", md5.Sum([]byte("CN=parent180 synthetic root"+strconv.FormatInt(serial+2, 10))))
}

type parent180RewriteTransport struct{ up *httptest.Server }

func (t parent180RewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "api.mch.weixin.qq.com" {
		return nil, adapter.ErrInvalid
	}
	u, _ := url.Parse(t.up.URL)
	copy := r.Clone(r.Context())
	copy.URL.Scheme = u.Scheme
	copy.URL.Host = u.Host
	return t.up.Client().Transport.RoundTrip(copy)
}

func TestParentChannel180ActualTLSAlipayAndWechatV3UseCurrentForNewAndOriginalForHistory(t *testing.T) {
	for _, family := range []string{"alipay", "alipay-certificate", "wechat-v3"} {
		t.Run(family, func(t *testing.T) {
			merchantA, _ := rsa.GenerateKey(rand.Reader, 2048)
			merchantB, _ := rsa.GenerateKey(rand.Reader, 2048)
			platformA, _ := rsa.GenerateKey(rand.Reader, 2048)
			platformB, _ := rsa.GenerateKey(rand.Reader, 2048)
			var certA, certB map[string]string
			var snA, snB string
			if family == "alipay-certificate" {
				certA, snA = parent180AlipayCertificates(t, merchantA, platformA, 180)
				certB, snB = parent180AlipayCertificates(t, merchantB, platformB, 190)
			}
			var creates, queries, refunds, bad atomic.Int64
			var next atomic.Int64
			var mu sync.Mutex
			versions := map[string]int64{}
			preauthTrades := map[string]bool{}
			released := map[string]bool{}
			var authQueries, captures, unfreezes atomic.Int64
			var outgoingSends, outgoingQueries, proofApplies, proofQueries atomic.Int64
			var up *httptest.Server
			up = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var trade, method string
				var version int64
				var body []byte
				var params map[string]string
				var biz map[string]any
				if strings.HasPrefix(family, "alipay") {
					r.ParseForm()
					params = map[string]string{}
					for k, v := range r.PostForm {
						if len(v) != 1 {
							bad.Add(1)
							w.WriteHeader(400)
							return
						}
						params[k] = v[0]
					}
					method = params["method"]
					json.Unmarshal([]byte(params["biz_content"]), &biz)
					trade, _ = biz["out_trade_no"].(string)
					if trade == "" {
						trade, _ = biz["out_order_no"].(string)
					}
					if trade == "" {
						trade, _ = biz["out_biz_no"].(string)
					}
					if trade == "" && method == "alipay.data.bill.ereceipt.apply" {
						value, _ := biz["key"].(string)
						trade = strings.TrimPrefix(value, "ALI_TRANSFER_")
					}
					if trade == "" && method == "alipay.data.bill.ereceipt.query" {
						value, _ := biz["file_id"].(string)
						trade = strings.TrimPrefix(value, "FILE180_")
					}
					if trade == "" {
						auth, _ := biz["auth_no"].(string)
						trade = strings.TrimPrefix(auth, "AUTH180_")
					}
				} else {
					body, _ = io.ReadAll(r.Body)
					json.Unmarshal(body, &biz)
					trade, _ = biz["out_trade_no"].(string)
					switch {
					case r.Method == "POST" && r.URL.Path == "/v3/pay/transactions/native":
						method = "create"
					case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v3/pay/transactions/out-trade-no/"):
						method = "query"
						trade = strings.TrimPrefix(r.URL.Path, "/v3/pay/transactions/out-trade-no/")
					case r.Method == "POST" && r.URL.Path == "/v3/refund/domestic/refunds":
						method = "refund"
					}
				}
				mu.Lock()
				version = versions[trade]
				isPreauth := preauthTrades[trade]
				wasReleased := released[trade]
				if method == "alipay.trade.precreate" || method == "create" {
					version = next.Load()
					versions[trade] = version
					creates.Add(1)
				}
				if method == "alipay.fund.trans.toaccount.transfer" || method == "alipay.fund.trans.uni.transfer" {
					version = next.Load()
					versions[trade] = version
					outgoingSends.Add(1)
				}
				mu.Unlock()
				if (method == "alipay.trade.query" && !isPreauth) || method == "query" {
					queries.Add(1)
				}
				if method == "alipay.trade.refund" || method == "refund" {
					refunds.Add(1)
				}
				merchant, platform := merchantA, platformA
				if version == 1 {
					merchant, platform = merchantB, platformB
				}
				if trade == "" {
					bad.Add(1)
					w.WriteHeader(400)
					return
				}
				if strings.HasPrefix(family, "alipay") {
					want, _ := adapter.SignAlipayRSA2(params, merchant)
					if params["sign"] != want {
						bad.Add(1)
						w.WriteHeader(403)
						return
					}
					out := map[string]any{"code": "10000", "msg": "Success", "out_trade_no": trade}
					switch method {
					case "alipay.fund.trans.toaccount.transfer", "alipay.fund.trans.uni.transfer":
						http.Error(w, "synthetic lost outgoing response", 503)
						return
					case "alipay.fund.trans.common.query":
						outgoingQueries.Add(1)
						out["out_biz_no"] = trade
						out["order_id"] = "ALI_TRANSFER_" + trade
						out["status"] = "SUCCESS"
						out["trans_amount"] = "1.00"
						out["pay_date"] = "2026-10-27 00:00:00"
					case "alipay.data.bill.ereceipt.apply":
						proofApplies.Add(1)
						out["file_id"] = "FILE180_" + trade
					case "alipay.data.bill.ereceipt.query":
						proofQueries.Add(1)
						out["file_id"] = "FILE180_" + trade
						out["status"] = "SUCCESS"
						out["download_url"] = "https://example.invalid/synthetic-receipt180"
					case "alipay.fund.auth.operation.detail.query":
						authQueries.Add(1)
						out["out_order_no"] = trade
						out["out_request_no"] = biz["out_request_no"]
						out["auth_no"] = "AUTH180_" + trade
						out["amount"] = "100.00"
						out["rest_amount"] = "100.00"
						out["payer_user_id"] = "2088000000000180"
						out["status"] = "SUCCESS"
						out["order_status"] = "AUTHORIZED"
						out["operation_type"] = "FREEZE"
						if wasReleased {
							out["rest_amount"] = "0.00"
							out["order_status"] = "FINISH"
							out["operation_type"] = "UNFREEZE"
						}
					case "alipay.trade.pay":
						captures.Add(1)
						out["trade_no"] = "ALI180_" + trade
						out["total_amount"] = "100.00"
						out["buyer_user_id"] = "2088000000000180"
					case "alipay.fund.auth.order.unfreeze":
						unfreezes.Add(1)
						out["auth_no"] = biz["auth_no"]
						out["out_request_no"] = biz["out_request_no"]
						out["amount"] = "100.00"
						mu.Lock()
						released[trade] = true
						mu.Unlock()
					case "alipay.trade.precreate":
						out["qr_code"] = "https://qr.alipay.com/synthetic180"
					case "alipay.trade.query":
						out["trade_no"] = "ALI180_" + trade
						out["trade_status"] = "TRADE_SUCCESS"
						out["total_amount"] = "100.00"
						out["buyer_user_id"] = "2088000000000180"
						if isPreauth {
							out["code"] = "40004"
							out["sub_code"] = "ACQ.TRADE_NOT_EXIST"
							delete(out, "trade_no")
						}
					case "alipay.trade.refund":
						out["trade_no"] = "ALI180_" + trade
						out["refund_fee"] = biz["refund_amount"]
					default:
						bad.Add(1)
						w.WriteHeader(404)
						return
					}
					raw, _ := json.Marshal(out)
					key := strings.ReplaceAll(method, ".", "_") + "_response"
					fields := map[string]any{key: json.RawMessage(raw), "sign": parent180ResponseSignature(platform, raw)}
					if family == "alipay-certificate" {
						sn := snA
						if version == 1 {
							sn = snB
						}
						fields["alipay_cert_sn"] = sn
						if params["app_cert_sn"] == "" || params["alipay_root_cert_sn"] == "" {
							bad.Add(1)
						}
					}
					envelope, _ := json.Marshal(fields)
					w.Header().Set("Content-Type", "application/json")
					w.Write(envelope)
				} else {
					values := map[string]string{}
					for _, match := range regexp.MustCompile(`([a-z_]+)="([^"]*)"`).FindAllStringSubmatch(r.Header.Get("Authorization"), -1) {
						values[match[1]] = match[2]
					}
					canonical := r.Method + "\n" + r.URL.RequestURI() + "\n" + values["timestamp"] + "\n" + values["nonce_str"] + "\n" + string(body) + "\n"
					sig, _ := base64.StdEncoding.DecodeString(values["signature"])
					h := sha256.Sum256([]byte(canonical))
					if rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, h[:], sig) != nil || values["mchid"] != "parent180-mch" {
						bad.Add(1)
						w.WriteHeader(403)
						return
					}
					var out map[string]any
					switch method {
					case "create":
						out = map[string]any{"code_url": "weixin://wxpay/bizpayurl?pr=synthetic180-v3"}
					case "query":
						out = map[string]any{"appid": "parent180-app", "mchid": "parent180-mch", "out_trade_no": trade, "transaction_id": "W180" + trade, "trade_state": "SUCCESS", "amount": map[string]any{"total": 10000, "currency": "CNY"}, "payer": map[string]string{"openid": "synthetic180-buyer"}}
					case "refund":
						out = map[string]any{"out_trade_no": trade, "transaction_id": "W180" + trade, "out_refund_no": biz["out_refund_no"], "refund_id": "REFUND180", "status": "SUCCESS", "amount": map[string]any{"refund": 1000, "total": 10000, "currency": "CNY"}}
					default:
						bad.Add(1)
						w.WriteHeader(404)
						return
					}
					raw, _ := json.Marshal(out)
					stamp := strconv.FormatInt(time.Now().Unix(), 10)
					nonce := "parent180-local-v3"
					w.Header().Set("Wechatpay-Timestamp", stamp)
					w.Header().Set("Wechatpay-Nonce", nonce)
					w.Header().Set("Wechatpay-Serial", "PUB_KEY_ID_PARENT180")
					w.Header().Set("Wechatpay-Signature", parent180ResponseSignature(platform, []byte(stamp+"\n"+nonce+"\n"+string(raw)+"\n")))
					w.Header().Set("Content-Type", "application/json")
					w.Write(raw)
				}
			}))
			t.Cleanup(up.Close)
			f := channel168Local(t)
			origin := channelconfig.Channel{ID: 8, Plugin: "alipay", AppTypes: []string{"3", "5"}, Config: map[string]string{"appid": "parent180-app", "appmchid": "parent180-mch", "appsecret": parent180Private(merchantA), "appkey": parent180Public(platformA), "sign_type": "0"}}
			if family == "alipay-certificate" {
				origin.Config["sign_type"] = "1"
				delete(origin.Config, "appkey")
				origin.Certificates = certA
			}
			if family == "wechat-v3" {
				origin.Plugin = "wxpay"
				origin.AppTypes = []string{"1"}
				origin.Config = map[string]string{"appid": "parent180-app", "appmchid": "parent180-mch", "wechat_api_version": "v3", "v3_api_key": "0123456789abcdef0123456789abcdef", "v3_merchant_serial": "PARENT180_SERIAL"}
				origin.Certificates = map[string]string{"v3_merchant_key": parent180Private(merchantA), "v3_verifier_PUB_KEY_ID_PARENT180": parent180Public(platformA)}
			}
			factory, e := channelconfig.NewParentChannelFactory(origin, channelconfig.ParentBuildOptions{BuildOptions: channelconfig.BuildOptions{AllowLive: true}})
			if e != nil {
				t.Fatal(e)
			}
			local := &parent180TLSFactory{ParentChannelFactory: factory, endpoint: up.URL, localProvider: func(c channelconfig.Channel) (adapter.Provider, error) {
				if strings.HasPrefix(family, "alipay") {
					key, e := compat.ParsePrivateKey(c.Config["appsecret"])
					if e != nil {
						return nil, e
					}
					var public *rsa.PublicKey
					var certificate *adapter.AlipayCertificateConfig
					if c.Config["sign_type"] == "1" {
						certificate = &adapter.AlipayCertificateConfig{ApplicationPEM: c.Certificates["app_cert_path"], PlatformPEM: c.Certificates["alipay_cert_path"], RootPEM: c.Certificates["root_cert_path"]}
					} else {
						public, e = compat.ParsePublicKey(c.Config["appkey"])
						if e != nil {
							return nil, e
						}
					}
					return adapter.NewAlipay(adapter.AlipayConfig{AppID: c.Config["appid"], SellerID: c.Config["appmchid"], PrivateKey: key, PublicKey: public, CertificateMode: certificate, Methods: []string{"native", "preauth"}, GatewayURL: up.URL, HTTPClient: up.Client()})
				}
				key, e := compat.ParsePrivateKey(c.Certificates["v3_merchant_key"])
				if e != nil {
					return nil, e
				}
				public, e := compat.ParsePublicKey(c.Certificates["v3_verifier_PUB_KEY_ID_PARENT180"])
				if e != nil {
					return nil, e
				}
				return adapter.NewWechatV3(adapter.WechatV3Config{AppID: c.Config["appid"], MerchantID: c.Config["appmchid"], MerchantSerial: c.Config["v3_merchant_serial"], APIv3Key: []byte(c.Config["v3_api_key"]), PrivateKey: key, VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_PARENT180": public}, Methods: []string{"native"}, HTTPClient: &http.Client{Transport: parent180RewriteTransport{up}}})
			}}
			bootstrap, _, e := local.BootstrapSnapshot(8)
			if e != nil {
				t.Fatal(e)
			}
			f.s.ProviderFlow.Providers[8] = bootstrap.Payment
			f.s.ProviderFlow.MerchantChannels = f.s.MerchantChannels
			f.s.ProviderFlow.ParentFactories = map[int64]adapter.ParentAccountFactory{8: local}
			if e = f.s.ProviderFlow.InstallParentAccountFactories(f.s.ProviderFlow.ParentFactories); e != nil {
				t.Fatal(e)
			}
			var choices struct {
				Data struct{ Items []map[string]any }
			}
			w := parent180HTTP(f, "GET", "/templates", "")
			json.Unmarshal(w.Body.Bytes(), &choices)
			if w.Code != 200 || len(choices.Data.Items) != 1 {
				t.Fatal(w.Code, w.Body.String())
			}
			newConfig, newCerts := map[string]any{}, map[string]any{}
			for k, v := range origin.Config {
				newConfig[k] = map[string]string{"operation": "replace", "value": v}
			}
			for k, v := range origin.Certificates {
				newCerts[k] = map[string]string{"operation": "replace", "value": v}
			}
			row := parent180Post(t, f, map[string]any{"source_channel_id": "8", "expected_source_fingerprint": choices.Data.Items[0]["fingerprint"], "request_id": "18018018018018018018018018018018", "name": "Synthetic protocol parent180", "rate": "99.50", "mode": 0, "account": map[string]any{"app_types": origin.AppTypes, "config": newConfig, "certificates": newCerts}})
			id := row["id"].(string)
			change, _ := json.Marshal(map[string]any{"expected_fingerprint": row["fingerprint"], "enabled": true})
			w = parent180HTTP(f, "PATCH", "/"+id, string(change))
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var state struct{ Data map[string]any }
			json.Unmarshal(w.Body.Bytes(), &state)
			payMethod := func(no, method string) {
				p := compat.Params{"pid": "1", "out_trade_no": no, "type": origin.Plugin, "name": "synthetic protocol", "money": "100.00", "channel_id": id, "method": method, "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
				p["sign"] = compat.SignMD5(p, "test-secret")
				v := url.Values{}
				for k, value := range p {
					v.Set(k, value)
				}
				r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(v.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				out := httptest.NewRecorder()
				f.s.Handler().ServeHTTP(out, r)
				var decoded map[string]any
				json.Unmarshal(out.Body.Bytes(), &decoded)
				if decoded["code"] != float64(1) {
					t.Fatal("actual managed protocol create failed", decoded)
				}
			}
			pay := func(no string) { payMethod(no, "native") }
			pay("parent180-protocol-A")
			order, e := f.s.Store.Order(t.Context(), 1, "parent180-protocol-A")
			if e != nil {
				t.Fatal(e)
			}
			var preauthOrders []string
			if strings.HasPrefix(family, "alipay") {
				if _, e = f.p.Exec(t.Context(), `UPDATE merchants SET profile=profile||'{"transfer_api_enabled":true,"transfer_rate":"0"}'::jsonb WHERE id=1;INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
					t.Fatal(e)
				}
				fund, e := f.s.Store.CreateOrder(t.Context(), payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "parent180-wallet-fund", Amount: 1000, Name: "Synthetic reserve"})
				if e != nil {
					t.Fatal(e)
				}
				if e = f.s.Store.MarkPaid(t.Context(), fund.TradeNo, "parent180-wallet-fund-local"); e != nil {
					t.Fatal(e)
				}
				channel, _ := strconv.ParseInt(id, 10, 64)
				outgoing, e := f.s.ProviderFlow.Transfer(t.Context(), 1, channel, "parent180-original-ali-transfer", adapter.TransferRequest{Type: "alipay", Account: "synthetic@example.test", Amount: 100, Remark: "synthetic receipt180"})
				if e == nil || outgoing.ID <= 0 || outgoingSends.Load() != 1 {
					t.Fatal("actual alias outgoing did not retain one unknown", outgoingSends.Load(), e)
				}
			}
			if strings.HasPrefix(family, "alipay") {
				for _, no := range []string{"parent180-preauth-capture", "parent180-preauth-release"} {
					payMethod(no, "preauth")
					o, e := f.s.Store.Order(t.Context(), 1, no)
					if e != nil {
						t.Fatal(e)
					}
					attempt, e := f.s.Store.ProviderAttempt(t.Context(), o.TradeNo)
					if e != nil || attempt.MaterialType != "preauth" {
						t.Fatal("freeze material missing", e)
					}
					v, e := url.ParseQuery(attempt.Material)
					if e != nil {
						t.Fatal(e)
					}
					params := map[string]string{}
					for k, values := range v {
						params[k] = values[0]
					}
					want, _ := adapter.SignAlipayRSA2(params, merchantA)
					if want != params["sign"] {
						t.Fatal("freeze used wrong account")
					}
					mu.Lock()
					versions[o.TradeNo] = 0
					preauthTrades[o.TradeNo] = true
					mu.Unlock()
					preauthOrders = append(preauthOrders, o.TradeNo)
				}
			}
			account := map[string]any{"config": map[string]any{"appsecret": map[string]string{"operation": "replace", "value": parent180Private(merchantB)}, "appkey": map[string]string{"operation": "replace", "value": parent180Public(platformB)}}}
			if family == "wechat-v3" {
				account = map[string]any{"certificates": map[string]any{"v3_merchant_key": map[string]string{"operation": "replace", "value": parent180Private(merchantB)}, "v3_verifier_PUB_KEY_ID_PARENT180": map[string]string{"operation": "replace", "value": parent180Public(platformB)}}}
			}
			if family == "alipay-certificate" {
				certs := map[string]any{}
				for k, v := range certB {
					certs[k] = map[string]string{"operation": "replace", "value": v}
				}
				account = map[string]any{"config": map[string]any{"appsecret": map[string]string{"operation": "replace", "value": parent180Private(merchantB)}}, "certificates": certs}
			}
			change, _ = json.Marshal(map[string]any{"expected_fingerprint": state.Data["fingerprint"], "account": account})
			w = parent180HTTP(f, "PATCH", "/"+id, string(change))
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			next.Store(1)
			pay("parent180-protocol-B")
			if _, e = f.s.ProviderFlow.Query(t.Context(), 1, order.TradeNo); e != nil {
				t.Fatal("old protocol query changed key", e)
			}
			if _, e = f.s.ProviderFlow.Refund(t.Context(), 1, order.TradeNo, "parent180-protocol-refund", 1000); e != nil {
				t.Fatal("old protocol refund changed key", e)
			}
			if len(preauthOrders) > 0 {
				outgoing, e := f.s.ProviderFlow.QueryTransfer(t.Context(), 1, "parent180-original-ali-transfer")
				if e != nil || outgoing.Status != "succeeded" || outgoingQueries.Load() != 1 {
					t.Fatal("outgoing query changed original account", outgoingQueries.Load(), e)
				}
				if _, e = f.s.ProviderFlow.TransferProof(t.Context(), 1, "parent180-original-ali-transfer"); e != nil {
					t.Fatal("receipt apply changed original account", e)
				}
				proof, e := f.s.ProviderFlow.TransferProof(t.Context(), 1, "parent180-original-ali-transfer")
				if e != nil || proof.State != "SUCCESS" || proofApplies.Load() != 1 || proofQueries.Load() != 1 || outgoingSends.Load() != 1 {
					t.Fatal("receipt query changed original account or resent", proofApplies.Load(), proofQueries.Load(), e)
				}
				channel, _ := strconv.ParseInt(id, 10, 64)
				for _, trade := range preauthOrders {
					if e = f.s.ProviderFlow.RefreshPreauthorization(t.Context(), trade, channel); e != nil {
						t.Fatal("authorization query changed original account", e)
					}
				}
				callback := map[string]string{"app_id": "parent180-app", "payee_user_id": "parent180-mch", "out_order_no": preauthOrders[0], "out_request_no": preauthOrders[0], "amount": "100.00", "auth_no": "AUTH180_" + preauthOrders[0], "payer_user_id": "2088000000000180", "status": "SUCCESS", "operation_type": "FREEZE", "sign_type": "RSA2"}
				if family == "alipay-certificate" {
					callback["alipay_cert_sn"] = snA
				}
				callback["sign"] = parent180ResponseSignature(platformA, []byte(adapter.AlipayNotificationCanonical(callback)))
				encoded := url.Values{}
				for k, v := range callback {
					encoded.Set(k, v)
				}
				f.s.ProviderFlow.AllowLive = false
				if e = f.s.ProviderFlow.HandlePreauthorizationNotification(t.Context(), channel, preauthOrders[0], nil, []byte(encoded.Encode())); e != nil {
					t.Fatal("original signed freeze callback changed account", e)
				}
				callback["sign"] = parent180ResponseSignature(platformB, []byte(adapter.AlipayNotificationCanonical(callback)))
				encoded.Set("sign", callback["sign"])
				if e = f.s.ProviderFlow.HandlePreauthorizationNotification(t.Context(), channel, preauthOrders[0], nil, []byte(encoded.Encode())); e == nil {
					t.Fatal("current platform key authenticated original freeze callback")
				}
				f.s.ProviderFlow.AllowLive = true
				toggle := func(enabled bool) {
					read := parent180HTTP(f, "GET", "/"+id, "")
					var current struct{ Data map[string]any }
					if json.Unmarshal(read.Body.Bytes(), &current) != nil || read.Code != 200 {
						t.Fatal("cannot read original parent")
					}
					payload, _ := json.Marshal(map[string]any{"expected_fingerprint": current.Data["fingerprint"], "enabled": enabled})
					if changed := parent180HTTP(f, "PATCH", "/"+id, string(payload)); changed.Code != 200 {
						t.Fatal("parent toggle failed", changed.Code)
					}
				}
				toggle(false)
				if _, e = f.s.ProviderFlow.CapturePreauthorization(t.Context(), 1, preauthOrders[0]); e == nil || captures.Load() != 0 {
					t.Fatal("disabled parent captured original authorization", captures.Load(), e)
				}
				original, e := f.s.Store.Preauthorization(t.Context(), preauthOrders[0])
				if e != nil || original.CaptureState != "none" {
					t.Fatal("disabled capture consumed first-send", e)
				}
				toggle(true)
				if _, e = f.s.ProviderFlow.CapturePreauthorization(t.Context(), 1, preauthOrders[0]); e != nil {
					t.Fatal("capture changed original account", e)
				}
				if _, e = f.s.ProviderFlow.ReleaseAuthorization(t.Context(), 1, preauthOrders[1]); e != nil {
					t.Fatal("unfreeze changed original account", e)
				}
				if captures.Load() != 1 || unfreezes.Load() != 1 || authQueries.Load() < 4 {
					t.Fatal("preauth did not use actual protocols", captures.Load(), unfreezes.Load(), authQueries.Load())
				}
			}
			if creates.Load() != 2 || queries.Load() != 1 || refunds.Load() != 1 || bad.Load() != 0 {
				t.Fatal("incorrect actual protocol effects", creates.Load(), queries.Load(), refunds.Load(), bad.Load())
			}
		})
	}
}
