package contract_test

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestChannelSharingConsoleActualTLSFundsOwnershipAndOriginalQuery(t *testing.T) {
	s, pool, key := fixture(t)
	ctx := context.Background()
	s.Sandbox = false
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	apiKey := []byte("0123456789abcdef0123456789abcdef")
	var binds, unbinds, sends, queries, returnSends, returnQueries, pfSends, pfQueries atomic.Int32
	var pfNo, pfTrade string
	var pfReject atomic.Bool
	var pfAmount atomic.Int64
	pfAmount.Store(150)
	var returnInput map[string]any
	var original struct {
		OutNo     string `json:"out_order_no"`
		TradeNo   string `json:"transaction_id"`
		Receivers []struct {
			Type, Account string
			Amount        int64
		} `json:"receivers"`
	}
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw []byte
		switch {
		case r.URL.Path == "/v3/profitsharing/orders/unfreeze" && r.Method == "POST":
			pfSends.Add(1)
			if pfReject.Swap(false) {
				problem := []byte(`{"code":"PARAM_ERROR","message":"local synthetic rejection"}`)
				for name, values := range v3ContractHeaders(t, problem, platform) {
					w.Header()[name] = values
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(400)
				w.Write(problem)
				return
			}
			var input map[string]string
			json.NewDecoder(r.Body).Decode(&input)
			pfNo, pfTrade = input["out_order_no"], input["transaction_id"]
			raw, _ = json.Marshal(map[string]any{"out_order_no": pfNo, "transaction_id": pfTrade, "order_id": "WX_" + pfNo, "state": "FINISHED"})
		case strings.HasPrefix(r.URL.Path, "/v3/profitsharing/orders/PF"):
			pfQueries.Add(1)
			if !strings.HasSuffix(r.URL.Path, pfNo) || r.URL.Query().Get("transaction_id") != pfTrade {
				t.Error("PF changed original number")
			}
			raw, _ = json.Marshal(map[string]any{"out_order_no": pfNo, "transaction_id": pfTrade, "order_id": "WX_" + pfNo, "state": "FINISHED", "receivers": []map[string]any{{"type": "MERCHANT_ID", "account": "local-mch", "amount": pfAmount.Load(), "result": "SUCCESS", "detail_id": "LOCAL_PF_DETAIL"}}})
		case r.URL.Path == "/v3/profitsharing/return-orders" && r.Method == "POST":
			returnSends.Add(1)
			json.NewDecoder(r.Body).Decode(&returnInput)
			w.WriteHeader(503)
			return
		case strings.HasPrefix(r.URL.Path, "/v3/profitsharing/return-orders/"):
			returnQueries.Add(1)
			if r.URL.Query().Get("out_order_no") != original.OutNo {
				t.Error("return changed original sharing number")
			}
			raw, _ = json.Marshal(map[string]any{"order_id": "WX_LOCAL_PS", "out_order_no": original.OutNo, "out_return_no": returnInput["out_return_no"], "return_mchid": returnInput["return_mchid"], "return_id": "WX_LOCAL_PR", "amount": returnInput["amount"], "result": "SUCCESS"})
		case r.URL.Path == "/v3/profitsharing/receivers/add":
			binds.Add(1)
			var input map[string]json.RawMessage
			json.NewDecoder(r.Body).Decode(&input)
			raw, _ = json.Marshal(map[string]json.RawMessage{"type": input["type"], "account": input["account"], "relation_type": input["relation_type"]})
		case r.URL.Path == "/v3/profitsharing/receivers/delete":
			unbinds.Add(1)
			var input map[string]json.RawMessage
			json.NewDecoder(r.Body).Decode(&input)
			raw, _ = json.Marshal(map[string]json.RawMessage{"type": input["type"], "account": input["account"]})
		case r.URL.Path == "/v3/pay/transactions/native":
			raw = []byte(`{"code_url":"weixin://wxpay/bizpayurl?pr=local-console-test"}`)
		case r.URL.Path == "/v3/profitsharing/orders" && r.Method == "POST":
			sends.Add(1)
			if e := json.NewDecoder(r.Body).Decode(&original); e != nil {
				t.Error(e)
			}
			raw, _ = json.Marshal(map[string]any{"transaction_id": original.TradeNo, "out_order_no": original.OutNo, "order_id": "WX_LOCAL_PS", "state": "PROCESSING"})
		case strings.HasPrefix(r.URL.Path, "/v3/profitsharing/orders/"):
			queries.Add(1)
			if r.URL.Query().Get("transaction_id") != original.TradeNo || !strings.HasSuffix(r.URL.Path, original.OutNo) {
				t.Error("changed original sharing identity")
			}
			items := []map[string]any{}
			for _, item := range original.Receivers {
				items = append(items, map[string]any{"type": item.Type, "account": item.Account, "amount": item.Amount, "result": "SUCCESS", "detail_id": "WX_LOCAL_DETAIL"})
			}
			raw, _ = json.Marshal(map[string]any{"transaction_id": original.TradeNo, "out_order_no": original.OutNo, "order_id": "WX_LOCAL_PS", "state": "FINISHED", "receivers": items})
		case strings.HasSuffix(r.URL.Path, "/amounts"):
			raw, _ = json.Marshal(map[string]any{"transaction_id": original.TradeNo, "unsplit_amount": 0})
		default:
			t.Errorf("unexpected local TLS endpoint %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		for name, values := range v3ContractHeaders(t, raw, platform) {
			w.Header()[name] = values
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
	}))
	defer up.Close()
	p, e := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local-app", MerchantID: "local-mch", MerchantSerial: "MERCHANT_SERIAL", PrivateKey: key, APIv3Key: apiKey, VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_HTTP": &platform.PublicKey}, EnableChannelSharing: true, Methods: []string{"native"}, HTTPClient: &http.Client{Transport: v3ContractTLS{up}}})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.MerchantChannels = map[int64][]int64{1: {7}, 2: {7}}
	s.FundsActionResolver = s.ResolveFundsAction
	s.Sessions = identity.NewPersistentSessions(pool, false)
	if _, e = pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"admin_funds_authorization_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	user := identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	f := identity.NewFundsAuthorization(pool, nil)
	if e = f.SetPassword(ctx, identity.FundsActor{User: user, SessionToken: token, ClientIP: "192.0.2.1"}, "contract-password-123", "", "local-funds-password-456", ""); e != nil {
		t.Fatal(e)
	}
	request := func(method, target, body, grant string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		if grant != "" {
			r.Header.Set("X-Payment-Authorization", grant)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	grant := func(target, body string) string {
		t.Helper()
		preview, _ := json.Marshal(map[string]any{"target": target, "body": json.RawMessage(body)})
		w := request("POST", "/console/v1/admin/security/funds-authorizations/challenges", string(preview), "")
		if w.Code != 201 {
			t.Fatal("sharing preview", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "synthetic-private") {
			t.Fatal("plaintext leaked into funds preview")
		}
		var c struct{ Data identity.FundsChallenge }
		json.Unmarshal(w.Body.Bytes(), &c)
		w = request("POST", "/console/v1/admin/security/funds-authorizations/confirm", fmt.Sprintf(`{"challenge_id":%q,"payment_password":"local-funds-password-456"}`, c.Data.ChallengeID), "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var g struct{ Data identity.FundsGrant }
		json.Unmarshal(w.Body.Bytes(), &g)
		return g.Data.Authorization
	}
	root := "/console/v1/admin/channel-sharing"
	for _, target := range []string{root + "/orders?merchant_id=1&limit=01", root + "/receivers?merchant_id=1&cursor=00", root + "/channels?merchant_id=1&limit=20"} {
		if w := request("GET", target, "", ""); w.Code != 400 && w.Code != 422 {
			t.Fatal("noncanonical sharing query accepted", w.Code, w.Body.String())
		}
	}
	if w := request("GET", root+"/channels?merchant_id=1", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"available":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w := request("POST", root+"/receivers", `{"merchant_id":"1","channel_id":"7","external_no":"receiver-console","type":"MERCHANT_ID","account":"synthetic-private-account","name":"测试接收方","relation_type":"PARTNER"}`, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "synthetic-private-account") {
		t.Fatal("receiver draft", w.Code, w.Body.String())
	}
	var rec struct {
		Data payment.ChannelSharingReceiverView
	}
	json.Unmarshal(w.Body.Bytes(), &rec)
	merchantToken, merchantCSRF, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	merchantRequest := func(method, target, body, csrfToken string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: merchantToken})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrfToken)
		out := httptest.NewRecorder()
		s.Handler().ServeHTTP(out, r)
		return out
	}
	for _, target := range []string{"/console/v1/merchant/channel-sharing/receivers?merchant_id=1", "/console/v1/merchant/channel-sharing/receivers?merchant_id=2", "/console/v1/merchant/channel-sharing/receivers?merchant_id="} {
		if w = merchantRequest("GET", target, "", ""); w.Code != 403 {
			t.Fatal("merchant accepted owner override", w.Code, w.Body.String())
		}
	}
	if w = merchantRequest("GET", fmt.Sprintf("/console/v1/merchant/channel-sharing/receivers/%d", rec.Data.ID), "", ""); w.Code != 200 {
		t.Fatal("merchant cannot read own masked receiver", w.Code, w.Body.String())
	}
	if w = merchantRequest("POST", fmt.Sprintf("/console/v1/merchant/channel-sharing/receivers/%d/bind", rec.Data.ID), `{"external_no":"csrf-bypass"}`, ""); w.Code != 403 || binds.Load() != 0 {
		t.Fatal("CSRF bypass", w.Code, w.Body.String())
	}
	if w = merchantRequest("POST", fmt.Sprintf("/console/v1/merchant/channel-sharing/receivers/%d/bind", rec.Data.ID), `{"merchant_id":"","external_no":"scope-bypass"}`, merchantCSRF); w.Code != 403 || binds.Load() != 0 {
		t.Fatal("empty body owner bypass", w.Code, w.Body.String())
	}
	bindTarget := fmt.Sprintf("%s/receivers/%d/bind", root, rec.Data.ID)
	bindBody := `{"merchant_id":"1","external_no":"bind-console"}`
	if w = request("POST", bindTarget, bindBody, ""); w.Code != 428 || binds.Load() != 0 {
		t.Fatal("unauthorized bind", w.Code, w.Body.String())
	}
	if w = request("POST", bindTarget, bindBody, grant(bindTarget, bindBody)); w.Code != 200 || binds.Load() != 1 {
		t.Fatal("authorized bind", w.Code, w.Body.String())
	}
	ruleTarget := root + "/rules/7"
	ruleBody := fmt.Sprintf(`{"merchant_id":"1","expected":"0","enabled":true,"minimum_amount":"0","methods":["native"],"items":[{"receiver_id":"%d","mode":"rate","rate_bps":"2000","fixed_amount":"0"}]}`, rec.Data.ID)
	if w = request("POST", ruleTarget, ruleBody, ""); w.Code != 428 {
		t.Fatal("unauthorized rule", w.Code, w.Body.String())
	}
	if w = request("POST", ruleTarget, ruleBody, grant(ruleTarget, ruleBody)); w.Code != 200 {
		t.Fatal("authorized rule", w.Code, w.Body.String())
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "console-original-payment", Type: "wxpay", Amount: 100, Name: "local console sharing"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProviderFlow.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e != nil {
		t.Fatal(e)
	}
	transaction, _ := json.Marshal(map[string]any{"appid": "local-app", "mchid": "local-mch", "out_trade_no": o.TradeNo, "transaction_id": "WXC" + o.TradeNo, "trade_state": "SUCCESS", "amount": map[string]any{"total": 100, "currency": "CNY"}, "payer": map[string]string{"openid": "local-console-buyer"}})
	block, _ := aes.NewCipher(apiKey)
	gcm, _ := cipher.NewGCM(block)
	nonce := []byte("0123456789ab")
	encrypted := gcm.Seal(nil, nonce, transaction, []byte("transaction"))
	callback, _ := json.Marshal(map[string]any{"id": "CONSOLE_EVENT", "event_type": "TRANSACTION.SUCCESS", "resource_type": "encrypt-resource", "resource": map[string]string{"original_type": "transaction", "algorithm": "AEAD_AES_256_GCM", "nonce": string(nonce), "associated_data": "transaction", "ciphertext": base64.StdEncoding.EncodeToString(encrypted)}})
	r := httptest.NewRequest("POST", "/callbacks/providers/7/"+o.TradeNo, bytes.NewReader(callback))
	r.Header = v3ContractHeaders(t, callback, platform)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal("local signed callback", w.Code, w.Body.String())
	}
	psBody := fmt.Sprintf(`{"merchant_id":"1","trade_no":%q,"external_no":"console-stable-ps"}`, o.TradeNo)
	if w = request("POST", root+"/orders", psBody, ""); w.Code != 428 || sends.Load() != 0 {
		t.Fatal("unauthorized sharing", w.Code, w.Body.String())
	}
	w = request("POST", root+"/orders", psBody, grant(root+"/orders", psBody))
	if w.Code != 200 || sends.Load() != 1 {
		t.Fatal("authorized sharing", w.Code, w.Body.String())
	}
	// A challenge binds the reviewed facts but does not grant eligibility. The
	// actual core must reject this active PS before consuming or sending a PF.
	activePFBody := fmt.Sprintf(`{"merchant_id":"1","trade_no":%q,"external_no":"active-ps-must-not-finish"}`, o.TradeNo)
	activePFGrant := grant(root+"/finishes", activePFBody)
	var consumedBefore, consumedAfter int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM admin_funds_grants WHERE consumed_at IS NOT NULL").Scan(&consumedBefore); e != nil {
		t.Fatal(e)
	}
	if w = request("POST", root+"/finishes", activePFBody, activePFGrant); w.Code != 422 || pfSends.Load() != 0 {
		t.Fatal("PF eligibility bypassed active PS", w.Code, w.Body.String())
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM admin_funds_grants WHERE consumed_at IS NOT NULL").Scan(&consumedAfter); e != nil || consumedAfter != consumedBefore {
		t.Fatal("ineligible PF consumed reviewed grant", consumedBefore, consumedAfter, e)
	}
	if w = request("POST", root+"/orders/console-stable-ps/query", `{"merchant_id":"1"}`, ""); w.Code != 200 || queries.Load() != 1 || !strings.Contains(w.Body.String(), `"funds_released":true`) {
		t.Fatal("sharing original query", w.Code, w.Body.String())
	}
	if w = request("GET", root+"/orders/console-stable-ps?merchant_id=2", "", ""); w.Code != 404 {
		t.Fatal("cross tenant order read", w.Code, w.Body.String())
	}
	if w = request("GET", fmt.Sprintf("%s/receivers/%d?merchant_id=2", root, rec.Data.ID), "", ""); w.Code != 404 {
		t.Fatal("cross tenant receiver read", w.Code, w.Body.String())
	}
	if w = request("POST", root+"/orders", `{"merchant_id":"1","trade_no":"a","trade_no":"b","external_no":"duplicate"}`, ""); w.Code != 400 {
		t.Fatal("duplicate JSON accepted", w.Code, w.Body.String())
	}
	for _, bad := range []string{`{"merchant_id":"1","trade_no":"a","external_no":"x","account":"leak"}`, `{"merchant_id":"1","trade_no":"a","external_no":"x","enabled":true}`} {
		if w = request("POST", root+"/orders", bad, ""); w.Code != 400 {
			t.Fatal("irrelevant field accepted", w.Code, w.Body.String())
		}
	}
	b, e := s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 80 || b.Reserved != 0 {
		t.Fatal("original sharing accounting", b, e)
	}
	returnBody := fmt.Sprintf(`{"merchant_id":"1","sharing_external_no":"console-stable-ps","receiver_id":"%d","external_no":"console-stable-pr","amount":"10"}`, rec.Data.ID)
	if w = request("POST", root+"/returns", returnBody, ""); w.Code != 428 || returnSends.Load() != 0 {
		t.Fatal("unauthorized return", w.Code, w.Body.String())
	}
	if w = request("POST", root+"/returns", returnBody, grant(root+"/returns", returnBody)); w.Code != 409 || returnSends.Load() != 1 {
		t.Fatal("unknown return", w.Code, w.Body.String())
	}
	if w = request("GET", root+"/returns/console-stable-pr?merchant_id=1", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"unknown"`) {
		t.Fatal("lost return response cannot read original", w.Code, w.Body.String())
	}
	if w = request("POST", root+"/returns", returnBody, ""); w.Code != 200 || returnSends.Load() != 1 {
		t.Fatal("same return replay resent or consumed fresh grant", w.Code, w.Body.String())
	}
	if w = request("POST", root+"/returns/console-stable-pr/query", `{"merchant_id":"1"}`, ""); w.Code != 200 || returnQueries.Load() != 1 || !strings.Contains(w.Body.String(), `"state":"succeeded"`) {
		t.Fatal("original return query", w.Code, w.Body.String())
	}
	if w = request("GET", root+"/returns/console-stable-pr?merchant_id=2", "", ""); w.Code != 404 {
		t.Fatal("cross owner return read", w.Code, w.Body.String())
	}
	b, e = s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 90 || b.Reserved != 0 {
		t.Fatal("return original merchant credit", b, e)
	}
	if w = request("GET", root+"/finish-preview/"+o.TradeNo+"?merchant_id=1", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"source":"sharing"`) || pfSends.Load() != 0 {
		t.Fatal("existing auto-finish was not read as original sharing", w.Code, w.Body.String())
	}
	pfOrder, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "console-independent-finish", Type: "wxpay", Amount: 150, Name: "local independent finish"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProviderFlow.Submit(ctx, pfOrder.TradeNo, 7, "native", "127.0.0.1", ""); e != nil {
		t.Fatal(e)
	}
	pfTransaction, _ := json.Marshal(map[string]any{"appid": "local-app", "mchid": "local-mch", "out_trade_no": pfOrder.TradeNo, "transaction_id": "WXC" + pfOrder.TradeNo, "trade_state": "SUCCESS", "amount": map[string]any{"total": 150, "currency": "CNY"}, "payer": map[string]string{"openid": "local-console-buyer"}})
	pfEncrypted := gcm.Seal(nil, nonce, pfTransaction, []byte("transaction"))
	pfCallback, _ := json.Marshal(map[string]any{"id": "CONSOLE_PF_EVENT", "event_type": "TRANSACTION.SUCCESS", "resource_type": "encrypt-resource", "resource": map[string]string{"original_type": "transaction", "algorithm": "AEAD_AES_256_GCM", "nonce": string(nonce), "associated_data": "transaction", "ciphertext": base64.StdEncoding.EncodeToString(pfEncrypted)}})
	r = httptest.NewRequest("POST", "/callbacks/providers/7/"+pfOrder.TradeNo, bytes.NewReader(pfCallback))
	r.Header = v3ContractHeaders(t, pfCallback, platform)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal("PF original signed callback", w.Code, w.Body.String())
	}
	if w = request("GET", root+"/finish-preview/"+pfOrder.TradeNo+"?merchant_id=1", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"eligible"`) {
		t.Fatal("PF original preview", w.Code, w.Body.String())
	}
	pfBody := fmt.Sprintf(`{"merchant_id":"1","trade_no":%q,"external_no":"console-stable-pf"}`, pfOrder.TradeNo)
	if w = request("POST", root+"/finishes", pfBody, ""); w.Code != 428 || pfSends.Load() != 0 {
		t.Fatal("unauthorized PF", w.Code, w.Body.String())
	}
	if w = request("POST", root+"/finishes", pfBody, grant(root+"/finishes", pfBody)); w.Code != 200 || pfSends.Load() != 1 || !strings.Contains(w.Body.String(), `"state":"pending"`) {
		t.Fatal("PF acceptance", w.Code, w.Body.String())
	}
	b, e = s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 90 || b.Reserved != 150 {
		t.Fatal("PF acceptance released funds", b, e)
	}
	if w = request("POST", root+"/finishes/console-stable-pf/query", `{"merchant_id":"1"}`, ""); w.Code != 200 || pfQueries.Load() != 1 || !strings.Contains(w.Body.String(), `"funds_released":true`) {
		t.Fatal("PF original terminal query", w.Code, w.Body.String())
	}
	if w = request("GET", root+"/finishes/console-stable-pf?merchant_id=2", "", ""); w.Code != 404 {
		t.Fatal("cross owner PF read", w.Code, w.Body.String())
	}
	b, e = s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 240 || b.Reserved != 0 {
		t.Fatal("PF verified release wrong", b, e)
	}
	retryOrder, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "console-explicit-finish-retry", Type: "wxpay", Amount: 70, Name: "local retry"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProviderFlow.Submit(ctx, retryOrder.TradeNo, 7, "native", "127.0.0.1", ""); e != nil {
		t.Fatal(e)
	}
	retryTransaction, _ := json.Marshal(map[string]any{"appid": "local-app", "mchid": "local-mch", "out_trade_no": retryOrder.TradeNo, "transaction_id": "WXC" + retryOrder.TradeNo, "trade_state": "SUCCESS", "amount": map[string]any{"total": 70, "currency": "CNY"}, "payer": map[string]string{"openid": "local-console-buyer"}})
	retryEncrypted := gcm.Seal(nil, nonce, retryTransaction, []byte("transaction"))
	retryCallback, _ := json.Marshal(map[string]any{"id": "CONSOLE_PF_RETRY_EVENT", "event_type": "TRANSACTION.SUCCESS", "resource_type": "encrypt-resource", "resource": map[string]string{"original_type": "transaction", "algorithm": "AEAD_AES_256_GCM", "nonce": string(nonce), "associated_data": "transaction", "ciphertext": base64.StdEncoding.EncodeToString(retryEncrypted)}})
	r = httptest.NewRequest("POST", "/callbacks/providers/7/"+retryOrder.TradeNo, bytes.NewReader(retryCallback))
	r.Header = v3ContractHeaders(t, retryCallback, platform)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	retryOriginalBody := fmt.Sprintf(`{"merchant_id":"1","trade_no":%q,"external_no":"console-failed-pf"}`, retryOrder.TradeNo)
	pfReject.Store(true)
	if w = request("POST", root+"/finishes", retryOriginalBody, grant(root+"/finishes", retryOriginalBody)); w.Code != 422 || pfSends.Load() != 2 {
		t.Fatal("PF definite signed rejection", w.Code, w.Body.String())
	}
	if w = request("POST", root+"/finishes", retryOriginalBody, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"failed"`) || pfSends.Load() != 2 {
		t.Fatal("old PF rejection replay resent", w.Code, w.Body.String())
	}
	if w = request("GET", root+"/finish-preview/"+retryOrder.TradeNo+"?merchant_id=1", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"eligible"`) || strings.Contains(w.Body.String(), `"previous_failed_id":"0"`) {
		t.Fatal("credible PF failure cannot explicitly create new intent", w.Code, w.Body.String())
	}
	retryNewBody := fmt.Sprintf(`{"merchant_id":"1","trade_no":%q,"external_no":"console-new-explicit-pf"}`, retryOrder.TradeNo)
	if w = request("POST", root+"/finishes", retryNewBody, ""); w.Code != 428 || pfSends.Load() != 2 {
		t.Fatal("PF retry omitted fresh grant", w.Code, w.Body.String())
	}
	pfAmount.Store(70)
	if w = request("POST", root+"/finishes", retryNewBody, grant(root+"/finishes", retryNewBody)); w.Code != 200 || pfSends.Load() != 3 {
		t.Fatal("explicit PF retry with history snapshot", w.Code, w.Body.String())
	}
	if w = request("POST", root+"/finishes/console-new-explicit-pf/query", `{"merchant_id":"1"}`, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"funds_released":true`) {
		t.Fatal("PF retry terminal query", w.Code, w.Body.String())
	}
	b, e = s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 310 || b.Reserved != 0 {
		t.Fatal("PF retry original reserve", b, e)
	}
	w = request("GET", ruleTarget+"?merchant_id=1", "", "")
	var currentRule struct {
		Data payment.ChannelSharingRuleView
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &currentRule) != nil {
		t.Fatal("read original rule", w.Code, w.Body.String())
	}
	disableBody := fmt.Sprintf(`{"merchant_id":"1","expected":"%d"}`, currentRule.Data.ID)
	if w = request("POST", ruleTarget+"/disable", disableBody, ""); w.Code != 428 {
		t.Fatal("unauthorized disable", w.Code, w.Body.String())
	}
	if w = request("POST", ruleTarget+"/disable", disableBody, grant(ruleTarget+"/disable", disableBody)); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal("authorized disable", w.Code, w.Body.String())
	}
	unbindTarget := fmt.Sprintf("%s/receivers/%d/unbind", root, rec.Data.ID)
	unbindBody := `{"merchant_id":"1","external_no":"unbind-console"}`
	if w = request("POST", unbindTarget, unbindBody, ""); w.Code != 428 || unbinds.Load() != 0 {
		t.Fatal("unauthorized unbind", w.Code, w.Body.String())
	}
	if w = request("POST", unbindTarget, unbindBody, grant(unbindTarget, unbindBody)); w.Code != 200 || unbinds.Load() != 1 {
		t.Fatal("authorized unbind after completed PF", w.Code, w.Body.String())
	}
	check, e := s.Store.CheckLedger(ctx)
	if e != nil || check.InvalidAccounts != 0 || check.InvalidJournals != 0 || check.InvalidChannelSharingFundings != 0 || check.InvalidChannelSharingReturns != 0 || check.InvalidChannelSharingFinishes != 0 {
		t.Fatal(check, e)
	}
}
