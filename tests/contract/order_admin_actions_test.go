package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrderAdminUXStatusMarkArchiveRestoreKeepFinancialFacts(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "admin-order-ux", Name: "item", Type: "alipay", Amount: 125})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "admin-ux-paid"); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1, AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	invoke := func(method, path, body, csrfValue string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(method, "/console/v1/admin"+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		r.Header.Set("X-CSRF-Token", csrfValue)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(w.Body.String(), e)
		}
		return w.Code, out
	}
	path := "/orders/" + o.TradeNo
	body := `{"status":"pending","external_no":"status-one","reason":"后台人工标记","expected_revision":"0"}`
	if code, _ := invoke("POST", path+"/admin-status", body, ""); code != 403 {
		t.Fatal("missing CSRF", code)
	}
	code, out := invoke("POST", path+"/admin-status", body, csrf)
	if code != 200 {
		t.Fatal("missing status operation", code, out)
	}
	control := out["data"].(map[string]any)
	rev := control["revision"].(string)
	if control["status"] != "pending" || rev == "0" {
		t.Fatal(control)
	}
	if code, out = invoke("POST", path+"/admin-status", body, csrf); code != 200 {
		t.Fatal("same request not idempotent", code, out)
	}
	if code, _ = invoke("POST", path+"/admin-status", strings.Replace(body, "status-one", "status-stale", 1), csrf); code != 409 {
		t.Fatal("stale revision accepted", code)
	}
	current, _ := s.Store.OrderByTradeNo(ctx, o.TradeNo)
	balance, _ := s.Store.Balance(ctx, 1)
	if current.Status != "paid" || balance.Available != 125 {
		t.Fatal("backoffice mark overwrote money facts", current.Status, balance)
	}
	code, out = invoke("GET", "/orders?status=pending", "", csrf)
	if code != 200 || out["data"].(map[string]any)["total"].(float64) != 1 {
		t.Fatal("operational status filter", code, out)
	}
	archive := `{"external_no":"archive-one","reason":"后台逻辑删除","expected_revision":"` + rev + `"}`
	code, out = invoke("POST", path+"/archive", archive, csrf)
	if code != 200 {
		t.Fatal(code, out)
	}
	rev = out["data"].(map[string]any)["revision"].(string)
	code, out = invoke("GET", "/orders", "", csrf)
	if code != 200 || out["data"].(map[string]any)["total"].(float64) != 0 {
		t.Fatal("archived order remains in list", code, out)
	}
	code, out = invoke("GET", "/orders?include_archived=true", "", csrf)
	if code != 200 || out["data"].(map[string]any)["total"].(float64) != 1 {
		t.Fatal("archive cannot be reviewed", code, out)
	}
	code, out = invoke("POST", path+"/restore", `{"external_no":"restore-one","reason":"恢复后台记录","expected_revision":"`+rev+`"}`, csrf)
	if code != 200 {
		t.Fatal(code, out)
	}
	var count int
	pool.QueryRow(ctx, "SELECT count(*) FROM orders WHERE id=$1", o.ID).Scan(&count)
	if count != 1 {
		t.Fatal("original financial order physically deleted")
	}
}

func TestOrderAdminUXManualRefundRequiresOwnBoundFundsGrantEvenWhenGlobalOff(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(pool, false)
	s.FundsActionResolver = s.ResolveFundsAction
	user := identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	funds := identity.NewFundsAuthorization(pool, nil)
	actor := identity.FundsActor{User: user, SessionToken: token, ClientIP: "192.0.2.1"}
	if e = funds.SetPassword(ctx, actor, "contract-password-123", "", "manual-funds-password-456", ""); e != nil {
		t.Fatal(e)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "manual-http", Name: "item", Type: "alipay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "manual-http-paid"); e != nil {
		t.Fatal(e)
	}
	request := func(path, body, grant string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		if grant != "" {
			r.Header.Set("X-Payment-Authorization", grant)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	target := "/console/v1/admin/orders/" + o.TradeNo + "/manual-refund"
	body := `{"amount":"0.40","external_no":"manual-http-one","reason":"原线下退款凭证","offline_confirmed":true}`
	if w := request(target, body, ""); w.Code != 428 {
		t.Fatal("manual refund escaped mandatory grant", w.Code, w.Body.String())
	}
	preview, _ := json.Marshal(map[string]any{"target": target, "body": json.RawMessage(body)})
	w := request("/console/v1/admin/security/funds-authorizations/challenges", string(preview), "")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var challenge struct{ Data identity.FundsChallenge }
	json.Unmarshal(w.Body.Bytes(), &challenge)
	w = request("/console/v1/admin/security/funds-authorizations/confirm", fmt.Sprintf(`{"challenge_id":%q,"payment_password":"manual-funds-password-456"}`, challenge.Data.ChallengeID), "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var grant struct{ Data identity.FundsGrant }
	json.Unmarshal(w.Body.Bytes(), &grant)
	if w = request(target, strings.Replace(body, "0.40", "0.50", 1), grant.Data.Authorization); w.Code != 428 {
		t.Fatal("grant did not bind original amount", w.Code, w.Body.String())
	}
	if w = request(target, body, grant.Data.Authorization); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request(target, body, ""); w.Code != 200 {
		t.Fatal("idempotent replay requires new grant", w.Code, w.Body.String())
	}
	b, e := s.Store.Balance(ctx, 1)
	if e != nil || b.Available != 60 || b.Reserved != 0 {
		t.Fatal(b, e)
	}
	var operations, audits int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_operations WHERE order_id=$1),(SELECT count(*) FROM backoffice_audit WHERE resource='orders' AND resource_id=$1 AND operation='manual-refund')`, o.ID).Scan(&operations, &audits); e != nil || operations != 0 || audits != 1 {
		t.Fatal(operations, audits, e)
	}
}

func TestOrderAdminUXMerchantCannotUseAdministrativeOrManualRefundActions(t *testing.T) {
	s, _, _ := fixture(t)
	token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	for _, action := range []string{"admin-status", "archive", "restore", "manual-refund", "manual-complete"} {
		r := httptest.NewRequest("POST", "/console/v1/merchant/orders/not-an-order/"+action, strings.NewReader(`{}`))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("%s accepted or leaked lookup: %d", action, w.Code)
		}
	}
}
