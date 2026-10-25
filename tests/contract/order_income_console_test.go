package contract_test

import (
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestOrderIncomeConsoleOperatorPermittedMerchantDeniedCSRFAndAtomicAudit(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "income-console", Name: "item", Amount: 125, Type: "alipay"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "income-paid"); e != nil {
		t.Fatal(e)
	}
	// Explicit non-super administrator as in original R_ADMIN order management.
	if _, e = pool.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('income-operator','admin',82,'income operator','unused','operator')"); e != nil {
		t.Fatal(e)
	}
	admin, csrf, e := s.Sessions.Create(identity.User{Username: "income-operator", Role: "admin", ID: 82, AdminClass: "operator"})
	if e != nil {
		t.Fatal(e)
	}
	merchant, mcsrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	invoke := func(role, token, csrfValue, action, body string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest("POST", "/console/v1/"+role+"/orders/"+o.TradeNo+"/"+action, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		r.Header.Set("X-CSRF-Token", csrfValue)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(w.Body.String(), e)
		}
		return w.Code, out
	}
	if status, _ := invoke("merchant", merchant, mcsrf, "freeze-income", `{"external_no":"not-admin"}`); status != 403 {
		t.Fatal("merchant income freeze allowed", status)
	}
	if status, _ := invoke("admin", admin, "", "freeze-income", `{"external_no":"no-csrf"}`); status != 403 {
		t.Fatal("income CSRF allowed", status)
	}
	if _, e = pool.Exec(ctx, `CREATE FUNCTION reject_income_console_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'local income audit failure'; END $$; CREATE TRIGGER reject_income_console_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_income_console_audit()`); e != nil {
		t.Fatal(e)
	}
	if status, _ := invoke("admin", admin, csrf, "freeze-income", `{"external_no":"audit-blocked"}`); status < 400 {
		t.Fatal("audit failed freeze committed", status)
	}
	b, _ := s.Store.Balance(ctx, 1)
	if b.Available != 125 {
		t.Fatal("audit failure moved money", b)
	}
	if _, e = pool.Exec(ctx, "DROP TRIGGER reject_income_console_audit ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	status, out := invoke("admin", admin, csrf, "freeze-income", `{"external_no":"stable-freeze"}`)
	if status != 200 || out["code"] != float64(0) {
		t.Fatal("operator freeze denied", status, out)
	}
	h := out["data"].(map[string]any)
	id := h["id"].(string)
	getDetail := func(role, token string) map[string]any {
		t.Helper()
		r := httptest.NewRequest("GET", "/console/v1/"+role+"/orders/"+o.TradeNo, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil || w.Code != 200 {
			t.Fatal(w.Code, w.Body.String(), e)
		}
		return out["data"].(map[string]any)
	}
	adminView := getDetail("admin", admin)
	if adminView["can_freeze_income"] != false || adminView["can_unfreeze_income"] != true || adminView["income_hold"].(map[string]any)["state"] != "held" {
		t.Fatal(adminView)
	}
	merchantView := getDetail("merchant", merchant)
	if merchantView["can_freeze_income"] != false || merchantView["can_unfreeze_income"] != false {
		t.Fatal("merchant saw admin income actions", merchantView)
	}
	if status, out = invoke("admin", admin, csrf, "freeze-income", `{"external_no":"stable-freeze"}`); status != 200 || out["data"].(map[string]any)["id"] != id {
		t.Fatal("freeze replay failed", status, out)
	}
	if status, out = invoke("admin", admin, csrf, "unfreeze-income", `{"hold_id":"`+id+`"}`); status != 200 || out["data"].(map[string]any)["state"] != "released" {
		t.Fatal(status, out)
	}
	if status, out = invoke("admin", admin, csrf, "unfreeze-income", `{"hold_id":"`+id+`"}`); status != 200 {
		t.Fatal("unfreeze replay", status, out)
	}
	b, _ = s.Store.Balance(ctx, 1)
	current, _ := s.Store.OrderByTradeNo(ctx, o.TradeNo)
	if b.Available != 125 || current.Status != "paid" {
		t.Fatal(b, current.Status)
	}
	var journals, audits int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM journals WHERE event_key LIKE 'order-income:%'").Scan(&journals); e != nil || journals != 2 {
		t.Fatal(journals, e)
	}
	holdID, _ := strconv.ParseInt(id, 10, 64)
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM order_income_hold_events WHERE hold_id=$1 AND actor_class='operator' AND actor_id=82", holdID).Scan(&audits); e != nil || audits != 2 {
		t.Fatal("missing financial actor audit", audits, e)
	}
}
