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

func TestConsolePreauthObservationTLSReadOnlyAndStrictGate(t *testing.T) {
	s, pool, appKey := fixture(t)
	ctx := context.Background()
	s.Sandbox = false
	s.Sessions = identity.NewPersistentSessions(pool, true)
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	reads := 0
	badSignature := false
	var during func()
	trade := ""
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads++
		if e := r.ParseForm(); e != nil {
			t.Error(e)
			return
		}
		params := map[string]string{}
		for k, v := range r.PostForm {
			if len(v) != 1 {
				t.Error("duplicate vendor field")
			}
			params[k] = v[0]
		}
		sig, e := adapter.SignAlipayRSA2(params, appKey)
		if e != nil || sig != params["sign"] || params["method"] != "alipay.fund.auth.operation.detail.query" {
			t.Error("unexpected vendor operation", params["method"])
		}
		var b map[string]any
		json.Unmarshal([]byte(params["biz_content"]), &b)
		if b["out_order_no"] != trade || b["out_request_no"] != trade {
			t.Error("not original freeze query", b)
		}
		if during != nil {
			during()
		}
		node := map[string]any{"code": "10000", "out_order_no": trade, "out_request_no": trade, "auth_no": "observation-http-auth", "payer_user_id": "2088000000000068", "amount": "10.00", "rest_amount": "4.50", "status": "SUCCESS", "operation_type": "FREEZE", "order_status": "AUTHORIZED"}
		raw, _ := json.Marshal(node)
		digest := sha256.Sum256(raw)
		signature, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		if badSignature {
			node["rest_amount"] = "4.51"
			raw, _ = json.Marshal(node)
		}
		json.NewEncoder(w).Encode(map[string]any{"alipay_fund_auth_operation_detail_query_response": json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(signature)})
	}))
	defer up.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "2021000000000068", SellerID: "2088000000000068", PrivateKey: appKey, PublicKey: &platform.PublicKey, Methods: []string{"preauth"}, HTTPClient: &http.Client{Transport: oauthContractTLSTransport{up, t}}})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: ali}, AllowLive: false, CallbackBaseURL: "https://pay.example.invalid"}
	s.MerchantChannels = map[int64][]int64{1: {7}}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "observation-http", Name: "deposit", Type: "alipay", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	trade = o.TradeNo
	scope, e := adapter.TransferAccountScope(ali)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Store.BeginProviderAttemptWithAccount(ctx, trade, 7, "preauth", ali.ApplicationID(), "", scope); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.RecordPreauthorization(ctx, 7, adapter.AuthorizationEvent{TradeNo: trade, RequestNo: trade, AuthNo: "observation-http-auth", BuyerID: "2088000000000068", Amount: 1000, State: "SUCCESS"}); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	path := "/console/v1/merchant/orders/" + trade + "/authorization-observations"
	invoke := func(method, path, body, csrfValue string) *httptest.ResponseRecorder {
		return lookupHTTP(t, s.Handler(), method, path, body, token, "merchant", csrfValue)
	}
	body := `{"client_request_no":"` + strings.Repeat("1", 64) + `"}`
	if w := invoke("POST", path, body, csrf); w.Code != 503 || reads != 0 {
		t.Fatal("default gate", w.Code, w.Body.String(), reads)
	}
	s.AuthorizationObservationEnabled = true
	if !s.CanObservePreauthorization(ctx, o) || reads != 0 {
		t.Fatal("local capability must not read vendor", reads)
	}
	if w := invoke("GET", "/console/v1/merchant/orders/"+trade, "", ""); w.Code != 200 || lookupData(t, w)["can_observe_authorization"] != true || reads != 0 {
		t.Fatal("detail capability missing or queried vendor", w.Code, w.Body.String())
	}
	if w := invoke("POST", path, body, ""); w.Code != 403 || reads != 0 {
		t.Fatal("CSRF", w.Code, reads)
	}
	for _, body := range []string{`{}`, `{"client_request_no":1}`, `{"client_request_no":null}`, `{"client_request_no":"` + strings.Repeat("a", 64) + `","x":"1"}`, `{"client_request_no":"` + strings.Repeat("a", 64) + `","client_request_no":"` + strings.Repeat("b", 64) + `"}`, `{"client_request_no":"` + strings.Repeat("A", 64) + `"}`} {
		if w := invoke("POST", path, body, csrf); w.Code != 400 || reads != 0 {
			t.Fatal("strict intent", w.Code, w.Body.String(), reads)
		}
	}
	foreign, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "observation-foreign", Name: "deposit", Type: "alipay", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if w := invoke("POST", "/console/v1/merchant/orders/"+foreign.TradeNo+"/authorization-observations", body, csrf); w.Code != 404 || reads != 0 {
		t.Fatal("foreign", w.Code, reads)
	}
	second, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "observation-http-second", Name: "deposit", Type: "alipay", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Store.BeginProviderAttemptWithAccount(ctx, second.TradeNo, 7, "preauth", ali.ApplicationID(), "", scope); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.RecordPreauthorization(ctx, 7, adapter.AuthorizationEvent{TradeNo: second.TradeNo, RequestNo: second.TradeNo, AuthNo: "observation-http-second-auth", BuyerID: "2088000000000068", Amount: 1000, State: "SUCCESS"}); e != nil {
		t.Fatal(e)
	}
	var before string
	if e = pool.QueryRow(ctx, `SELECT json_build_array((SELECT row_to_json(o) FROM orders o WHERE trade_no=$1),(SELECT count(*) FROM ledger_entries),(SELECT count(*) FROM jobs),(SELECT count(*) FROM verified_payment_receipts),(SELECT row_to_json(a) FROM payment_authorizations a JOIN orders ao ON ao.id=a.order_id WHERE ao.trade_no=$1))::text`, trade).Scan(&before); e != nil {
		t.Fatal(e)
	}
	w := invoke("POST", path, body, csrf)
	if w.Code != 200 || reads != 1 {
		t.Fatal("verified read", w.Code, w.Body.String(), reads)
	}
	d := lookupData(t, w)
	id, _ := d["request_id"].(string)
	if len(id) != 64 || d["status"] != "verified" || d["rest_amount"] != "450" || d["amount"] != "1000" || d["merchant_id"] != "1" || d["source_verified"] != true {
		t.Fatal("typed result", d)
	}
	for i := 0; i < 2; i++ {
		if w := invoke("GET", path+"/"+id, "", ""); w.Code != 200 || reads != 1 {
			t.Fatal("GET queried vendor", w.Code, w.Body.String(), reads)
		}
	}
	if w := invoke("POST", path, body, csrf); w.Code != 200 || reads != 1 {
		t.Fatal("same nonce re-exchanged", w.Code, reads)
	}
	admin, adminCSRF, e := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	adminPath := "/console/v1/admin/orders/" + trade + "/authorization-observations/" + id
	if w := lookupHTTP(t, s.Handler(), "GET", adminPath, "", admin, "admin", ""); w.Code != 200 || reads != 1 {
		t.Fatal("current admin local read", w.Code, w.Body.String())
	}
	if w := lookupHTTP(t, s.Handler(), "GET", "/console/v1/admin/orders/"+second.TradeNo+"/authorization-observations/"+id, "", admin, "admin", ""); w.Code != 404 || reads != 1 {
		t.Fatal("admin cross-path ID", w.Code, w.Body.String())
	}
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	delegated, delegatedCSRF, _, e := s.Sessions.CreateDelegated(ctx, tx, identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1}, identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}, admin)
	if e != nil {
		tx.Rollback(ctx)
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	for _, method := range []string{"GET", "POST"} {
		target := path
		if method == "GET" {
			target += "/" + id
		}
		if w := lookupHTTP(t, s.Handler(), method, target, body, delegated, "merchant", delegatedCSRF); w.Code != 403 || reads != 1 {
			t.Fatal("delegated access", method, w.Code, w.Body.String())
		}
	}
	if w := lookupHTTP(t, s.Handler(), "GET", "/console/v1/merchant/orders/"+trade, "", delegated, "merchant", ""); w.Code != 200 || lookupData(t, w)["can_observe_authorization"] != false || reads != 1 {
		t.Fatal("delegated detail advertised observation", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("GET", path+"/"+id, nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	dup := httptest.NewRecorder()
	s.Handler().ServeHTTP(dup, r)
	if dup.Code != 403 || reads != 1 {
		t.Fatal("duplicate session cookie", dup.Code)
	}
	if w := invoke("GET", path+"/"+id+"?provider=alipay", "", ""); w.Code != 400 || reads != 1 {
		t.Fatal("query override", w.Code)
	}
	if w := lookupHTTP(t, s.Handler(), "POST", "/console/v1/admin/orders/"+trade+"/authorization-observations", body, admin, "admin", adminCSRF); w.Code != 200 || reads != 1 {
		t.Fatal("admin idem read required funds grant or vendor", w.Code, w.Body.String())
	}
	oldOrigin := s.ProviderFlow.CallbackBaseURL
	s.ProviderFlow.CallbackBaseURL = "http://pay.example.invalid"
	if s.CanObservePreauthorization(ctx, o) {
		t.Fatal("insecure origin advertised read")
	}
	if w := invoke("GET", path+"/"+id, "", ""); w.Code != 503 || reads != 1 {
		t.Fatal("insecure read gate", w.Code)
	}
	s.ProviderFlow.CallbackBaseURL = oldOrigin
	s.Sandbox = true
	if s.CanObservePreauthorization(ctx, o) {
		t.Fatal("sandbox read advertised")
	}
	s.Sandbox = false
	badSignature = true
	unknownBody := `{"client_request_no":"` + strings.Repeat("2", 64) + `"}`
	w = invoke("POST", path, unknownBody, csrf)
	d = lookupData(t, w)
	if w.Code != 202 || reads != 2 || d["status"] != "unknown" || d["source_verified"] != false || d["rest_known"] != false {
		t.Fatal("unknown result", w.Code, d, reads)
	}
	if _, ok := d["rest_amount"]; ok {
		t.Fatal("unknown fabricated zero")
	}
	if w := invoke("POST", path, unknownBody, csrf); w.Code != 202 || reads != 2 {
		t.Fatal("unknown nonce repeated vendor", w.Code, reads)
	}
	badSignature = false
	during = func() { s.MerchantChannels[1] = nil }
	if w := invoke("POST", path, `{"client_request_no":"`+strings.Repeat("3", 64)+`"}`, csrf); w.Code < 400 || reads != 3 || strings.Contains(w.Body.String(), "4.50") {
		t.Fatal("revoked observation returned proof", w.Code, w.Body.String(), reads)
	}
	during = nil
	s.MerchantChannels[1] = []int64{7}
	var after string
	if e = pool.QueryRow(ctx, `SELECT json_build_array((SELECT row_to_json(o) FROM orders o WHERE trade_no=$1),(SELECT count(*) FROM ledger_entries),(SELECT count(*) FROM jobs),(SELECT count(*) FROM verified_payment_receipts),(SELECT row_to_json(a) FROM payment_authorizations a JOIN orders ao ON ao.id=a.order_id WHERE ao.trade_no=$1))::text`, trade).Scan(&after); e != nil || after != before {
		t.Fatal("read mutated financial tables", e, before, after)
	}
	s.AuthorizationObservationEnabled = false
	if w := invoke("GET", path+"/"+id, "", ""); w.Code != 503 || reads != 3 {
		t.Fatal("GET gate", w.Code, reads)
	}
}
