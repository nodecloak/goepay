package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func groupFeatureHTTPFixture(t *testing.T) (channel168Fixture, string, string) {
	t.Helper()
	f := channel168Local(t)
	if _, err := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('groupfeature122','merchant',1,'Synthetic group feature','unused-synthetic-hash'); UPDATE backoffice_records SET body=body||'{"recharge":"0","onecode":"0","subchannel_open":"0","user_profitsharing":"0","user_transfer_batch":"0"}'::jsonb WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	token, csrf, err := f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "groupfeature122", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	return f, token, csrf
}

func groupFeatureHTTPRequest(f channel168Fixture, token, csrf, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/merchant/"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	out := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(out, r)
	return out
}

func TestGroupFeatureActualHTTPSessionAndOperationsAgree(t *testing.T) {
	f, token, csrf := groupFeatureHTTPFixture(t)
	out := groupFeatureHTTPRequest(f, token, csrf, "GET", "session", "")
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Features map[string]bool `json:"merchant_features"`
		} `json:"data"`
	}
	if out.Code != http.StatusOK || json.Unmarshal(out.Body.Bytes(), &envelope) != nil || envelope.Code != 0 {
		t.Fatalf("merchant session HTTP=%d", out.Code)
	}
	for _, key := range []string{"recharge", "onecode", "subchannel_open", "user_profitsharing", "user_transfer_batch"} {
		if enabled, found := envelope.Data.Features[key]; !found || enabled {
			t.Errorf("session must expose disabled effective group feature %s", key)
		}
	}
	for _, tc := range []struct{ method, path, body, message string }{
		{"GET", "collection-generator", "", "未开启聚合收款码功能"},
		{"PATCH", "collection-generator", `{"recipient_name":"synthetic group 122"}`, "未开启聚合收款码功能"},
		{"GET", "collection-styles", "", "未开启聚合收款码功能"},
		{"GET", "payment-subchannels", "", "未开启子商户支付通道功能"},
		{"POST", "transfer-batches", `{}`, "未开启批量代付功能"},
		{"POST", "transfer-batches/999/execute", `{"limit":1}`, "未开启批量代付功能"},
		{"POST", "channel-sharing/receivers", `{}`, "未开启添加分账规则功能"},
		{"POST", "workflows/recharges", `{"external_no":"group-feature122-recharge","type":"alipay","amount":"0.01"}`, "未开启余额充值功能"},
		{"POST", "resources/collection-codes", `{"name":"synthetic","style_id":"0","title":"synthetic"}`, "未开启聚合收款码功能"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			out := groupFeatureHTTPRequest(f, token, csrf, tc.method, tc.path, tc.body)
			if out.Code != http.StatusForbidden || !strings.Contains(out.Body.String(), tc.message) {
				t.Fatalf("group feature did not gate endpoint: HTTP=%d, want403 %q", out.Code, tc.message)
			}
		})
	}
	channels := groupFeatureHTTPRequest(f, token, csrf, "GET", "channel-sharing/channels", "")
	var catalog struct {
		Data struct {
			Available bool   `json:"available"`
			Reason    string `json:"reason"`
		} `json:"data"`
	}
	if channels.Code != http.StatusOK || json.Unmarshal(channels.Body.Bytes(), &catalog) != nil || catalog.Data.Available || !strings.Contains(catalog.Data.Reason, "未开启添加分账规则功能") {
		t.Fatalf("closed sharing feature did not expose unavailable catalog without hiding history: HTTP=%d", channels.Code)
	}
	var orders, attempts, codes int
	if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM provider_attempts),(SELECT count(*) FROM backoffice_records WHERE resource='collection-codes')`).Scan(&orders, &attempts, &codes); err != nil || orders != 0 || attempts != 0 || codes != 0 {
		t.Fatal("closed features created payment or collection records", orders, attempts, codes, err)
	}
}

func TestGroupFeatureActualHTTPClosedBatchPreservesOriginalRecovery(t *testing.T) {
	f, provider, credentials := redpackFixture(t)
	actor := payment.TransferBatchActor{Role: "merchant", ID: 1, Name: "redpackmerchant"}
	batch, err := f.s.ProviderFlow.PrepareTransferBatch(t.Context(), 1, "synthetic-group-feature-batch", []payment.TransferBatchInput{{ChannelID: 7, Request: adapter.TransferRequest{Type: "alipay", Account: "synthetic-group122@example.invalid", Name: "Synthetic recipient", Amount: 100, Title: "Synthetic group batch"}}}, actor)
	if err != nil || len(batch.Items) != 1 {
		t.Fatal("could not prepare synthetic unsent batch", err)
	}
	if _, err = f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=body||'{"user_transfer_batch":"0"}'::jsonb WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	base := "transfer-batches/" + strconv.FormatInt(batch.ID, 10)
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "transfer-batches", ""},
		{"GET", base, ""},
		{"POST", base + "/query", `{}`},
	} {
		out := groupFeatureHTTPRequest(f, credentials[0], credentials[1], tc.method, tc.path, tc.body)
		if out.Code != http.StatusOK {
			t.Fatalf("closed feature hid original recovery %s %s: HTTP=%d", tc.method, tc.path, out.Code)
		}
	}
	if out := groupFeatureHTTPRequest(f, credentials[0], credentials[1], "POST", base+"/execute", `{"limit":1}`); out.Code != http.StatusForbidden {
		t.Fatalf("closed feature allowed new batch execution: HTTP=%d", out.Code)
	}
	if out := groupFeatureHTTPRequest(f, credentials[0], "wrong-csrf", "POST", base+"/cancel-unsent", `{}`); out.Code != http.StatusForbidden {
		t.Fatalf("recovery bypassed original CSRF: HTTP=%d", out.Code)
	}
	if err = f.s.Store.CreateMerchant(t.Context(), payment.Merchant{ID: 2, Name: "Other synthetic merchant", APIKey: "synthetic-group-feature-other-key"}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('groupfeature-other','merchant',2,'Other synthetic','unused-synthetic-hash')`); err != nil {
		t.Fatal(err)
	}
	otherToken, otherCSRF, err := f.s.Sessions.Create(identity.User{ID: 2, Role: "merchant", Username: "groupfeature-other", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, path, body string }{{"GET", base, ""}, {"POST", base + "/query", `{}`}, {"POST", base + "/cancel-unsent", `{}`}} {
		if out := groupFeatureHTTPRequest(f, otherToken, otherCSRF, tc.method, tc.path, tc.body); out.Code != http.StatusNotFound {
			t.Fatalf("closed-feature recovery bypassed original owner: %s HTTP=%d", tc.path, out.Code)
		}
	}
	if out := groupFeatureHTTPRequest(f, credentials[0], credentials[1], "POST", base+"/cancel-unsent", `{}`); out.Code != http.StatusOK {
		t.Fatalf("closed feature prevented proven-unsent cancellation: HTTP=%d %s", out.Code, out.Body.String())
	}
	current, err := f.s.Store.TransferBatch(t.Context(), 1, batch.ID)
	if err != nil || current.Summary.Cancelled != 1 || provider.sent.Load() != 0 {
		t.Fatal("recovery sent money or did not cancel the original item", current.Summary, provider.sent.Load(), err)
	}
	balance, err := f.s.Store.Balance(t.Context(), 1)
	if err != nil || balance.Available != 10000 || balance.Reserved != 0 {
		t.Fatal("proven-unsent cancellation did not restore original synthetic reservation", balance, err)
	}
}

