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
	"goepay/internal/platform/jobs"
	"goepay/internal/reconciliation"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsoleAuthorizationReleaseAuditCSRFUnknownAndReadOnlyRecovery(t *testing.T) {
	s, pool, merchant := fixture(t)
	ctx := context.Background()
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	writes, reads := 0, 0
	released := false
	preflightUnavailable := true
	queryRequest := ""
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		method := r.Form.Get("method")
		var biz map[string]any
		json.Unmarshal([]byte(r.Form.Get("biz_content")), &biz)
		if method == "alipay.fund.auth.order.unfreeze" {
			writes++
			w.WriteHeader(503)
			return
		}
		reads++
		node := map[string]any{}
		field := ""
		switch method {
		case "alipay.trade.query":
			if preflightUnavailable {
				w.WriteHeader(503)
				return
			}
			field = "alipay_trade_query_response"
			node = map[string]any{"code": "40004", "sub_code": "ACQ.TRADE_NOT_EXIST"}
		case "alipay.fund.auth.operation.detail.query":
			field = "alipay_fund_auth_operation_detail_query_response"
			queryRequest = biz["out_request_no"].(string)
			rest, operation := "1.25", "FREEZE"
			if strings.HasPrefix(queryRequest, "UA") {
				if !released {
					node = map[string]any{"code": "40004", "sub_code": "AUTH.NOT_EXIST"}
					break
				}
				rest, operation = "0.00", "UNFREEZE"
			}
			node = map[string]any{"code": "10000", "out_order_no": biz["out_order_no"], "out_request_no": queryRequest, "auth_no": "console-auth", "payer_user_id": "2088000000000000", "amount": "1.25", "rest_amount": rest, "status": "SUCCESS", "order_status": "AUTHORIZED", "operation_type": operation}
		default:
			t.Error("unexpected authorization method", method)
			w.WriteHeader(500)
			return
		}
		raw, _ := json.Marshal(node)
		digest := sha256.Sum256(raw)
		signature, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		json.NewEncoder(w).Encode(map[string]any{field: json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(signature)})
	}))
	defer upstream.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: merchant, PublicKey: &platform.PublicKey, Methods: []string{"preauth"}, GatewayURL: upstream.URL, HTTPClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	order, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "console-release", Name: "deposit", Type: "alipay", Amount: 125})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProviderFlow.SubmitPreauthorization(ctx, order.TradeNo, 7, "192.0.2.1", ""); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.RecordPreauthorization(ctx, 7, adapter.AuthorizationEvent{TradeNo: order.TradeNo, RequestNo: order.TradeNo, AuthNo: "console-auth", BuyerID: "2088000000000000", Amount: 125, State: "SUCCESS"}); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	invoke := func(action, providedCSRF string) (int, map[string]any) {
		r := httptest.NewRequest("POST", "/console/v1/merchant/orders/"+order.TradeNo+"/"+action, strings.NewReader("{}"))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		r.Header.Set("X-CSRF-Token", providedCSRF)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(w.Body.String(), e)
		}
		return w.Code, out
	}
	foreign, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "foreign-release", Name: "deposit", Type: "alipay", Amount: 125})
	if e != nil {
		t.Fatal(e)
	}
	ownedTrade := order.TradeNo
	order.TradeNo = foreign.TradeNo
	if code, _ := invoke("release-authorization", csrf); code != 404 || reads != 0 || writes != 0 {
		t.Fatal("cross merchant release", code, reads, writes)
	}
	order.TradeNo = ownedTrade
	if code, _ := invoke("release-authorization", ""); code != 403 || reads != 0 || writes != 0 {
		t.Fatal("CSRF bypass", code, reads, writes)
	}
	if _, e = pool.Exec(ctx, `CREATE FUNCTION reject_release_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected release audit failure'; END $$; CREATE TRIGGER reject_release_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_release_audit()`); e != nil {
		t.Fatal(e)
	}
	if code, _ := invoke("release-authorization", csrf); code == 200 || code == 202 || reads != 0 || writes != 0 {
		t.Fatal("audit failure reached upstream", code, reads, writes)
	}
	if _, e = pool.Exec(ctx, "DROP TRIGGER reject_release_audit ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow.AllowLive = false
	if code, _ := invoke("release-authorization", csrf); code != 503 || reads != 0 || writes != 0 {
		t.Fatal("stop switch reached provider", code, reads, writes)
	}
	s.ProviderFlow.AllowLive = true
	code, out := invoke("release-authorization", csrf)
	if code != 202 || out["data"].(map[string]any)["sent"] != false || writes != 0 {
		t.Fatal("read failure was mistaken for a send", code, out, writes)
	}
	preflightUnavailable = false
	code, out = invoke("release-authorization", csrf)
	if code != 202 || out["code"] != float64(0) || writes != 1 {
		t.Fatal("durable unknown release unavailable", code, out, writes)
	}
	_, _ = invoke("release-authorization", csrf)
	if writes != 1 {
		t.Fatal("release resent")
	}
	s.ProviderFlow.AllowLive = false
	payload, _ := json.Marshal(map[string]string{"trade_no": order.TradeNo})
	worker := reconciliation.Handler{Pool: pool, Flow: s.ProviderFlow}
	if e := worker.Handle(ctx, jobs.Job{Kind: "authorization.release.reconcile", Payload: payload}); e == nil || writes != 1 || !strings.HasPrefix(queryRequest, "UA") {
		t.Fatal("worker did not preserve/query original unknown release", writes, queryRequest, e)
	}
	released = true
	if e := worker.Handle(ctx, jobs.Job{Kind: "authorization.release.reconcile", Payload: payload}); e != nil || writes != 1 {
		t.Fatal("worker failed read-only release recovery", writes, e)
	}
	code, out = invoke("query-authorization-release", csrf)
	if code != 200 || out["data"].(map[string]any)["state"] != "succeeded" || writes != 1 || !strings.HasPrefix(queryRequest, "UA") {
		t.Fatal(code, out, writes, queryRequest)
	}
	var audits int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE resource='orders' AND resource_id=$1 AND operation IN ('release-authorization-intent','release-authorization-result','query-authorization-release-intent','query-authorization-release-result')", order.ID).Scan(&audits); e != nil || audits < 6 {
		t.Fatal("missing actor audits", audits, e)
	}
	b, _ := s.Store.Balance(ctx, 1)
	current, _ := s.Store.OrderByTradeNo(ctx, order.TradeNo)
	if b.Available != 0 || current.Status != "closed" {
		t.Fatal("release invented merchant refund", b, current.Status)
	}
}
