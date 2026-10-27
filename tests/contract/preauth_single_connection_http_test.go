package contract_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/server"
)

type preauthSingleHTTP75Fixture struct {
	s                             *server.Server
	observer, business            *pgxpool.Pool
	role, token, csrf, trade      string
	user                          identity.User
	freezeReads, paymentReads     atomic.Int64
	remainingReads, unfreezeCalls atomic.Int64
	bad                           atomic.Int64
	unfreezeRequest               atomic.Value
	upstream                      *httptest.Server
}

func preauthSingleHTTP75Local(t *testing.T, role string) *preauthSingleHTTP75Fixture {
	t.Helper()
	s, pool, appKey := fixture(t)
	f := &preauthSingleHTTP75Fixture{s: s, observer: pool, business: pool, role: role}
	s.Sandbox = false
	s.AuthorizationObservationEnabled = true
	s.Sessions = identity.NewPersistentSessions(pool, false)
	s.FundsActionResolver = s.ResolveFundsAction
	platform, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f.upstream = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			f.bad.Add(1)
			http.Error(w, "synthetic invalid form", 400)
			return
		}
		params := map[string]string{}
		for key, values := range r.PostForm {
			if len(values) != 1 {
				f.bad.Add(1)
			}
			params[key] = values[0]
		}
		signature, err := adapter.SignAlipayRSA2(params, appKey)
		if err != nil || signature != params["sign"] {
			f.bad.Add(1)
		}
		var body map[string]any
		if json.Unmarshal([]byte(params["biz_content"]), &body) != nil {
			f.bad.Add(1)
		}
		var field string
		var node map[string]any
		switch params["method"] {
		case "alipay.trade.query":
			f.paymentReads.Add(1)
			field = "alipay_trade_query_response"
			node = map[string]any{"code": "40004", "sub_code": "ACQ.TRADE_NOT_EXIST"}
		case "alipay.fund.auth.operation.detail.query":
			field = "alipay_fund_auth_operation_detail_query_response"
			request, _ := body["out_request_no"].(string)
			amount, rest, operation := "10.00", "4.50", "FREEZE"
			if strings.HasPrefix(request, "UA") {
				f.remainingReads.Add(1)
				if original, _ := f.unfreezeRequest.Load().(string); request != original {
					f.bad.Add(1)
				}
				amount, rest, operation = "4.50", "0.00", "UNFREEZE"
			} else {
				f.freezeReads.Add(1)
				if request != f.trade {
					f.bad.Add(1)
				}
			}
			if body["out_order_no"] != f.trade {
				f.bad.Add(1)
			}
			node = map[string]any{"code": "10000", "out_order_no": f.trade, "out_request_no": request, "auth_no": "single-http-auth75", "payer_user_id": "2088000000000075", "amount": amount, "rest_amount": rest, "status": "SUCCESS", "order_status": "AUTHORIZED", "operation_type": operation}
		case "alipay.fund.auth.order.unfreeze":
			f.unfreezeCalls.Add(1)
			request, _ := body["out_request_no"].(string)
			f.unfreezeRequest.Store(request)
			if !strings.HasPrefix(request, "UA") || body["auth_no"] != "single-http-auth75" || body["amount"] != "4.50" {
				f.bad.Add(1)
			}
			// A real signed adapter POST with a lost response must stay unknown
			// until the original UA request is queried. Never fabricate an ACK.
			w.WriteHeader(503)
			return
		default:
			f.bad.Add(1)
			http.Error(w, "synthetic unexpected operation", 500)
			return
		}
		raw, _ := json.Marshal(node)
		digest := sha256.Sum256(raw)
		signed, err := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		if err != nil {
			f.bad.Add(1)
			http.Error(w, "synthetic signing failure", 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{field: json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(signed)})
	}))
	t.Cleanup(f.upstream.Close)
	ali, err := adapter.NewAlipay(adapter.AlipayConfig{AppID: "2021000000000075", SellerID: "2088000000000075", PrivateKey: appKey, PublicKey: &platform.PublicKey, Methods: []string{"preauth"}, HTTPClient: &http.Client{Transport: oauthContractTLSTransport{f.upstream, t}}})
	if err != nil {
		t.Fatal(err)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.MerchantChannels = map[int64][]int64{1: {7}}
	o, err := s.Store.CreateOrder(t.Context(), payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "single-connection-http75", Name: "synthetic remaining amount", Type: "alipay", Amount: 1000})
	if err != nil {
		t.Fatal(err)
	}
	f.trade = o.TradeNo
	scope, err := adapter.TransferAccountScope(ali)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Store.BeginProviderAttemptWithAccount(t.Context(), f.trade, 7, "preauth", ali.ApplicationID(), "", scope); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.RecordPreauthorization(t.Context(), 7, adapter.AuthorizationEvent{TradeNo: f.trade, RequestNo: f.trade, AuthNo: "single-http-auth75", BuyerID: "2088000000000075", Amount: 1000, State: "SUCCESS"}); err != nil {
		t.Fatal(err)
	}
	f.user = identity.User{Username: role, Role: role, ID: 1, AuthVersion: 1}
	if role == "admin" {
		f.user.AdminClass = "super"
	}
	f.token, f.csrf, err = s.Sessions.Create(f.user)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *preauthSingleHTTP75Fixture) useSingleBusinessConnection(t *testing.T) {
	t.Helper()
	config := f.observer.Config()
	config.MinConns, config.MaxConns = 0, 1
	config.ConnConfig.RuntimeParams["application_name"] = "preauth75-single-business"
	one, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(one.Close)
	store, err := payment.NewWithKey(one, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	f.business, f.s.Pool, f.s.Store = one, one, store
	f.s.ProviderFlow.Store = store
	f.s.Sessions = identity.NewPersistentSessions(one, false)
	// FundsActionResolver is bound to the same Server pointer and receives
	// the caller's transaction. The old pool is only a test observer now.
	if f.business.Config().MaxConns != 1 {
		t.Fatal("fixture did not restrict every business operation to one connection")
	}
}

func (f *preauthSingleHTTP75Fixture) call(t *testing.T, method, suffix, body, grant, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	r := httptest.NewRequest(method, "/console/v1/"+f.role+suffix, strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	if grant != "" {
		r.Header.Set("X-Payment-Authorization", grant)
	}
	r.AddCookie(&http.Cookie{Name: "goepay_" + f.role, Value: f.token})
	r.RemoteAddr = "192.0.2.75:5075"
	w := httptest.NewRecorder()
	started := time.Now()
	f.s.Handler().ServeHTTP(w, r)
	if elapsed := time.Since(started); elapsed > 8*time.Second {
		t.Fatalf("HTTP request ignored its deadline or retained a nested transaction: %s", elapsed)
	}
	return w
}

func (f *preauthSingleHTTP75Fixture) observe(t *testing.T) string {
	t.Helper()
	w := f.call(t, http.MethodPost, "/orders/"+f.trade+"/authorization-observations", `{"client_request_no":"`+strings.Repeat("a", 64)+`"}`, "", f.csrf)
	if w.Code != http.StatusOK {
		t.Fatalf("actual signed HTTP observation must complete without nested acquisition: HTTP=%d body=%s freeze_queries=%d", w.Code, w.Body.String(), f.freezeReads.Load())
	}
	data := lookupData(t, w)
	id, _ := data["request_id"].(string)
	if len(id) != 64 || data["source_verified"] != true || data["status"] != "verified" || data["amount"] != "1000" || data["rest_amount"] != "450" || f.freezeReads.Load() != 1 || f.unfreezeCalls.Load() != 0 || f.bad.Load() != 0 {
		t.Fatal("signed observation lost its original identity/amount or performed a financial write", data)
	}
	return id
}

func (f *preauthSingleHTTP75Fixture) fundsGrant(t *testing.T, target, body string) string {
	t.Helper()
	if f.role != "admin" {
		return ""
	}
	if w := f.call(t, http.MethodPost, target, body, "", f.csrf); w.Code != http.StatusPreconditionRequired || f.unfreezeCalls.Load() != 0 {
		t.Fatal("mandatory independent administrator confirmation was bypassed", w.Code, w.Body.String())
	}
	password := "single-connection-funds-password75"
	funds := identity.NewFundsAuthorization(f.business, nil)
	if err := funds.SetPassword(t.Context(), identity.FundsActor{User: f.user, SessionToken: f.token, ClientIP: "192.0.2.75"}, "contract-password-123", "", password, ""); err != nil {
		t.Fatal(err)
	}
	challenge, _ := json.Marshal(map[string]any{"target": "/console/v1/admin" + target, "body": json.RawMessage(body)})
	w := f.call(t, http.MethodPost, "/security/funds-authorizations/challenges", string(challenge), "", f.csrf)
	if w.Code != http.StatusCreated {
		t.Fatal("real administrator funds preview failed", w.Code, w.Body.String())
	}
	id := lookupData(t, w)["challenge_id"]
	confirmation, _ := json.Marshal(map[string]any{"challenge_id": id, "payment_password": password})
	w = f.call(t, http.MethodPost, "/security/funds-authorizations/confirm", string(confirmation), "", f.csrf)
	if w.Code != http.StatusOK {
		t.Fatal("real administrator funds confirmation failed", w.Code, w.Body.String())
	}
	grant, _ := lookupData(t, w)["authorization"].(string)
	if grant == "" {
		t.Fatal("real funds confirmation omitted its original grant")
	}
	return grant
}

func TestPreauthObservation68ActualHTTPSignedCompleteFlowUsesSingleBusinessConnection(t *testing.T) {
	for _, role := range []string{"admin", "merchant"} {
		t.Run(role, func(t *testing.T) {
			f := preauthSingleHTTP75Local(t, role)
			f.useSingleBusinessConnection(t)
			id := f.observe(t)
			base := "/orders/" + f.trade + "/authorization-observations"
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				path, body := base+"/"+id, ""
				if method == http.MethodPost {
					path, body = base, `{"client_request_no":"`+strings.Repeat("a", 64)+`"}`
				}
				w := f.call(t, method, path, body, "", f.csrf)
				if w.Code != http.StatusOK || lookupData(t, w)["request_id"] != id || f.freezeReads.Load() != 1 {
					t.Fatal("read-only original observation replay deadlocked or repeated the signed upstream query", method, w.Code, w.Body.String(), f.freezeReads.Load())
				}
			}
			var journals, receipts, operations int
			if err := f.observer.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM journals),(SELECT count(*) FROM verified_payment_receipts),(SELECT count(*) FROM provider_operations)`).Scan(&journals, &receipts, &operations); err != nil || journals != 0 || receipts != 0 || operations != 0 || f.unfreezeCalls.Load() != 0 || f.bad.Load() != 0 {
				t.Fatal("observation fabricated a payment, funds operation, or release", journals, receipts, operations, err)
			}
		})
	}
}

func TestPreauthRemaining75ActualHTTPAdminMerchantUnfreezeUsesSingleBusinessConnectionAndOriginalGuards(t *testing.T) {
	for _, role := range []string{"admin", "merchant"} {
		t.Run(role, func(t *testing.T) {
			f := preauthSingleHTTP75Local(t, role)
			// Establish authentic observation and administrator confirmation on
			// the normal pool, so the release test independently reaches the
			// nested guard chain even while the complete one-connection
			// observation flow above remains RED.
			id := f.observe(t)
			target := "/orders/" + f.trade + "/release-remaining-authorization"
			body := fmt.Sprintf(`{"expected_observation_id":%q,"expected_remaining_cents":"450","expected_source_revision":"0"}`, id)
			if w := f.call(t, http.MethodPost, target, body, "", ""); w.Code != http.StatusForbidden || f.unfreezeCalls.Load() != 0 {
				t.Fatal("original session CSRF protection was bypassed", w.Code, w.Body.String())
			}
			grant := f.fundsGrant(t, target, body)
			f.useSingleBusinessConnection(t)
			w := f.call(t, http.MethodPost, target, body, grant, f.csrf)
			if w.Code != http.StatusAccepted || f.unfreezeCalls.Load() != 1 {
				t.Fatalf("actual %s release must pass both original guards without a nested-connection wait: HTTP=%d body=%s unfreezes=%d freeze_queries=%d payment_queries=%d", role, w.Code, w.Body.String(), f.unfreezeCalls.Load(), f.freezeReads.Load(), f.paymentReads.Load())
			}
			data := lookupData(t, w)
			if data["sent"] != true || data["amount"] != "450" || data["original_amount"] != "1000" || data["state"] != "unknown" || data["source_revision"] != "1" || f.bad.Load() != 0 {
				t.Fatal("release lost its independent source, exact remaining amount, or unknown claim", data)
			}
			// Revoking new writes never prevents querying the committed UA.
			f.s.ProviderFlow.AllowLive = false
			w = f.call(t, http.MethodPost, "/orders/"+f.trade+"/query-authorization-release", `{}`, "", f.csrf)
			if w.Code != http.StatusOK || lookupData(t, w)["state"] != "succeeded" || f.unfreezeCalls.Load() != 1 || f.remainingReads.Load() != 1 || f.bad.Load() != 0 {
				t.Fatal("original one-connection read recovery was blocked or resent the unfreeze", w.Code, w.Body.String(), f.unfreezeCalls.Load(), f.remainingReads.Load())
			}
			var journals, claims int
			if err := f.observer.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM journals),(SELECT count(*) FROM authorization_remaining_claims)`).Scan(&journals, &claims); err != nil || journals != 0 || claims != 1 {
				t.Fatal("buyer unfreeze created merchant money or more than one send claim", journals, claims, err)
			}
		})
	}
}
