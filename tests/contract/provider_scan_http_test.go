package contract_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScanHTTPOriginalContractsUnknownRecoveryAndLateNotify(t *testing.T) {
	runChannel168ScanHTTPOriginalContractsUnknownRecoveryAndLateNotify(t, nil)
}
func TestChannel168ScanHTTPOriginalContractsUnknownRecoveryAndLateNotify(t *testing.T) {
	runChannel168ScanHTTPOriginalContractsUnknownRecoveryAndLateNotify(t, installChannel168Fees)
}
func runChannel168ScanHTTPOriginalContractsUnknownRecoveryAndLateNotify(t *testing.T, channelFees channel168FixturePolicy) {
	s, pool, key := fixture(t)
	ctx := context.Background()
	const wxKey = "0123456789abcdef0123456789abcdef"
	var wxWrites, aliWrites atomic.Int32
	var paid atomic.Bool
	var unknown sync.Map
	signAli := func(raw []byte) string {
		digest := sha256.Sum256(raw)
		sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if e != nil {
			t.Fatal(e)
		}
		return base64.StdEncoding.EncodeToString(sig)
	}
	wxUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Trade   string `xml:"out_trade_no"`
			Subject string `xml:"body"`
			Auth    string `xml:"auth_code"`
		}
		xml.NewDecoder(r.Body).Decode(&body)
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "wxapp", "mch_id": "mch", "out_trade_no": body.Trade, "transaction_id": "wx-" + body.Trade, "total_fee": "100", "openid": "wx-buyer", "trade_type": "MICROPAY"}
		if r.URL.Path == "/pay/micropay" {
			wxWrites.Add(1)
			if body.Auth == "" {
				t.Error("missing auth code")
			}
			if strings.Contains(body.Subject, "unknown") {
				unknown.Store(body.Trade, true)
				out["result_code"] = "FAIL"
				out["err_code"] = "USERPAYING"
			}
		} else if r.URL.Path == "/pay/orderquery" {
			out["trade_state"] = "SUCCESS"
			if _, ok := unknown.Load(body.Trade); ok && !paid.Load() {
				out["trade_state"] = "USERPAYING"
			}
		} else {
			t.Error("unexpected wx method", r.URL.Path)
		}
		out["sign"], _ = adapter.SignWechatV2(out, wxKey, "MD5")
		w.Write(providerXML(out))
	}))
	defer wxUpstream.Close()
	aliUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		var biz map[string]any
		json.Unmarshal([]byte(r.Form.Get("biz_content")), &biz)
		trade := biz["out_trade_no"].(string)
		out := map[string]string{"code": "10000", "out_trade_no": trade, "trade_no": "ali-" + trade, "total_amount": "1.00", "buyer_user_id": "2088000000000000", "trade_status": "TRADE_SUCCESS"}
		method := r.Form.Get("method")
		if method == "alipay.trade.pay" {
			aliWrites.Add(1)
			if biz["auth_code"] == nil || r.Form.Get("notify_url") == "" {
				t.Error("missing code/callback")
			}
			if strings.Contains(biz["subject"].(string), "unknown") {
				unknown.Store(trade, true)
				out["code"] = "10003"
			}
		} else if method == "alipay.trade.query" {
			if _, ok := unknown.Load(trade); ok && !paid.Load() {
				out["trade_status"] = "WAIT_BUYER_PAY"
			}
		} else {
			t.Error("unexpected ali method", method)
		}
		raw, _ := json.Marshal(out)
		json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(method, ".", "_") + "_response": json.RawMessage(raw), "sign": signAli(raw)})
	}))
	defer aliUpstream.Close()
	wx, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "wxapp", MerchantID: "mch", APIKey: wxKey, Methods: []string{"native", "scan"}, BaseURL: wxUpstream.URL, HTTPClient: wxUpstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "aliapp", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"page", "scan"}, GatewayURL: aliUpstream.URL, HTTPClient: aliUpstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: wx, 8: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{9}, 32)
	s.MerchantChannels = map[int64][]int64{1: {7, 8}, 2: {7, 8}}
	if channelFees != nil {
		applyChannel168Fixture(t, s, channelFees)
	}
	invoke := func(legacy bool, p compat.Params) map[string]any {
		t.Helper()
		path := "/api/pay/create"
		p["pid"] = "2"
		p["timestamp"] = fmt.Sprint(time.Now().Unix())
		p["sign_type"] = "RSA"
		if legacy {
			path = "/mapi.php"
			p["pid"] = "1"
			delete(p, "timestamp")
			p["sign_type"] = "MD5"
			p["sign"] = compat.SignMD5(p, "contract-secret")
		} else {
			p["sign"], _ = compat.SignRSA(p, key)
		}
		v := url.Values{}
		for k, x := range p {
			v.Set(k, x)
		}
		_, out := call(t, s.Handler(), "POST", path, v)
		return out
	}
	var unresolved []struct {
		trade, kind string
		mid         int64
		channel     int64
	}
	for _, scenario := range []struct {
		kind, code, channel string
		legacy              bool
	}{{"wxpay", "120000000000000001", "7", true}, {"alipay", "280000000000000001", "8", false}} {
		for _, state := range []string{"normal", "unknown"} {
			paid.Store(false)
			external := scenario.kind + "-scan-" + state
			p := compat.Params{"method": "scan", "auth_code": scenario.code, "channel_id": scenario.channel, "out_trade_no": external, "name": external, "money": "1.00", "clientip": "192.0.2.1", "notify_url": "https://merchant.example.invalid/notify"}
			out := invoke(scenario.legacy, p)
			trade, _ := out["trade_no"].(string)
			if trade == "" {
				t.Fatal("scan result omitted query identifier", out)
			}
			mid := int64(2)
			channel := int64(8)
			if scenario.legacy {
				mid = 1
				channel = 7
			}
			if state == "unknown" {
				if out["code"] != float64(-1) || out["pay_type"] == "scan" {
					t.Fatal("unknown scan declared successful", out)
				}
				unresolved = append(unresolved, struct {
					trade, kind string
					mid         int64
					channel     int64
				}{trade, scenario.kind, mid, channel})
			} else {
				want := float64(0)
				if scenario.legacy {
					want = 1
				}
				if out["code"] != want || !scenario.legacy && out["pay_type"] != "scan" {
					t.Fatal("wrong successful scan contract", out)
				}
				if scenario.legacy {
					if out["payurl"] == nil || out["pay_type"] != nil || out["pay_info"] != nil {
						t.Fatal("V1 response gained V2 fields", out)
					}
					w := httptest.NewRecorder()
					s.Handler().ServeHTTP(w, httptest.NewRequest("GET", out["payurl"].(string), nil))
					if w.Code != 200 || !strings.Contains(w.Body.String(), "付款码支付已完成") {
						t.Fatal("legacy paid result page unavailable", w.Code)
					}
				} else {
					var info map[string]string
					if e = json.Unmarshal([]byte(out["pay_info"].(string)), &info); e != nil || info["type"] != scenario.kind || info["trade_no"] != trade || info["money"] != "1.00" || info["buyer"] == "" {
						t.Fatal(info, e)
					}
				}
			}
			again := invoke(scenario.legacy, p)
			if again["trade_no"] != trade {
				t.Fatal("duplicate scan identifier changed", again)
			}
		}
	}
	if wxWrites.Load() != 2 || aliWrites.Load() != 2 {
		t.Fatal("duplicate scan write", wxWrites.Load(), aliWrites.Load())
	}
	expectedNet := int64(100)
	if channelFees != nil {
		// Original Pro retains floor(100 * 99.50 / 100) = 99 cents.
		expectedNet = 99
	}
	var invalidSnapshots int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE amount<>100 OR coalesce(actual_charge_amount104,amount)<>100 OR merchant_amount<>$1 OR platform_fee<>100-$1`, expectedNet).Scan(&invalidSnapshots); e != nil || invalidSnapshots != 0 {
		t.Fatal("scan gross/net/fee did not retain original fee calculation", invalidSnapshots, e)
	}
	s.ProviderFlow.AllowLive = false
	for _, item := range unresolved {
		if _, e = s.ProviderFlow.Query(ctx, item.mid, item.trade); e != nil {
			t.Fatal(e)
		}
	}
	for _, mid := range []int64{1, 2} {
		b, _ := s.Store.Balance(ctx, mid)
		if b.Available != expectedNet || b.Reserved != 0 {
			t.Fatal("pending scan credited", mid, b)
		}
	}
	paid.Store(true)
	for _, item := range unresolved {
		for i := 0; i < 2; i++ {
			if _, e = s.ProviderFlow.Query(ctx, item.mid, item.trade); e != nil {
				t.Fatal(e)
			}
		}
		var callback []byte
		if item.kind == "wxpay" {
			p := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "wxapp", "mch_id": "mch", "out_trade_no": item.trade, "transaction_id": "wx-" + item.trade, "total_fee": "100", "openid": "wx-buyer"}
			p["sign"], _ = adapter.SignWechatV2(p, wxKey, "MD5")
			callback = providerXML(p)
		} else {
			p := map[string]string{"app_id": "aliapp", "out_trade_no": item.trade, "trade_no": "ali-" + item.trade, "total_amount": "1.00", "trade_status": "TRADE_SUCCESS", "buyer_id": "2088000000000000", "sign_type": "RSA2"}
			p["sign"] = signAli([]byte(adapter.AlipayNotificationCanonical(p)))
			v := url.Values{}
			for k, x := range p {
				v.Set(k, x)
			}
			callback = []byte(v.Encode())
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("POST", fmt.Sprintf("/callbacks/providers/%d/%s", item.channel, item.trade), bytes.NewReader(callback)))
		if w.Code != 200 || item.kind == "wxpay" && !strings.Contains(w.Body.String(), "SUCCESS") || item.kind == "alipay" && w.Body.String() != "success" {
			t.Fatal("late callback failed", w.Code, w.Body.String())
		}
	}
	for _, mid := range []int64{1, 2} {
		b, _ := s.Store.Balance(ctx, mid)
		if b.Available != 2*expectedNet || b.Reserved != 0 {
			t.Fatal("late callback duplicate credit", mid, b)
		}
	}
	s.ProviderFlow.AllowLive = true
	for _, p := range []compat.Params{{"type": "alipay", "auth_code": "120000000000000001"}, {"auth_code": "invalid"}} {
		p["method"] = "scan"
		p["out_trade_no"] = "rejected-code"
		p["name"] = "test"
		p["money"] = "1.00"
		p["clientip"] = "192.0.2.1"
		p["notify_url"] = "https://merchant.example.invalid/notify"
		if out := invoke(false, p); out["code"] != float64(-1) {
			t.Fatal("invalid type/code accepted", out)
		}
	}
	var count int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM orders WHERE out_trade_no='rejected-code'").Scan(&count); e != nil || count != 0 {
		t.Fatal("bad code allocated order", count, e)
	}
	if wxWrites.Load() != 2 || aliWrites.Load() != 2 {
		t.Fatal("unknown scan resent")
	}
}

func TestPreauthorizationAndScanChannelStillRoutesWebToAuthorization(t *testing.T) {
	s, _, key := fixture(t)
	p, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"preauth", "scan"}})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{9: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{9}, 32)
	s.MerchantChannels = map[int64][]int64{2: {9}}
	params := compat.Params{"pid": "2", "type": "alipay", "method": "web", "out_trade_no": "preauth-with-scan", "name": "test", "money": "1.00", "sign_type": "RSA", "timestamp": fmt.Sprint(time.Now().Unix())}
	params["sign"], _ = compat.SignRSA(params, key)
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	_, out := call(t, s.Handler(), "POST", "/api/pay/create", values)
	if out["code"] != float64(0) || out["pay_type"] != "jump" {
		t.Fatal("scan opt-in removed authorization web product", out)
	}
	a, e := s.Store.ProviderAttempt(context.Background(), out["trade_no"].(string))
	if e != nil || a.Method != "preauth" {
		t.Fatal(a, e)
	}
}
