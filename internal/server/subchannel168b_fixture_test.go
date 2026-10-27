package server

import (
	"context"
	"encoding/json"
	"goepay/internal/identity"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSubchannel168BExportSyntheticHTTPUIFixture(t *testing.T) {
	target := os.Getenv("GOEPAY_SUBCHANNEL168B_UI_FIXTURE")
	if target == "" {
		t.Skip("dedicated synthetic UI fixture target required")
	}
	f, _ := subchannel168BLocal(t)
	f.configure(t, 7, "99.50")
	if _, e := f.p.Exec(context.Background(), `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}');SELECT setval(pg_get_serial_sequence('payment_subchannels','id'),9007199254740993,false)`); e != nil {
		t.Fatal(e)
	}
	for _, item := range []struct {
		name, app string
		enabled   bool
	}{{"本地合成商户子通道名称用于完整显示与换行校验😀", "fixture-app-one", true}, {"本地合成未启用账户", "fixture-app-two", false}} {
		body, _ := json.Marshal(map[string]any{"merchant_id": "1", "parent_id": "7", "name": item.name, "enabled": item.enabled, "weight": 99, "min_amount": "1", "max_amount": "100000", "values": map[string]string{"app": item.app, "secret": "synthetic-only-secret"}})
		w := f.childHTTP("POST", "", string(body))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if out := f.pay(t, "subchannel168b-ui-pending", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	policyW := f.childHTTP("GET", "/policy?merchant_id=1", "")
	var policy struct{ Data map[string]any }
	json.Unmarshal(policyW.Body.Bytes(), &policy)
	patch, _ := json.Marshal(map[string]any{"roll_type": 2, "expected_fingerprint": policy.Data["fingerprint"]})
	if w := f.childHTTP("PATCH", "/policy?merchant_id=1", string(patch)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, e := f.p.Exec(context.Background(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('fixture168bmerchant','merchant',1,'Synthetic Merchant','unused-synthetic-only','operator')`); e != nil {
		t.Fatal(e)
	}
	token, _, e := f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "fixture168bmerchant", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	fetch := func(role, path string) json.RawMessage {
		t.Helper()
		var w *httptest.ResponseRecorder
		if role == "admin" {
			w = f.childHTTP("GET", path, "")
		} else {
			r := httptest.NewRequest("GET", "/console/v1/merchant/payment-subchannels"+path, nil)
			r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
			w = httptest.NewRecorder()
			f.s.Handler().ServeHTTP(w, r)
		}
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var envelope struct{ Data json.RawMessage }
		if json.Unmarshal(w.Body.Bytes(), &envelope) != nil {
			t.Fatal("fixture JSON")
		}
		return envelope.Data
	}
	out := map[string]any{"synthetic_only": true, "admin": fetch("admin", ""), "merchant": fetch("merchant", ""), "schemas": fetch("admin", "/schemas"), "policy": fetch("merchant", "/policy")}
	raw, e := json.MarshalIndent(out, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"synthetic-only-secret", "fixture-app-one", "fixture-app-two"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("fixture escaped private provider values")
		}
	}
	if e = os.WriteFile(target, raw, 0600); e != nil {
		t.Fatal(e)
	}
}
