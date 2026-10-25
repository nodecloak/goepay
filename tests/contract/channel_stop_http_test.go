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
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestChannelStop76HTTPRealSignedFaultMandatoryRestoreGrantAndCurrentPayerBinding(t *testing.T) {
	s, p, key := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(p, false)
	s.FundsActionResolver = s.ResolveFundsAction
	user := identity.User{Role: "admin", Username: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	var writes atomic.Int32
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseForm(); e != nil {
			t.Error(e)
			return
		}
		params := map[string]string{}
		for k, v := range r.PostForm {
			if len(v) != 1 {
				t.Error("duplicate gateway field")
			}
			params[k] = v[0]
		}
		sig, e := adapter.SignAlipayRSA2(params, key)
		if e != nil || sig != params["sign"] || params["method"] != "alipay.fund.trans.toaccount.transfer" {
			t.Error("wrong original signed operation", e)
			return
		}
		writes.Add(1)
		raw := []byte(`{"code":"40004","sub_code":"PAYER_BALANCE_NOT_ENOUGH"}`)
		digest := sha256.Sum256(raw)
		signature, e := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		if e != nil {
			t.Error(e)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"alipay_fund_trans_toaccount_transfer_response": json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(signature)})
	}))
	defer up.Close()
	provider := func(seller string) *adapter.Alipay {
		a, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "local-channel76", SellerID: seller, PrivateKey: key, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client()})
		if e != nil {
			t.Fatal(e)
		}
		return a
	}
	ali := provider("2088000000000076")
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: ali}, MerchantChannels: map[int64][]int64{1: {7}}, AllowLive: true, ChannelAutoStopEnabled: true}
	if _, e = p.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true}' WHERE id=1;INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "channel76-local-credit", Amount: 2000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "channel76-local-paid"); e != nil {
		t.Fatal(e)
	}
	zero := int64(0)
	recipient, e := s.Store.SetSettlementRecipient(ctx, 1, payment.SettlementRecipientInput{Type: "alipay", Account: "local-recipient@example.test", Name: "Local Recipient"}, &zero, payment.SettlementRecipientActor{Role: "merchant", ID: 1, Name: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Store.SetSettlementSchedulePreference(ctx, 1, 0, true, recipient.ConfigID, payment.SettlementPlanActor{Role: "merchant", ID: 1, Name: "merchant"}); e != nil {
		t.Fatal(e)
	}
	var now time.Time
	if e = p.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	actor := payment.SettlementPlanActor{Role: "admin", Class: "super", ID: 1, Name: "admin"}
	plan, e := s.Store.CreateSettlementPlan(ctx, payment.SettlementPlanInput{Name: "Local channel fault", Mode: "generate_only", MerchantIDs: []int64{1}, Schedule: payment.SettlementScheduleSpec{Frequency: "hour", Interval: 1, AnchorAt: now.Truncate(time.Hour)}}, actor)
	if e != nil {
		t.Fatal(e)
	}
	guard := payment.WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { return nil })
	gen, e := s.Store.AuthorizeSettlementPlan(guard, plan.ID, plan.Revision, payment.SettlementPlanAuthorizationInput{ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), MaxItemGross: 1000, MaxRunGross: 1000, MaxDayGross: 1000, MaxDayItems: 1}, actor)
	if e != nil {
		t.Fatal(e)
	}
	run, e := s.Store.CreateSettlementPlanRun(ctx, plan.ID, plan.Revision, actor)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.GenerateSettlementPlanRun(ctx, run.ID); e != nil {
		t.Fatal(e)
	}
	runView, e := s.Store.SettlementPlanRun(ctx, run.ID, actor)
	if e != nil || len(runView.Items) != 1 || runView.Items[0].RequestID <= 0 {
		t.Fatal(runView, e)
	}
	_, e = s.ProviderFlow.AuthorizeSettlementAuto(guard, plan.ID, plan.Revision, payment.SettlementAutoInput{GenerationAuthorizationID: gen.ID, ValidFrom: gen.ValidFrom, ExpiresAt: gen.ExpiresAt, MaxItemGross: 1000, MaxRunGross: 1000, MaxDayGross: 1000, MaxDayItems: 1, CostPolicy: "provider_managed_excluded", Channels: []payment.SettlementAutoChannel{{MerchantID: 1, ChannelID: 7}}}, actor)
	if e != nil {
		t.Fatal(e)
	}
	if out, e := s.ProviderFlow.ExecuteSettlementAuto(ctx, runView.Items[0].RequestID, nil); e == nil || out.State != "unknown" || writes.Load() != 1 {
		t.Fatal("no authenticated original stop", out, e, writes.Load())
	}
	call := func(method, path, body, grant, csrfHeader string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/admin"+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("X-CSRF-Token", csrfHeader)
		r.Header.Set("X-Payment-Authorization", grant)
		r.RemoteAddr = "192.0.2.76:7600"
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := call("GET", "/channel-stops", "", "", csrf)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	items := lookupData(t, w)["items"].([]any)
	if len(items) != 1 {
		t.Fatal(items)
	}
	status := items[0].(map[string]any)
	if status["blocked"] != true || status["notice_state"] != "pending" {
		t.Fatal("wrong effective runtime state", status)
	}
	target := "/channel-stops/7/resume"
	body := fmt.Sprintf(`{"expected_event_id":"%s","expected_revision":"%s"}`, status["event_id"], status["revision"])
	if w = call("POST", target, body, "", ""); w.Code != 403 {
		t.Fatal("CSRF bypass", w.Code, w.Body.String())
	}
	if w = call("POST", target, body, "", csrf); w.Code != 428 {
		t.Fatal("global-off bypassed mandatory confirmation", w.Code, w.Body.String())
	}
	for _, bad := range []string{`{}`, strings.Replace(body, `"1"`, `1`, 1), strings.TrimSuffix(body, "}") + `,"enabled":true}`} {
		if w = call("POST", target, bad, "", csrf); w.Code != 400 {
			t.Fatal("ambiguous restore accepted", w.Code, w.Body.String())
		}
	}
	funds := identity.NewFundsAuthorization(p, nil)
	if e = funds.SetPassword(ctx, identity.FundsActor{User: user, SessionToken: token, ClientIP: "192.0.2.76"}, "contract-password-123", "", "local-channel76-funds-password", ""); e != nil {
		t.Fatal(e)
	}
	challenge, _ := json.Marshal(map[string]any{"target": "/console/v1/admin" + target, "body": json.RawMessage(body)})
	w = call("POST", "/security/funds-authorizations/challenges", string(challenge), "", csrf)
	if w.Code != 201 {
		t.Fatal("concrete stop preview", w.Code, w.Body.String())
	}
	preview := lookupData(t, w)
	summary := preview["summary"].(map[string]any)
	if summary["channel_stop_code"] != "40004:PAYER_BALANCE_NOT_ENOUGH" {
		t.Fatal("preview omitted authenticated reason", summary)
	}
	confirm, _ := json.Marshal(map[string]any{"challenge_id": preview["challenge_id"], "payment_password": "local-channel76-funds-password"})
	w = call("POST", "/security/funds-authorizations/confirm", string(confirm), "", csrf)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	grant := lookupData(t, w)["authorization"].(string)
	s.ProviderFlow.Providers[7] = provider("2088000000001076")
	w = call("POST", target, body, grant, csrf)
	if w.Code != 428 {
		t.Fatal("changed current payer inherited old restore grant", w.Code, w.Body.String())
	}
	s.ProviderFlow.Providers[7] = ali
	w = call("POST", target, body, grant, csrf)
	if w.Code != 200 || lookupData(t, w)["blocked"] != false || writes.Load() != 1 {
		t.Fatal("explicit restore failed or resent", w.Code, w.Body.String(), writes.Load())
	}
	var consumed int
	if e = p.QueryRow(ctx, `SELECT count(*) FROM admin_funds_grants WHERE consumed_at IS NOT NULL`).Scan(&consumed); e != nil || consumed != 1 {
		t.Fatal("restore grant did not consume atomically", consumed, e)
	}
	check, e := s.Store.CheckLedger(ctx)
	if e != nil || check.InvalidChannelStops != 0 || check.InvalidAccounts != 0 || check.InvalidJournals != 0 {
		t.Fatal(check, e)
	}
}
