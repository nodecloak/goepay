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

func independentStale185(t *testing.T, rotate, unknown bool, business string, lostLocalResult bool) {
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

	w = req(target, "{\"limit\":1}", grant.Data.Authorization)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	second, e := f.s.Store.Order(ctx, 1, "credential-second185")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Refund(ctx, 1, second.TradeNo, "independent-concurrent-single185", 25); e != nil {
		t.Fatal(e)
	}
	w = req(target, "{\"limit\":1}", grant.Data.Authorization)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var got struct{ Data payment.OrderBusinessBatch }
	if e = json.Unmarshal(w.Body.Bytes(), &got); e != nil {
		t.Fatal(e)
	}
	t.Logf("after actual concurrent original single refund: state=%s items=%+v", got.Data.State, got.Data.Items)
	var batchRefunds, batchOps int
	if e = f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM refunds WHERE refund_no=$1),(SELECT count(*) FROM provider_operations op JOIN refunds r ON r.id=op.refund_id WHERE r.refund_no=$1)`, fmt.Sprintf("batch185-refund:%s:2", id)).Scan(&batchRefunds, &batchOps); e != nil {
		t.Fatal(e)
	}
	t.Logf("second batch item rows=%d claimed_operations=%d", batchRefunds, batchOps)
	cancel := batch185Request(f, "POST", "/"+id+"/cancel", fmt.Sprintf(`{"expected_fingerprint":%q}`, got.Data.Fingerprint))
	t.Logf("cancel HTTP=%d %s", cancel.Code, cancel.Body.String())
	if batchRefunds == 0 && batchOps == 0 && got.Data.State == "attention" {
		t.Fatal("definitive stale full-refund budget became unrecoverable unknown with no original refund or claimed operation; cancel leaves it unchanged")
	}
}
func TestOrderBatch185IndependentStaleRefundNoSideEffectTerminates(t *testing.T) {
	independentStale185(t, true, false, "refund", false)
}
