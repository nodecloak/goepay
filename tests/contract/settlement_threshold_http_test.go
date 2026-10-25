package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestSettlementThreshold74HTTPOriginalModeMandatoryGrantAndMaskedWholeBalanceSummary(t *testing.T) {
	s, p, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(p, false)
	s.FundsActionResolver = s.ResolveFundsAction
	user := identity.User{Role: "admin", Username: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"}
	token, csrf, e := s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, path, body, grant string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(method, "/console/v1/admin"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("X-Payment-Authorization", grant)
		r.RemoteAddr = "192.0.2.74:1234"
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		if json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		return w.Code, out
	}
	zero := int64(0)
	recipient, e := s.Store.SetSettlementRecipient(ctx, 1, payment.SettlementRecipientInput{Type: "alipay", Account: "threshold74-private@example.invalid", Name: "Threshold Private"}, &zero, payment.SettlementRecipientActor{Role: "merchant", ID: 1, Name: "merchant"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Store.SetSettlementSchedulePreference(ctx, 1, 0, true, recipient.ConfigID, payment.SettlementPlanActor{Role: "merchant", ID: 1, Name: "merchant"}); e != nil {
		t.Fatal(e)
	}
	for key, value := range map[string]string{"auto_settle_money": "1.00", "transfer_alipay": "7", "settlement_fee_rate_bps": "300", "settlement_fee_maximum_cents": "25"} {
		raw, _ := json.Marshal(map[string]string{"key": key, "value": value})
		if _, e = p.Exec(ctx, "INSERT INTO backoffice_records(resource,body)VALUES('settings',$1)", raw); e != nil {
			t.Fatal(e)
		}
	}
	var now time.Time
	if e = p.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		t.Fatal(e)
	}
	definition := map[string]any{"name": "余额自动转账", "mode": "balance_threshold", "merchant_ids": []string{"1"}, "schedule": map[string]any{"frequency": "minute", "interval": "1", "anchor_at": now.UTC().Truncate(time.Minute).Format(time.RFC3339), "execute_hour": 0, "execute_minute": 0, "execute_second": 0}}
	raw, _ := json.Marshal(definition)
	status, created := call("POST", "/settlement-plans", string(raw), "")
	if status != 201 {
		t.Fatal(status, created)
	}
	id := created["data"].(map[string]any)["id"].(string)
	if created["data"].(map[string]any)["enabled"] != false {
		t.Fatal("definition opened funds")
	}
	body, _ := json.Marshal(map[string]string{"expected_revision": "1", "expected_authorization_id": "0", "valid_from": now.Add(-time.Minute).UTC().Truncate(time.Second).Format(time.RFC3339), "expires_at": now.Add(time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339), "max_item_gross": "1000", "max_run_gross": "1000", "max_day_gross": "1000", "max_day_items": "1"})
	target := "/settlement-plans/" + id + "/authorizations"
	if status, _ = call("POST", target, string(body), ""); status != 428 {
		t.Fatal("threshold bypassed mandatory independent generation grant", status)
	}
	funds := identity.NewFundsAuthorization(p, nil)
	if e = funds.SetPassword(ctx, identity.FundsActor{User: user, SessionToken: token, ClientIP: "192.0.2.74"}, "contract-password-123", "", "local-threshold-password-456", ""); e != nil {
		t.Fatal(e)
	}
	challengeBody, _ := json.Marshal(map[string]any{"target": "/console/v1/admin" + target, "body": json.RawMessage(body)})
	status, challenge := call("POST", "/security/funds-authorizations/challenges", string(challengeBody), "")
	if status != 201 {
		t.Fatal(status, challenge)
	}
	summary := challenge["data"].(map[string]any)["summary"].(map[string]any)
	if summary["business_mode"] != "balance_threshold" || summary["balance_threshold_money"] != "1.00" || summary["transfer_channel_id"] != "7" || !strings.Contains(fmt.Sprint(summary["selection_rule"]), "5") || !strings.Contains(fmt.Sprint(summary["payout_amount"]), "整余额") || strings.Contains(fmt.Sprint(summary), "threshold74-private") {
		t.Fatal("threshold funds preview lost original business/source or leaked private payee", summary)
	}
	confirm, _ := json.Marshal(map[string]any{"challenge_id": challenge["data"].(map[string]any)["challenge_id"], "payment_password": "local-threshold-password-456"})
	status, grant := call("POST", "/security/funds-authorizations/confirm", string(confirm), "")
	if status != 200 {
		t.Fatal(status, grant)
	}
	proof := grant["data"].(map[string]any)["authorization"].(string)
	status, authorized := call("POST", target, string(body), proof)
	if status != 201 {
		t.Fatal(status, authorized)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "threshold-http-income", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "threshold-http-local"); e != nil {
		t.Fatal(e)
	}
	status, scanned := call("POST", "/settlement-plans/"+id+"/runs", `{"expected_revision":"1"}`, "")
	if status != 202 {
		t.Fatal(status, scanned)
	}
	runID := scanned["data"].(map[string]any)["id"].(string)
	var rid int64
	if _, e = fmt.Sscan(runID, &rid); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.GenerateSettlementPlanRun(ctx, rid); e != nil {
		t.Fatal(e)
	}
	status, read := call("GET", "/settlement-plan-runs/"+runID, "", "")
	if status != 200 {
		t.Fatal(status, read)
	}
	v := read["data"].(map[string]any)
	items := v["items"].([]any)
	if v["created_count"] != "1" || len(items) != 1 || items[0].(map[string]any)["gross"] != "1000" || items[0].(map[string]any)["fee"] != "25" {
		t.Fatal(v)
	}
}
