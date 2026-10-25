package contract_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsoleManualPreauthorizationCaptureTLSAuditCSRFUnknownAndQueryOnlyRecovery(t *testing.T) {
	s, pool, merchant := fixture(t)
	s.Sandbox = false
	ctx := context.Background()
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	reads, writes := 0, 0
	paid := false
	expectedTrade := ""
	activeAuth := "manual-auth"
	captureUnknown := true
	var duringAuth func()
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		var b map[string]any
		json.Unmarshal([]byte(r.Form.Get("biz_content")), &b)
		field := ""
		var node map[string]any
		switch r.Form.Get("method") {
		case "alipay.trade.query":
			reads++
			field = "alipay_trade_query_response"
			node = map[string]any{"code": "40004", "sub_code": "ACQ.TRADE_NOT_EXIST"}
			if paid {
				node = map[string]any{"code": "10000", "out_trade_no": b["out_trade_no"], "trade_no": "manual-real-payment-" + expectedTrade, "trade_status": "TRADE_SUCCESS", "total_amount": "1.25", "buyer_user_id": "2088000000000000"}
			}
		case "alipay.fund.auth.operation.detail.query":
			reads++
			if duringAuth != nil {
				duringAuth()
			}
			field = "alipay_fund_auth_operation_detail_query_response"
			node = map[string]any{"code": "10000", "out_order_no": b["out_order_no"], "out_request_no": b["out_request_no"], "auth_no": activeAuth, "payer_user_id": "2088000000000000", "amount": "1.25", "rest_amount": "1.25", "status": "SUCCESS", "order_status": "AUTHORIZED", "operation_type": "FREEZE"}
		case "alipay.trade.pay":
			writes++
			var audits int
			if e = pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE operation='capture-authorization-intent'").Scan(&audits); e != nil || audits == 0 {
				t.Error("capture before durable actor intent", audits, e)
			}
			if b["auth_no"] != activeAuth || b["total_amount"] != "1.25" || b["out_trade_no"] != expectedTrade || b["auth_confirm_mode"] != "COMPLETE" {
				t.Error("capture binding changed", b)
			}
			if captureUnknown {
				w.WriteHeader(503)
				return
			}
			field = "alipay_trade_pay_response"
			node = map[string]any{"code": "10000", "out_trade_no": expectedTrade, "trade_no": "manual-real-payment-" + expectedTrade, "total_amount": "1.25", "buyer_user_id": "2088000000000000"}
		default:
			t.Error("unexpected manual capture API", r.Form.Get("method"))
			w.WriteHeader(500)
			return
		}
		raw, _ := json.Marshal(node)
		digest := sha256.Sum256(raw)
		sig, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		json.NewEncoder(w).Encode(map[string]any{field: json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(sig)})
	}))
	defer up.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"preauth"}, HTTPClient: &http.Client{Transport: oauthContractTLSTransport{up, t}}})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.MerchantChannels = map[int64][]int64{1: {7}}
	// Authorization identifiers are globally unique; the first trusted query must
	// report exactly the authorization stored on this order.
	order, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "manual-console", Name: "deposit", Type: "alipay", Amount: 125})
	if e != nil {
		t.Fatal(e)
	}
	expectedTrade = order.TradeNo
	if _, e = s.ProviderFlow.SubmitPreauthorization(ctx, order.TradeNo, 7, "192.0.2.1", ""); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.RecordPreauthorization(ctx, 7, adapter.AuthorizationEvent{TradeNo: order.TradeNo, RequestNo: order.TradeNo, AuthNo: "manual-auth", BuyerID: "2088000000000000", Amount: 125, State: "SUCCESS"}); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	if !s.CanCapturePreauthorization(ctx, order) || reads != 0 || writes != 0 {
		t.Fatal("capture capability made provider I/O or hid eligible state", reads, writes)
	}
	invoke := func(trade, csrfValue string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/console/v1/merchant/orders/"+trade+"/capture-authorization", strings.NewReader("{}"))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		r.Header.Set("X-CSRF-Token", csrfValue)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	foreign, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "foreign-manual", Name: "deposit", Type: "alipay", Amount: 125})
	if e != nil {
		t.Fatal(e)
	}
	if w := invoke(foreign.TradeNo, csrf); w.Code != 404 || reads != 0 || writes != 0 {
		t.Fatal("foreign capture reached provider", w.Code)
	}
	if w := invoke(order.TradeNo, ""); w.Code != 403 || reads != 0 || writes != 0 {
		t.Fatal("capture CSRF bypass", w.Code)
	}
	if _, e = pool.Exec(ctx, `CREATE FUNCTION reject_capture_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected capture audit failure'; END $$; CREATE TRIGGER reject_capture_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_capture_audit()`); e != nil {
		t.Fatal(e)
	}
	if w := invoke(order.TradeNo, csrf); w.Code < 400 || reads != 0 || writes != 0 {
		t.Fatal("failed actor audit allowed upstream", w.Code, reads, writes)
	}
	if _, e = pool.Exec(ctx, "DROP TRIGGER reject_capture_audit ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow.AllowLive = false
	if s.CanCapturePreauthorization(ctx, order) || reads != 0 {
		t.Fatal("stopped capture advertised capability")
	}
	if w := invoke(order.TradeNo, csrf); w.Code != 503 || reads != 0 {
		t.Fatal("stop capture bypass", w.Code)
	}
	s.ProviderFlow.AllowLive = true
	s.MerchantChannels[1] = nil
	if w := invoke(order.TradeNo, csrf); w.Code < 400 || reads != 0 {
		t.Fatal("unmapped channel capture bypass", w.Code)
	}
	s.MerchantChannels[1] = []int64{7}
	duringAuth = func() { s.MerchantChannels[1] = nil }
	if w := invoke(order.TradeNo, csrf); w.Code < 400 || writes != 0 {
		t.Fatal("permissions revoked during preflight", w.Code, writes)
	}
	duringAuth = nil
	s.MerchantChannels[1] = []int64{7}
	if w := invoke(order.TradeNo, csrf); w.Code != 202 || writes != 1 {
		t.Fatal("unknown capture state missing", w.Code, w.Body.String(), writes)
	}
	if s.CanCapturePreauthorization(ctx, order) {
		t.Fatal("unknown capture advertised retry")
	}
	before := reads
	for i := 0; i < 2; i++ {
		if w := invoke(order.TradeNo, csrf); w.Code != 409 || writes != 1 || reads != before {
			t.Fatal("unknown capture resent/requeried by write action", w.Code, writes, reads)
		}
	}
	bal, _ := s.Store.Balance(ctx, 1)
	if bal.Available != 0 {
		t.Fatal("unknown manual capture credited", bal)
	}
	paid = true
	for i := 0; i < 2; i++ {
		if _, e = s.ProviderFlow.Query(ctx, 1, order.TradeNo); e != nil {
			t.Fatal(e)
		}
	}
	bal, _ = s.Store.Balance(ctx, 1)
	a, _ := s.Store.Preauthorization(ctx, order.TradeNo)
	if bal.Available != 125 || a.CaptureState != "succeeded" || writes != 1 {
		t.Fatal(bal, a, writes)
	}
	var audits int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE operation IN ('capture-authorization-intent','capture-authorization-result') AND resource_id=$1", order.ID).Scan(&audits); e != nil || audits < 4 {
		t.Fatal("manual capture missing actor results", audits, e)
	}
	// An audit-result failure cannot reverse a real capture or grant a retry.
	paid = false
	captureUnknown = false
	activeAuth = "manual-success-auth"
	second, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "manual-success", Name: "deposit", Type: "alipay", Amount: 125})
	if e != nil {
		t.Fatal(e)
	}
	expectedTrade = second.TradeNo
	if _, e = s.ProviderFlow.SubmitPreauthorization(ctx, second.TradeNo, 7, "192.0.2.1", ""); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.RecordPreauthorization(ctx, 7, adapter.AuthorizationEvent{TradeNo: second.TradeNo, RequestNo: second.TradeNo, AuthNo: activeAuth, BuyerID: "2088000000000000", Amount: 125, State: "SUCCESS"}); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `CREATE FUNCTION reject_capture_result_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation='capture-authorization-result' THEN RAISE EXCEPTION 'injected capture result audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_capture_result_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_capture_result_audit()`); e != nil {
		t.Fatal(e)
	}
	if w := invoke(second.TradeNo, csrf); w.Code != 503 || writes != 2 {
		t.Fatal("result audit failure misreported capture", w.Code, w.Body.String(), writes)
	}
	if _, e = pool.Exec(ctx, "DROP TRIGGER reject_capture_result_audit ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	if w := invoke(second.TradeNo, csrf); w.Code != 409 || writes != 2 {
		t.Fatal("audit failure enabled duplicate capture", w.Code, writes)
	}
	bal, _ = s.Store.Balance(ctx, 1)
	a, _ = s.Store.Preauthorization(ctx, second.TradeNo)
	if bal.Available != 250 || a.CaptureState != "succeeded" {
		t.Fatal("real capture lost after audit failure", bal, a)
	}
}
