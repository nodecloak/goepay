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

func child168BMerchantSession(t *testing.T, f channel168Fixture, id int64) (string, string) {
	t.Helper()
	name := fmt.Sprintf("child168bmerchant%d", id)
	if _, e := f.p.Exec(context.Background(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES($1,'merchant',$2,'Synthetic','unused-synthetic-only')`, name, id); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := f.s.Sessions.Create(identity.User{ID: id, Role: "merchant", Username: name, AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	return token, csrf
}
func child168BMerchantRequest(f channel168Fixture, token, csrf, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/merchant/"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func TestSubchannel168BActualHTTPMerchantOwnerAndReadWriteBoundaries(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	row := child168BCreate(t, f, "owner-child", true)
	if e := f.s.Store.CreateMerchant(context.Background(), payment.Merchant{ID: 2, Name: "Synthetic Other", APIKey: "synthetic-other"}); e != nil {
		t.Fatal(e)
	}
	token, csrf := child168BMerchantSession(t, f, 1)
	other, otherCSRF := child168BMerchantSession(t, f, 2)
	id := row["id"].(string)
	for _, path := range []string{"payment-subchannels", "payment-subchannels/" + id, "payment-subchannels/policy"} {
		w := child168BMerchantRequest(f, token, csrf, "GET", path, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "owner-child") || strings.Contains(w.Body.String(), "synthetic-only-secret") {
			t.Fatal("owned safe read", w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"payment-subchannels/" + id, "payment-subchannels/" + id + "/test?intent=" + strings.Repeat("b", 32), "payment-subchannels/" + id + "/collection-link"} {
		w := child168BMerchantRequest(f, other, otherCSRF, "GET", path, "")
		if w.Code != 404 {
			t.Fatal("foreign child endpoint exposed existence", path, w.Code, w.Body.String())
		}
	}
	b, _ := json.Marshal(map[string]any{"expected_fingerprint": row["fingerprint"], "weight": 5})
	w := child168BMerchantRequest(f, token, csrf, "PATCH", "payment-subchannels/"+id, string(b))
	if w.Code != 200 {
		t.Fatal("own weight write", w.Code, w.Body.String())
	}
	for _, operation := range []struct{ method, path, body string }{{"DELETE", "payment-subchannels/" + id, fmt.Sprintf(`{"expected_fingerprint":%q}`, row["fingerprint"])}, {"POST", "payment-subchannels", `{}`}, {"GET", "payment-subchannels/schemas", ``}, {"PATCH", "payment-subchannels/" + id, fmt.Sprintf(`{"expected_fingerprint":%q,"values":{"app":{"operation":"replace","value":"foreign"}}}`, row["fingerprint"])}, {"PATCH", "payment-subchannels/" + id, fmt.Sprintf(`{"expected_fingerprint":%q,"parent_id":"8"}`, row["fingerprint"])}} {
		w = child168BMerchantRequest(f, token, csrf, operation.method, operation.path, operation.body)
		if w.Code == 200 {
			t.Fatal("merchant gained administrator action", operation.path, operation.method)
		}
	}
	w = child168BMerchantRequest(f, other, otherCSRF, "PATCH", "payment-subchannels/"+id, string(b))
	if w.Code != 404 {
		t.Fatal("foreign merchant write", w.Code)
	}
}
func TestSubchannel168BActualHTTPOrderListAndCSVUseSameOwnedChildFK(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	a := child168BCreate(t, f, "filter-one", true)
	b := child168BCreate(t, f, "filter-two", true)
	for i, row := range []map[string]any{a, b} {
		out := subchannel168BProductPay(t, f, fmt.Sprintf("child-filter-own-%d", i), "native", map[string]string{"subchannel_id": row["id"].(string)})
		if out["code"] != float64(1) {
			t.Fatal(out)
		}
	}
	token, csrf := child168BMerchantSession(t, f, 1)
	id := a["id"].(string)
	for _, suffix := range []string{"", "&format=csv"} {
		w := child168BMerchantRequest(f, token, csrf, "GET", "orders?subchannel="+id+suffix, "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), "child-filter-own-0") || strings.Contains(w.Body.String(), "child-filter-own-1") {
			t.Fatal("owned FK list/CSV mismatch", w.Code, w.Body.String())
		}
	}
	if e := f.s.Store.CreateMerchant(context.Background(), payment.Merchant{ID: 2, Name: "Synthetic Other", APIKey: "synthetic-other"}); e != nil {
		t.Fatal(e)
	}
	other, otherCSRF := child168BMerchantSession(t, f, 2)
	for _, suffix := range []string{"", "&format=csv"} {
		w := child168BMerchantRequest(f, other, otherCSRF, "GET", "orders?subchannel="+id+suffix, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "child-filter-own-") {
			t.Fatal("child filter leaked other owner order", w.Code, w.Body.String())
		}
	}
}
func TestSubchannel168BActualHTTPMerchantCanStopAnUnusableAccountWithoutBorrowingParent(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	row := child168BCreate(t, f, "stop-child", true)
	token, csrf := child168BMerchantSession(t, f, 1)
	delete(f.s.ProviderFlow.SubchannelFactories, 7)
	b, _ := json.Marshal(map[string]any{"expected_fingerprint": row["fingerprint"], "enabled": false})
	w := child168BMerchantRequest(f, token, csrf, "PATCH", "payment-subchannels/"+row["id"].(string), string(b))
	if w.Code != 200 {
		t.Fatal("safe stop required executable parent material", w.Code, w.Body.String())
	}
	var enabled bool
	if e := f.p.QueryRow(context.Background(), `SELECT enabled FROM payment_subchannels`).Scan(&enabled); e != nil || enabled {
		t.Fatal("stop not consumed", enabled, e)
	}
}
func TestSubchannel168BActualHTTPOrderDetailKeepsFullChildAndParentIDs(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	if _, e := f.p.Exec(context.Background(), `SELECT setval(pg_get_serial_sequence('payment_subchannels','id'),9007199254740993,false)`); e != nil {
		t.Fatal(e)
	}
	child168BCreate(t, f, "detail-child", true)
	if out := f.pay(t, "child-detail-full-id", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(context.Background(), 1, "child-detail-full-id")
	if e != nil {
		t.Fatal(e)
	}
	token, csrf := child168BMerchantSession(t, f, 1)
	w := child168BMerchantRequest(f, token, csrf, "GET", "orders/"+o.TradeNo, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var row struct{ Data map[string]any }
	if json.Unmarshal(w.Body.Bytes(), &row) != nil {
		t.Fatal("bad details")
	}
	if row.Data["provider"].(map[string]any)["channel_id"] != "7" {
		t.Fatal("parent ID is not an exact JSON string")
	}
	child, ok := row.Data["payment_subchannel"].(map[string]any)
	if !ok || child["id"] != "9007199254740993" || child["parent_id"] != "7" || child["account_version"] != "1" {
		t.Fatal("original exact child context absent", child)
	}
}
func TestSubchannel168BActualHTTPRechargeKeepsChildAccountAndSingleOriginalIntent(t *testing.T) {
	f, factory, _ := subchannel168BProductLocal(t)
	token, csrf := child168BMerchantSession(t, f, 1)
	for i := 0; i < 2; i++ {
		w := child168BMerchantRequest(f, token, csrf, "POST", "workflows/recharges", `{"amount":"1.00","type":"alipay","method":"page","external_no":"child168b-recharge"}`)
		if w.Code != 201 {
			t.Fatal("child recharge entrypoint", w.Code, w.Body.String())
		}
	}
	if factory.providers["products-v1"].creates.Load() != 1 || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 0 {
		t.Fatal("recharge used parent or resent")
	}
	var routes, attempts, intents int
	if e := f.p.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM payment_subchannel_routes),(SELECT count(*) FROM provider_attempts),(SELECT count(*) FROM recharge_intents)`).Scan(&routes, &attempts, &intents); e != nil || routes != 1 || attempts != 1 || intents != 1 {
		t.Fatal("recharge original source incomplete", routes, attempts, intents, e)
	}
}
