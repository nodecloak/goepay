package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrderBatch185RefundBatchAlwaysRequiresOriginalFinancialAuthorization(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	if out := f.pay(t, "refund-batch185", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(context.Background(), 1, "refund-batch185")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(context.Background(), 1, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	body := strings.Replace(batch185Input(o.TradeNo, "refund-local185"), `"action":"query"`, `"action":"refund"`, 1)
	w := batch185Request(f, "POST", "", body)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var v struct{ Data map[string]any }
	if json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatal(w.Body.String())
	}
	w = batch185Request(f, "POST", "/"+v.Data["id"].(string)+"/execute", `{"limit":1}`)
	if w.Code != 428 {
		t.Fatal("batch refund bypassed explicit original funds grant", w.Code, w.Body.String())
	}
	var refs, ops int
	if e = f.p.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM refunds),(SELECT count(*) FROM provider_operations WHERE kind='refund')`).Scan(&refs, &ops); e != nil || refs+ops != 0 || factory.providers["products-v1"].refunds.Load() != 0 {
		t.Fatal(refs, ops, e)
	}
}

func TestOrderBatch185OneOriginalFundsGrantExecutesFixedRefundAndReplaysWithoutSendingAgain(t *testing.T) {
	runOriginalFundsBatch185(t, false, false, "refund", false)
}
func TestOrderBatch185PaymentCredentialRotationStopsRemainingAuthorizedItems(t *testing.T) {
	runOriginalFundsBatch185(t, true, false, "refund", false)
}
func TestOrderBatch185UnknownOriginalRefundRecoversAfterCredentialChangeWithoutResend(t *testing.T) {
	runOriginalFundsBatch185(t, false, true, "refund", false)
}
func TestOrderBatch185FreezeUsesOriginalIncomeAndOneBatchGrant(t *testing.T) {
	runOriginalFundsBatch185(t, false, false, "freeze-income", false)
}
func TestOrderBatch185UnfreezeUsesExactPreparedHoldAndOneBatchGrant(t *testing.T) {
	runOriginalFundsBatch185(t, false, false, "unfreeze-income", false)
}
func TestOrderBatch185CommittedUnfreezeResultLossRecoversAfterCredentialRotation(t *testing.T) {
	runOriginalFundsBatch185(t, false, false, "unfreeze-income", true)
}
func runOriginalFundsBatch185(t *testing.T, rotate, unknown bool, business string, lostLocalResult bool) {
	t.Helper()
	f, factory, row := subchannel168BProductLocal(t)
	ctx := context.Background()
	if out := f.pay(t, "authorized-refund185", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(ctx, 1, "authorized-refund185")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); e != nil {
		t.Fatal(e)
	}
	if business == "unfreeze-income" {
		if _, e = f.s.Store.FreezeOrderIncome(ctx, 1, o.TradeNo, "prior-hold185", payment.IncomeHoldActor{ID: 168, Name: "admin168", Class: "super"}); e != nil {
			t.Fatal(e)
		}
	}
	password, e := identity.HashPassword("local-login185-password")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.p.Exec(ctx, `UPDATE console_users SET password_hash=$1 WHERE username='admin168'`, password); e != nil {
		t.Fatal(e)
	}
	user := identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1}
	if e = f.p.QueryRow(ctx, `SELECT auth_version FROM console_users WHERE username='admin168' AND role='admin'`).Scan(&user.AuthVersion); e != nil {
		t.Fatal(e)
	}
	f.token, f.csrf, e = f.s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	a := identity.FundsActor{User: user, SessionToken: f.token, ClientIP: "192.0.2.1"}
	if e = identity.NewFundsAuthorization(f.p, nil).SetPassword(ctx, a, "local-login185-password", "", "local-payment185-password", ""); e != nil {
		t.Fatal(e)
	}
	f.s.FundsActionResolver = f.s.ResolveFundsAction
	body := strings.Replace(batch185Input(o.TradeNo, "authorized-batch185"), `"action":"query"`, `"action":"refund"`, 1)
	if business != "refund" {
		body = strings.Replace(body, `"action":"refund"`, `"action":"`+business+`"`, 1)
	}
	if rotate {
		subchannel168BRotate(t, f, row)
		if out := f.pay(t, "credential-second185", "1.00", 7); out["code"] != float64(1) {
			t.Fatal(out)
		}
		second, e := f.s.Store.Order(ctx, 1, "credential-second185")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.s.ProviderFlow.Query(ctx, 1, second.TradeNo); e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(map[string]any{"external_no": "authorized-batch185", "action": "refund", "reason": "local credential rotation", "items": []map[string]string{{"trade_no": o.TradeNo}, {"trade_no": second.TradeNo}}})
		body = string(raw)
	}
	w := batch185Request(f, "POST", "", body)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var v struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &v)
	id := v.Data["id"].(string)
	req := func(path, body, grant string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.Header.Set("X-Payment-Authorization", grant)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	target := "/console/v1/admin/orders/business-batches/" + id + "/execute"
	preview, _ := json.Marshal(map[string]any{"target": target, "body": map[string]int{"limit": 1}})
	w = req("/console/v1/admin/security/funds-authorizations/challenges", string(preview), "")
	if w.Code != 201 {
		t.Fatal("batch challenge", w.Code, w.Body.String())
	}
	var challenge struct{ Data identity.FundsChallenge }
	json.Unmarshal(w.Body.Bytes(), &challenge)
	w = req("/console/v1/admin/security/funds-authorizations/confirm", fmt.Sprintf(`{"challenge_id":%q,"payment_password":"local-payment185-password"}`, challenge.Data.ChallengeID), "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var grant struct{ Data identity.FundsGrant }
	json.Unmarshal(w.Body.Bytes(), &grant)
	if unknown {
		f.s.ProviderFlow.SubchannelFactories[7] = &batch185UnknownRefundFactory{subchannel168BProductFactory: factory}
	}
	if lostLocalResult {
		if _, e = f.p.Exec(ctx, `CREATE FUNCTION batch185_fail_result() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.event_type='result' THEN RAISE EXCEPTION 'synthetic batch result failure';END IF;RETURN NEW;END$$;CREATE TRIGGER batch185_fail_result BEFORE INSERT ON order_business_batch_events FOR EACH ROW EXECUTE FUNCTION batch185_fail_result()`); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 2; i++ {
		if (rotate || unknown || lostLocalResult) && i == 1 {
			if e = identity.NewFundsAuthorization(f.p, nil).SetPassword(ctx, a, "local-login185-password", "local-payment185-password", "rotated-payment185-password", ""); e != nil {
				t.Fatal(e)
			}
		}
		w = req(target, `{"limit":1}`, grant.Data.Authorization)
		if lostLocalResult && i == 0 {
			if w.Code != 503 {
				t.Fatal("synthetic persisted result fault was not surfaced", w.Code, w.Body.String())
			}
			if _, e = f.p.Exec(ctx, `DROP TRIGGER batch185_fail_result ON order_business_batch_events;DROP FUNCTION batch185_fail_result()`); e != nil {
				t.Fatal(e)
			}
			continue
		}
		if rotate && i == 1 {
			if w.Code != 428 {
				t.Fatal("credential rotation retained old batch financial authority", w.Code, w.Body.String())
			}
			if factory.providers["products-v1"].refunds.Load() != 1 {
				t.Fatal("credential rotation sent another refund")
			}
			if factory.providers["products-v2"].refunds.Load() != 0 {
				t.Fatal("credential rotation sent remaining account refund")
			}
			return
		}
		if w.Code != 200 {
			t.Fatal("authorized batch execution", i, w.Code, w.Body.String())
		}
		var out struct {
			Data struct {
				State string `json:"state"`
			} `json:"data"`
		}
		json.Unmarshal(w.Body.Bytes(), &out)
		if out.Data.State != "completed" && !(rotate && i == 0 && out.Data.State == "running") && !(unknown && i == 0 && out.Data.State == "attention") {
			t.Fatal(w.Body.String())
		}
	}
	if business != "refund" {
		h, e := f.s.Store.OrderIncomeHold(ctx, 1, o.TradeNo)
		if e != nil {
			t.Fatal(e)
		}
		expected := "held"
		if business == "unfreeze-income" {
			expected = "released"
		}
		if h.State != expected || factory.providers["products-v1"].refunds.Load() != 0 {
			t.Fatal("batch changed unrelated funds or upstream business", h.State, business)
		}
	} else if factory.providers["products-v1"].refunds.Load() != 1 {
		t.Fatal("batch grant or replay resent refund", factory.providers["products-v1"].refunds.Load())
	}
	if _, e = f.s.Store.CheckLedger(ctx); e != nil {
		t.Fatal(e)
	}
}

type batch185UnknownRefund struct{ *subchannel168BProducts }

func (p *batch185UnknownRefund) PaymentProductIdentity() string {
	v, _ := adapter.PaymentProductScope(p.subchannel168BProducts)
	return v
}
func (p *batch185UnknownRefund) Refund(context.Context, adapter.RefundRequest) (adapter.RefundResult, error) {
	p.refunds.Add(1)
	return adapter.RefundResult{}, &adapter.UnknownOutcome{Operation: "local refund", Cause: errors.New("synthetic lost reply")}
}

type batch185UnknownRefundFactory struct{ *subchannel168BProductFactory }

func (f *batch185UnknownRefundFactory) Restore(raw []byte) (adapter.SubchannelExecution, error) {
	v, e := f.subchannel168BProductFactory.Restore(raw)
	if e != nil {
		return v, e
	}
	p, ok := v.Payment.(*subchannel168BProducts)
	if !ok {
		return v, adapter.ErrInvalid
	}
	v.Payment = &batch185UnknownRefund{p}
	return v, nil
}
