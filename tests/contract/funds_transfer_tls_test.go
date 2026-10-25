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
	"sync"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestAdminFundsAuthorizationActualTLSUnknownSingleSendAndQuery(t *testing.T) {
	s, p, appKey := fixture(t)
	ctx := context.Background()
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	var sends, queries atomic.Int32
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseForm(); e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		params := map[string]string{}
		for k, v := range r.Form {
			if len(v) != 1 {
				t.Error("duplicate upstream parameter")
			}
			params[k] = v[0]
		}
		signature, e := base64.StdEncoding.DecodeString(params["sign"])
		digest := sha256.Sum256([]byte(adapter.AlipayRequestCanonical(params)))
		if e != nil || rsa.VerifyPKCS1v15(&appKey.PublicKey, crypto.SHA256, digest[:], signature) != nil {
			t.Error("invalid actual upstream request signature")
			w.WriteHeader(400)
			return
		}
		var biz map[string]string
		if e = json.Unmarshal([]byte(params["biz_content"]), &biz); e != nil {
			t.Error(e)
			return
		}
		if params["method"] == "alipay.fund.trans.toaccount.transfer" {
			sends.Add(1)
			if biz["amount"] != "1.25" || biz["payee_account"] != "synthetic-12345678" {
				t.Error("changed transfer", biz)
			}
			w.WriteHeader(503)
			return
		}
		if params["method"] != "alipay.fund.trans.common.query" {
			t.Error("unexpected provider method", params["method"])
			w.WriteHeader(400)
			return
		}
		queries.Add(1)
		node, _ := json.Marshal(map[string]string{"code": "10000", "out_biz_no": biz["out_biz_no"], "order_id": "local-transfer-proof", "status": "SUCCESS", "trans_amount": "1.25", "pay_date": "2026-10-23 10:00:00"})
		d := sha256.Sum256(node)
		sig, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, d[:])
		json.NewEncoder(w).Encode(map[string]any{"alipay_fund_trans_common_query_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(sig)})
	}))
	defer up.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "local-app", PrivateKey: appKey, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client()})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: ali}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	s.MerchantChannels = map[int64][]int64{1: {7}}
	s.FundsActionResolver = s.ResolveFundsAction
	s.Sessions = identity.NewPersistentSessions(p, false)
	if _, e = p.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"0"}' WHERE id=1; INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"admin_funds_authorization_enabled","value":"true"}'),('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "funds-tls-source", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "funds-tls-source"); e != nil {
		t.Fatal(e)
	}
	user := identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	f := identity.NewFundsAuthorization(p, nil)
	if e = f.SetPassword(ctx, identity.FundsActor{User: user, SessionToken: token, ClientIP: "192.0.2.1"}, "contract-password-123", "", "local-funds-password-456", ""); e != nil {
		t.Fatal(e)
	}
	req := func(target, body, grant string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("X-Payment-Authorization", grant)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	target := "/console/v1/admin/transfers"
	body := `{"merchant_id":"1","channel_id":"7","type":"alipay","account":"synthetic-12345678","name":"测试姓名","amount":"1.25","remark":"local-only","external_no":"stable-tls-intent"}`
	if w := req(target, body, ""); w.Code != 428 {
		t.Fatal("sent without confirmation", w.Code, w.Body.String())
	}
	if sends.Load() != 0 {
		t.Fatal("unauthorized upstream call")
	}
	preview, _ := json.Marshal(map[string]any{"target": target, "body": json.RawMessage(body)})
	w := req("/console/v1/admin/security/funds-authorizations/challenges", string(preview), "")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var c struct {
		Data struct {
			identity.FundsChallenge
			Summary map[string]string
		}
	}
	json.Unmarshal(w.Body.Bytes(), &c)
	if c.Data.Summary["recipient"] != "***5678" || c.Data.Summary["recipient_name"] != "测***" || c.Data.Summary["gross_amount"] != "1.25" || c.Data.Summary["balance_debit"] != "1.25" {
		t.Fatal("unreviewable or unmasked summary", c.Data.Summary)
	}
	w = req("/console/v1/admin/security/funds-authorizations/confirm", fmt.Sprintf(`{"challenge_id":%q,"payment_password":"local-funds-password-456"}`, c.Data.ChallengeID), "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var g struct{ Data identity.FundsGrant }
	json.Unmarshal(w.Body.Bytes(), &g)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out := req(target, body, g.Data.Authorization)
			if out.Code != 200 && out.Code != 202 {
				t.Errorf("same authorized intent %d %s", out.Code, out.Body.String())
			}
		}()
	}
	wg.Wait()
	if sends.Load() != 1 {
		t.Fatal("upstream duplicate", sends.Load())
	}
	transfer, e := s.Store.Transfer(ctx, 1, "stable-tls-intent")
	if e != nil || transfer.Status != "unknown" {
		t.Fatal(transfer, e)
	}
	b, e := s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 875 || b.Reserved != 125 {
		t.Fatal("unknown reserve lost", b, e)
	}
	w = req(fmt.Sprintf("/console/v1/admin/transfers/%d/query", transfer.ID), `{}`, "")
	if w.Code != 200 || queries.Load() != 1 {
		t.Fatal("query incorrectly requires password", w.Code, w.Body.String())
	}
	b, e = s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 875 || b.Reserved != 0 {
		t.Fatal("verified query failed", b, e)
	}
	var consumed int
	if e = p.QueryRow(ctx, "SELECT count(*) FROM admin_funds_grants WHERE consumed_at IS NOT NULL").Scan(&consumed); e != nil || consumed != 1 {
		t.Fatal(consumed, e)
	}
}
