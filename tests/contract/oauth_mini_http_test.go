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
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMiniOAuthHTTPConsumesBeforeTLSExchangeAndBindsTrustedPayer(t *testing.T) {
	runMiniOAuthHTTPConsumesBeforeTLSExchangeAndBindsTrustedPayerWithOriginalGroupFixture(t, false)
}
func TestGroupRoutingUXHTTPMiniOAuthHTTPConsumesBeforeTLSExchangeAndBindsTrustedPayer(t *testing.T) {
	runMiniOAuthHTTPConsumesBeforeTLSExchangeAndBindsTrustedPayerWithOriginalGroupFixture(t, true)
}
func TestChannel168MiniOAuthHTTPConsumesBeforeTLSExchangeAndBindsTrustedPayer(t *testing.T) {
	runMiniOAuthHTTPConsumesBeforeTLSExchangeAndBindsTrustedPayerWithOriginalGroupFixture(t, true, installChannel168Fees)
}
func runMiniOAuthHTTPConsumesBeforeTLSExchangeAndBindsTrustedPayerWithOriginalGroupFixture(t *testing.T, groupRouting bool, channelFees ...channel168FixturePolicy) {
	s, pool, key := fixture(t)
	if groupRouting {
		installGroupRoutingEntrypointFixture152(t, s, map[string]string{"alipay": "7"})
	}
	s.Sandbox = false
	ctx := context.Background()
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	exchanges, creates := 0, 0
	failExchange := false
	var duringExchange func()
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		field := ""
		var node map[string]any
		switch r.Form.Get("method") {
		case "alipay.system.oauth.token":
			exchanges++
			if failExchange {
				w.WriteHeader(503)
				w.Write([]byte("mini-code mini-secret-token"))
				return
			}
			if duringExchange != nil {
				duringExchange()
			}
			field = "alipay_system_oauth_token_response"
			node = map[string]any{"user_id": "2088000000000000", "access_token": "mini-secret-token", "expires_in": "3600"}
		case "alipay.trade.create":
			creates++
			var b map[string]any
			json.Unmarshal([]byte(r.Form.Get("biz_content")), &b)
			if b["buyer_id"] != "2088000000000000" {
				t.Error("untrusted mini payer", b)
			}
			field = "alipay_trade_create_response"
			node = map[string]any{"code": "10000", "out_trade_no": b["out_trade_no"], "trade_no": "mini-awaiting-payment"}
		case "alipay.trade.query":
			var b map[string]any
			json.Unmarshal([]byte(r.Form.Get("biz_content")), &b)
			field = "alipay_trade_query_response"
			node = map[string]any{"code": "10000", "out_trade_no": b["out_trade_no"], "trade_no": "mini-awaiting-payment", "trade_status": "TRADE_SUCCESS", "total_amount": "1.25", "buyer_user_id": "2088000000000000"}
		default:
			t.Error("unexpected mini request", r.Form.Get("method"))
			w.WriteHeader(500)
			return
		}
		raw, _ := json.Marshal(node)
		digest := sha256.Sum256(raw)
		sig, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		json.NewEncoder(w).Encode(map[string]any{field: json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(sig)})
	}))
	defer up.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &platform.PublicKey, Methods: []string{"mini"}, MiniProgram: &adapter.MiniProgramConfig{Path: "pages/pay/pay"}, HTTPClient: &http.Client{Transport: oauthContractTLSTransport{up, t}}})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.MiniOAuthProviders = map[int64]adapter.MiniOAuthProvider{7: ali}
	s.CheckoutKey = bytes.Repeat([]byte{7}, 32)
	s.MerchantChannels = map[int64][]int64{1: {7}}
	if len(channelFees) > 0 && channelFees[0] != nil {
		applyChannel168Fixture(t, s, channelFees[0])
	}
	start := func(no string) (string, string) {
		t.Helper()
		var o payment.Order
		var e error
		if len(channelFees) > 0 && channelFees[0] != nil {
			_, out := call(t, s.Handler(), "POST", "/mapi.php", signed(compat.Params{"pid": "1", "out_trade_no": no, "money": "1.25", "name": "item", "type": "alipay", "method": "applet", "channel_id": "7", "is_applet": "1", "clientip": "192.0.2.5", "notify_url": "https://merchant.example.invalid/notify"}, "contract-secret"))
			if out["code"] != float64(1) {
				t.Fatal("configured mini HTTP birth", out)
			}
			o, e = s.Store.Order(ctx, 1, no)
		} else {
			o, e = s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: no, Type: "alipay", Name: "item", Amount: 125})
		}
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Store.SetOrderClientIP(ctx, o.ID, "192.0.2.5"); e != nil {
			t.Fatal(e)
		}
		b := payment.MiniLaunchBinding{TradeNo: o.TradeNo, MerchantID: 1, ChannelID: 7, Provider: "alipay", AppID: "app", Method: "mini", Purpose: "mini-payment", Path: "pages/pay/pay"}
		if e = s.Store.PrepareMiniPayment(ctx, b); e != nil {
			t.Fatal(e)
		}
		state, e := s.Store.IssueMiniState(ctx, b)
		if e != nil {
			t.Fatal(e)
		}
		return fmt.Sprintf("/oauth/mini/complete/alipay/7/%s/?state=%s", o.TradeNo, state), o.TradeNo
	}
	invoke := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.RemoteAddr = "198.51.100.4:42000"
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	path, trade := start("mini-main")
	initialBinding, e := s.Store.MiniPaymentIntent(ctx, trade)
	if e != nil {
		t.Fatal(e)
	}
	secondState, e := s.Store.IssueMiniState(ctx, initialBinding)
	if e != nil {
		t.Fatal(e)
	}
	for _, body := range []string{`{"code":"one","code":"two"}`, `{"code":"one","openid":"forged"}`, `{"code":"one","state":"wrong"}`, `{"code":"one","state":""}`, `{"code":"one"} {}`, `{"code":"one"}` + strings.Repeat(" ", 8300) + `{}`} {
		if w := invoke(path, body); w.Code != 400 || exchanges != 0 {
			t.Fatal("ambiguous mini body reached exchange", w.Code, exchanges)
		}
	}
	w := invoke(path, `{"code":"mini-code"}`)
	if w.Code != 200 || exchanges != 1 || creates != 1 {
		t.Fatal("mini completion failed", w.Code, w.Body.String(), exchanges, creates)
	}
	for _, secret := range []string{"mini-code", "mini-secret-token", "2088000000000000", "state="} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("mini response leaked exchange material", secret)
		}
	}
	if w = invoke(path, `{"code":"new-code"}`); w.Code != 400 || exchanges != 1 {
		t.Fatal("repeated state exchanged code", w.Code, exchanges)
	}
	secondPath := fmt.Sprintf("/oauth/mini/complete/alipay/7/%s/?state=%s", trade, secondState)
	if w = invoke(secondPath, `{"code":"switch-payer-code"}`); w.Code != 400 || exchanges != 1 {
		t.Fatal("existing attempt allowed fresh identity exchange", w.Code, exchanges)
	}
	o, e := s.Store.OrderByTradeNo(ctx, trade)
	if e != nil || o.Status == "paid" {
		t.Fatal("mini identity treated as paid", o, e)
	}
	bal, e := s.Store.Balance(ctx, 1)
	if e != nil || bal.Available != 0 {
		t.Fatal(bal, e)
	}
	for _, kind := range []string{"stop", "merchant", "channel", "policy"} {
		path, _ = start("mini-revoke-" + kind)
		before := exchanges
		switch kind {
		case "stop":
			s.ProviderFlow.AllowLive = false
		case "merchant":
			if _, e = pool.Exec(ctx, "UPDATE merchants SET enabled=false WHERE id=1"); e != nil {
				t.Fatal(e)
			}
		case "channel":
			s.MerchantChannels[1] = nil
		case "policy":
			duringExchange = func() { s.ProviderFlow.AllowLive = false }
		}
		w = invoke(path, `{"code":"revoke-`+kind+`"}`)
		if w.Code == 200 || creates != 1 {
			t.Fatal("revoked mini created payment", kind, w.Code)
		}
		if kind != "policy" && exchanges != before {
			t.Fatal("disabled mini exchanged code", kind)
		}
		s.ProviderFlow.AllowLive = true
		s.MerchantChannels[1] = []int64{7}
		if _, e = pool.Exec(ctx, "UPDATE merchants SET enabled=true WHERE id=1"); e != nil {
			t.Fatal(e)
		}
		duringExchange = nil
		before = exchanges
		if w = invoke(path, `{"code":"revived-code"}`); w.Code != 400 || exchanges != before {
			t.Fatal("restored config revived mini state", kind, w.Code)
		}
	}
	for _, blocked := range []struct{ kind, value string }{{"buyer", "2088000000000000"}, {"ip", "192.0.2.5"}, {"ip", "198.51.100.4"}} {
		path, _ = start("mini-block-" + blocked.value)
		before := exchanges
		raw, _ := json.Marshal(map[string]any{"kind": blocked.kind, "value": blocked.value, "enabled": true, "reason": "local mini contract"})
		var recordID int64
		if e = pool.QueryRow(ctx, "INSERT INTO backoffice_records(resource,body) VALUES('blacklist',$1) RETURNING id", raw).Scan(&recordID); e != nil {
			t.Fatal(e)
		}
		w = invoke(path, `{"code":"blocked-`+blocked.value+`"}`)
		if w.Code != 403 || creates != 1 || exchanges != before+1 {
			t.Fatal("mini bypassed trusted payer/source policy", blocked, w.Code, creates, exchanges)
		}
		if _, e = pool.Exec(ctx, "DELETE FROM backoffice_records WHERE id=$1", recordID); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 2; i++ {
		if _, e = s.ProviderFlow.Query(ctx, 1, trade); e != nil {
			t.Fatal("trusted mini payment query failed", e)
		}
	}
	notice := map[string]string{"app_id": "app", "out_trade_no": trade, "trade_no": "mini-awaiting-payment", "trade_status": "TRADE_SUCCESS", "total_amount": "1.25", "buyer_id": "2088000000000000", "notify_id": "mini-paid-notice", "sign_type": "RSA2"}
	digest := sha256.Sum256([]byte(adapter.AlipayNotificationCanonical(notice)))
	signature, signErr := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
	e = signErr
	notice["sign"] = base64.StdEncoding.EncodeToString(signature)
	if e != nil {
		t.Fatal(e)
	}
	form := url.Values{}
	for k, v := range notice {
		form.Set(k, v)
	}
	for i := 0; i < 2; i++ {
		if e = s.ProviderFlow.HandleNotification(ctx, 7, trade, nil, []byte(form.Encode())); e != nil {
			t.Fatal("late mini payment notification failed", e)
		}
	}
	bal, e = s.Store.Balance(ctx, 1)
	wantCredit := int64(125)
	if len(channelFees) > 0 && channelFees[0] != nil {
		wantCredit = 124
	}
	if e != nil || bal.Available != wantCredit {
		t.Fatal("trusted query/notification duplicated credit", bal, e)
	}
	var receipts int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM verified_payment_receipts WHERE order_id=$1", o.ID).Scan(&receipts); e != nil || receipts != 1 {
		t.Fatal("mini receipt count", receipts, e)
	}
	failurePath, failureTrade := start("mini-exchange-response-lost")
	failExchange = true
	before := exchanges
	if w = invoke(failurePath, `{"code":"lost-exchange-code"}`); w.Code != 400 || exchanges != before+1 || strings.Contains(w.Body.String(), "lost-exchange-code") {
		t.Fatal("lost exchange response mishandled", w.Code, w.Body.String())
	}
	failExchange = false
	failureBinding, e := s.Store.MiniPaymentIntent(ctx, failureTrade)
	if e != nil {
		t.Fatal(e)
	}
	freshState, e := s.Store.IssueMiniState(ctx, failureBinding)
	if e != nil {
		t.Fatal(e)
	}
	failurePath = fmt.Sprintf("/oauth/mini/complete/alipay/7/%s/?state=%s", failureTrade, freshState)
	before = exchanges
	if w = invoke(failurePath, `{"code":"lost-exchange-code"}`); w.Code != 400 || exchanges != before {
		t.Fatal("lost exchange code resent under fresh state", w.Code, exchanges)
	}
}
