package contract_test

import (
	"bytes"
	"context"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestWechatReceivingAccountSwitchRejectsCheckoutAndSignedNewMerchantCallback(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	key := "local-account-key-0123456789012345"
	var creates atomic.Int32
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		creates.Add(1)
		raw, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/pay/unifiedorder" || !bytes.Contains(raw, []byte("<mch_id>original-mch</mch_id>")) {
			t.Error("wrong create payer or endpoint", r.URL.Path)
		}
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "same-local-app", "mch_id": "original-mch", "code_url": "weixin://local-original-material"}
		out["sign"], _ = adapter.SignWechatV2(out, key, "MD5")
		w.Write(providerXML(out))
	}))
	defer up.Close()
	makeProvider := func(mch, secret string) adapter.Provider {
		t.Helper()
		p, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "same-local-app", MerchantID: mch, APIKey: secret, BaseURL: up.URL, HTTPClient: up.Client()})
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: makeProvider("original-mch", key)}, AllowLive: true, CallbackBaseURL: "https://local.example.invalid"}
	s.CheckoutKey = bytes.Repeat([]byte{7}, 32)
	s.MerchantChannels = map[int64][]int64{1: {7}}
	params := compat.Params{"pid": "1", "out_trade_no": "receiving-account-http", "money": "1.01", "name": "local", "type": "wxpay", "method": "web", "channel_id": "7"}
	_, result := call(t, s.Handler(), "POST", "/mapi.php", signed(params, "contract-secret"))
	if result["code"] != float64(1) {
		t.Fatal(result)
	}
	trade := result["trade_no"].(string)
	link := result["payurl"].(string)
	s.ProviderFlow.Providers[7] = makeProvider("changed-mch", key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", link, nil))
	if w.Code == 200 && strings.Contains(w.Body.String(), "weixin://local-original-material") {
		t.Fatal("reconfigured channel still served original account payment material")
	}
	callback := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "same-local-app", "mch_id": "changed-mch", "out_trade_no": trade, "transaction_id": "local-original-transaction", "total_fee": "101", "openid": "local-subject"}
	callback["sign"], _ = adapter.SignWechatV2(callback, key, "MD5")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/callbacks/providers/7/"+trade, bytes.NewReader(providerXML(callback))))
	if w.Code == 200 {
		t.Fatal("different merchant's valid signature credited old order", w.Body.String())
	}
	bal, e := s.Store.Balance(ctx, 1)
	if e != nil || bal.Available != 0 {
		t.Fatal(bal, e)
	}
	var proofCount int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM provider_attempt_accounts").Scan(&proofCount); e != nil || proofCount != 1 {
		t.Fatal(proofCount, e)
	}
	if _, e = pool.Exec(ctx, "UPDATE provider_attempt_accounts SET account_hash=decode(repeat('00',32),'hex')"); e == nil {
		t.Fatal("receiving merchant proof mutable")
	}
	// The same original merchant may rotate a credential. Only the public
	// account identity is bound, and the original material remains immutable.
	rotated := "rotated-account-key-01234567890123"
	s.ProviderFlow.Providers[7] = makeProvider("original-mch", rotated)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", link, nil))
	if w.Code != 200 {
		t.Fatal("key rotation blocked original checkout", w.Code)
	}
	callback["mch_id"] = "original-mch"
	callback["sign"], _ = adapter.SignWechatV2(callback, rotated, "MD5")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/callbacks/providers/7/"+trade, bytes.NewReader(providerXML(callback))))
	if w.Code != 200 {
		t.Fatal("original account not recoverable", w.Code, w.Body.String())
	}
	bal, e = s.Store.Balance(ctx, 1)
	if e != nil || bal.Available != 101 || creates.Load() != 1 {
		t.Fatal(bal, e, creates.Load())
	}
}
