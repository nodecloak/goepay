package server

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

func independentHolds185(t *testing.T, business string, dbFault bool) {
	t.Helper()
	rotate, unknown, lostLocalResult := business == "unfreeze-income" && !dbFault, false, false
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
		if business == "unfreeze-income" {
			if _, e = f.s.Store.FreezeOrderIncome(ctx, 1, second.TradeNo, "second-prior-hold185", payment.IncomeHoldActor{ID: 168, Name: "admin168", Class: "super"}); e != nil {
				t.Fatal(e)
			}
		}
		raw, _ := json.Marshal(map[string]any{"external_no": "authorized-batch185", "action": business, "reason": "local credential rotation", "items": []map[string]string{{"trade_no": o.TradeNo}, {"trade_no": second.TradeNo}}})
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

	if rotate {
		w = req(target, "{\"limit\":1}", grant.Data.Authorization)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		o, e = f.s.Store.Order(ctx, 1, "credential-second185")
		if e != nil {
			t.Fatal(e)
		}
	}
	if dbFault {
		if _, e = f.p.Exec(ctx, `CREATE FUNCTION independent185_fail_hold() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'synthetic hold audit fault';END$$;CREATE TRIGGER independent185_fail_hold BEFORE INSERT ON order_income_hold_events FOR EACH ROW EXECUTE FUNCTION independent185_fail_hold()`); e != nil {
			t.Fatal(e)
		}
	} else if business == "freeze-income" {
		if _, e = f.s.Store.FreezeOrderIncome(ctx, 1, o.TradeNo, "competing-original-hold185", payment.IncomeHoldActor{ID: 168, Name: "admin168", Class: "super"}); e != nil {
			t.Fatal(e)
		}
	} else {
		if _, e = f.s.Store.ReserveRefund(ctx, 1, o.OutTradeNo, "concurrent-held-refund185", 25); e != nil {
			t.Fatal(e)
		}
	}
	w = req(target, "{\"limit\":1}", grant.Data.Authorization)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var got struct{ Data payment.OrderBusinessBatch }
	if e = json.Unmarshal(w.Body.Bytes(), &got); e != nil {
		t.Fatal(e)
	}
	t.Logf("business=%s fault=%v first=%+v", business, dbFault, got.Data.Items)
	if dbFault {
		if got.Data.State != "attention" || got.Data.Items[0].State != "unknown" {
			t.Fatal("DB audit error incorrectly became definite failure", w.Body.String())
		}
		if _, e = f.p.Exec(ctx, `DROP TRIGGER independent185_fail_hold ON order_income_hold_events;DROP FUNCTION independent185_fail_hold()`); e != nil {
			t.Fatal(e)
		}
		w = req(target, "{\"limit\":1}", grant.Data.Authorization)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if e = json.Unmarshal(w.Body.Bytes(), &got); e != nil {
			t.Fatal(e)
		}
		if got.Data.State != "completed" || got.Data.Items[0].State != "succeeded" {
			t.Fatal("original same request recovery failed", w.Body.String())
		}
	} else {
		if got.Data.State != "completed" || got.Data.Items[len(got.Data.Items)-1].State != "failed" {
			t.Fatal("definitively unstarted stale hold action did not terminate", w.Body.String())
		}
		if business == "freeze-income" {
			var n int
			if e = f.p.QueryRow(ctx, `SELECT count(*) FROM order_income_holds WHERE request_no=$1`, "batch185-freeze-income:"+id+":1").Scan(&n); e != nil || n != 0 {
				t.Fatal("new batch hold exists", n, e)
			}
		} else {
			h, err := f.s.Store.OrderIncomeHold(ctx, 1, o.TradeNo)
			if err != nil || h.State != "held" || h.RefundReservedAmount != 25 {
				t.Fatal("failed batch released original pending funding", h, err)
			}
		}
	}
	if _, e = f.s.Store.CheckLedger(ctx); e != nil {
		t.Fatal(e)
	}
}
func TestOrderBatch185IndependentFreezeRejectedBeforeIntent(t *testing.T) {
	independentHolds185(t, "freeze-income", false)
}
func TestOrderBatch185IndependentUnfreezeRejectedBeforeIntentPreservesOtherReserve(t *testing.T) {
	independentHolds185(t, "unfreeze-income", false)
}
func TestOrderBatch185IndependentFreezeDBFaultKeepsUnknownAndRecoversSameRequest(t *testing.T) {
	independentHolds185(t, "freeze-income", true)
}
func TestOrderBatch185IndependentUnfreezeDBFaultKeepsUnknownAndRecoversSameRequest(t *testing.T) {
	independentHolds185(t, "unfreeze-income", true)
}