func TestGroupFeatureActualHTTPAdminManagementAndPublicCollectionNewPayment(t *testing.T) {
	f, token, csrf := groupFeatureHTTPFixture(t)
	r := httptest.NewRequest("POST", "/console/v1/admin/resources/collection-codes", strings.NewReader(`{"merchant_id":"1","name":"Synthetic group collection","style_id":"0","title":"Synthetic group collection"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	created := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(created, r)
	var envelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &envelope) != nil || envelope.Data.ID == "" {
		t.Fatalf("merchant group toggle improperly blocked administrator record management: HTTP=%d", created.Code)
	}
	if out := groupFeatureHTTPRequest(f, token, csrf, "GET", "collection-codes/"+envelope.Data.ID+"/link", ""); out.Code != http.StatusForbidden {
		t.Fatalf("closed merchant feature exposed a new collection link: HTTP=%d", out.Code)
	}
	expiry := strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10)
	query := url.Values{"expires": {expiry}, "token": {f.s.checkoutToken(envelope.Data.ID, "collect", expiry)}}
	public := httptest.NewRequest("POST", "/collect/"+envelope.Data.ID+"?"+query.Encode(), strings.NewReader(""))
	publicResult := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(publicResult, public)
	if publicResult.Code != http.StatusForbidden || !strings.Contains(publicResult.Body.String(), "未开启聚合收款码功能") {
		t.Fatalf("public collection bypassed current merchant group: HTTP=%d", publicResult.Code)
	}
	var orders int
	if err := f.p.QueryRow(t.Context(), `SELECT count(*) FROM orders`).Scan(&orders); err != nil || orders != 0 {
		t.Fatal("closed public collection created a new payment intent", orders, err)
	}
}

func TestGroupFeatureActualHTTPMalformedFieldKeepsUnrelatedSession(t *testing.T) {
	f, token, csrf := groupFeatureHTTPFixture(t)
	if _, err := f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=body||'{"recharge":true,"onecode":"1"}'::jsonb WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	out := groupFeatureHTTPRequest(f, token, csrf, "GET", "session", "")
	var envelope struct {
		Data struct {
			Features map[string]bool `json:"merchant_features"`
		} `json:"data"`
	}
	if out.Code != http.StatusOK || json.Unmarshal(out.Body.Bytes(), &envelope) != nil || envelope.Data.Features["recharge"] || !envelope.Data.Features["onecode"] || !envelope.Data.Features["user_totp_open"] {
		t.Fatalf("one malformed group field blocked account/security or enabled itself: HTTP=%d", out.Code)
	}
	if out := groupFeatureHTTPRequest(f, token, csrf, "POST", "workflows/recharges", `{}`); out.Code < 400 {
		t.Fatal("malformed feature did not fail closed")
	}
	if out := groupFeatureHTTPRequest(f, token, csrf, "GET", "workflows/recharges", ""); out.Code != http.StatusOK {
		t.Fatalf("malformed feature hid old recharge records: HTTP=%d", out.Code)
	}
}
