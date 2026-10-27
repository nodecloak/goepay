package server

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
)

// Both original Alipay account formats execute the real RSA2 SDK over local
// TLS. Literal C/N/F expectations are independent of the implementation quote.
func TestMerchantScan192ActualAlipayRSA2AndCertificateChargeAndQuery(t *testing.T) {
	for _, certificateMode := range []bool{false, true} {
		t.Run(map[bool]string{false: "public-key", true: "certificates"}[certificateMode], func(t *testing.T) {
			merchant, e := rsa.GenerateKey(rand.Reader, 2048)
			if e != nil {
				t.Fatal(e)
			}
			platform, e := rsa.GenerateKey(rand.Reader, 2048)
			if e != nil {
				t.Fatal(e)
			}
			var certs map[string]string
			var serial string
			if certificateMode {
				certs, serial = parent180AlipayCertificates(t, merchant, platform, 192)
			}
			var sends, queries, bad atomic.Int64
			var mu sync.Mutex
			amounts := map[string]string{}
			peer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if e := r.ParseForm(); e != nil {
					bad.Add(1)
					w.WriteHeader(400)
					return
				}
				params := map[string]string{}
				for k, v := range r.PostForm {
					if len(v) != 1 {
						bad.Add(1)
						w.WriteHeader(400)
						return
					}
					params[k] = v[0]
				}
				want, e := adapter.SignAlipayRSA2(params, merchant)
				if e != nil || want != params["sign"] {
					bad.Add(1)
					w.WriteHeader(403)
					return
				}
				var biz map[string]any
				if e = json.Unmarshal([]byte(params["biz_content"]), &biz); e != nil {
					bad.Add(1)
					w.WriteHeader(400)
					return
				}
				trade, _ := biz["out_trade_no"].(string)
				method := params["method"]
				mu.Lock()
				amount := amounts[trade]
				if method == "alipay.trade.pay" {
					sends.Add(1)
					amount, _ = biz["total_amount"].(string)
					amounts[trade] = amount
					if biz["auth_code"] != "281234567890123456" || biz["scene"] != "bar_code" || amount != "100.25" {
						bad.Add(1)
					}
				} else if method == "alipay.trade.query" {
					queries.Add(1)
				} else {
					bad.Add(1)
				}
				mu.Unlock()
				if trade == "" || amount == "" {
					bad.Add(1)
					w.WriteHeader(400)
					return
				}
				data := map[string]any{"code": "10000", "msg": "Success", "out_trade_no": trade, "trade_no": "ALI192_" + trade, "total_amount": amount, "buyer_user_id": "2088000000000192", "trade_status": "TRADE_SUCCESS"}
				raw, _ := json.Marshal(data)
				out := map[string]any{strings.ReplaceAll(method, ".", "_") + "_response": json.RawMessage(raw), "sign": parent180ResponseSignature(platform, raw)}
				if certificateMode {
					out["alipay_cert_sn"] = serial
					if params["app_cert_sn"] == "" || params["alipay_root_cert_sn"] == "" {
						bad.Add(1)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(out)
			}))
			t.Cleanup(peer.Close)
			f := channel168Local(t)
			if _, e = f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
				t.Fatal(e)
			}
			origin := channelconfig.Channel{ID: 8, Plugin: "alipay", AppTypes: []string{"3"}, Config: map[string]string{"appid": "scan192-app", "appmchid": "2088000000000192", "appsecret": parent180Private(merchant), "appkey": parent180Public(platform), "sign_type": "0", "enable_scan_pay": "false"}}
			if certificateMode {
				origin.Config["sign_type"] = "1"
				delete(origin.Config, "appkey")
				origin.Certificates = certs
			}
			factory, e := channelconfig.NewParentChannelFactory(origin, channelconfig.ParentBuildOptions{BuildOptions: channelconfig.BuildOptions{AllowLive: true}})
			if e != nil {
				t.Fatal(e)
			}
			local := &parent180TLSFactory{ParentChannelFactory: factory, endpoint: peer.URL, localProvider: func(c channelconfig.Channel) (adapter.Provider, error) {
				key, e := compat.ParsePrivateKey(c.Config["appsecret"])
				if e != nil {
					return nil, e
				}
				var public *rsa.PublicKey
				var certificates *adapter.AlipayCertificateConfig
				if c.Config["sign_type"] == "1" {
					certificates = &adapter.AlipayCertificateConfig{ApplicationPEM: c.Certificates["app_cert_path"], PlatformPEM: c.Certificates["alipay_cert_path"], RootPEM: c.Certificates["root_cert_path"]}
				} else {
					public, e = compat.ParsePublicKey(c.Config["appkey"])
					if e != nil {
						return nil, e
					}
				}
				client := peer.Client()
				client.Timeout = 2 * time.Second
				methods := []string{"native"}
				if c.Config["enable_scan_pay"] == "true" {
					methods = append(methods, "scan")
				}
				return adapter.NewAlipay(adapter.AlipayConfig{AppID: c.Config["appid"], SellerID: c.Config["appmchid"], PrivateKey: key, PublicKey: public, CertificateMode: certificates, Methods: methods, GatewayURL: peer.URL, HTTPClient: client})
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
			templates := channel168Data(t, parent180HTTP(f, "GET", "/templates", ""))["items"].([]any)
			config, certChanges := map[string]any{}, map[string]any{}
			for k, v := range origin.Config {
				config[k] = map[string]string{"operation": "replace", "value": v}
			}
			config["enable_scan_pay"] = map[string]string{"operation": "replace", "value": "true"}
			for k, v := range origin.Certificates {
				certChanges[k] = map[string]string{"operation": "replace", "value": v}
			}
			row := parent180Post(t, f, map[string]any{"source_channel_id": "8", "expected_source_fingerprint": templates[0].(map[string]any)["fingerprint"], "request_id": "19219219219219219219219219219219", "name": "Synthetic scan192 Alipay", "rate": "99.50", "mode": 0, "account": map[string]any{"app_types": origin.AppTypes, "config": config, "certificates": certChanges}})
			change, _ := json.Marshal(map[string]any{"expected_fingerprint": row["fingerprint"], "enabled": true})
			channel168Data(t, parent180HTTP(f, "PATCH", "/"+row["id"].(string), string(change)))
			owner := buyer190MerchantOn(t, f)
			policy := channel168Data(t, owner.buyer("GET", "", ""))
			change, _ = json.Marshal(map[string]any{"mode": 1, "mode_feerate": "0.25", "expected_fingerprint": policy["fingerprint"]})
			channel168Data(t, owner.buyer("PATCH", "", string(change)))
			const key = "56601362-916a-4f73-be0f-f5a684de1912"
			body := `{"request_id":"` + key + `","money":"100.00","remark":"支付宝合成收款","auth_code":"281234567890123456"}`
			w := merchantScan192Request(owner, "POST", "/prepare", body)
			if w.Code != 200 {
				t.Fatal("Alipay preparation", w.Code, w.Body.String())
			}
			prepared := channel168Data(t, w)
			if prepared["charge_money"] != "100.25" || prepared["net_money"] != "99.75" || prepared["fee_money"] != "0.50" || prepared["type"] != "alipay" || sends.Load() != 0 {
				t.Fatal("Alipay quote or zero-send prepare", prepared, sends.Load())
			}
			apply := `{"auth_code":"281234567890123456"}`
			paid := channel168Data(t, merchantScan192Request(owner, "POST", "/intents/"+key+"/apply", apply))
			if paid["state"] != "paid" {
				t.Fatal("Alipay receipt not settled", paid)
			}
			channel168Data(t, merchantScan192Request(owner, "POST", "/intents/"+key+"/apply", apply))
			channel168Data(t, merchantScan192Request(owner, "POST", "/intents/"+key+"/query", "{}"))
			if sends.Load() != 1 || queries.Load() != 1 || bad.Load() != 0 {
				t.Fatal("Alipay actual signed traffic", sends.Load(), queries.Load(), bad.Load())
			}
			var imbalance int
			if e = f.p.QueryRow(t.Context(), `SELECT count(*)FROM(SELECT journal_id FROM ledger_entries GROUP BY journal_id HAVING sum(amount)<>0)x`).Scan(&imbalance); e != nil || imbalance != 0 {
				t.Fatal("Alipay unbalanced", imbalance, e)
			}
		})
	}
}
