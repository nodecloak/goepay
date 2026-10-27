package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuyerCharge104OriginalCapabilitiesAndMerchantListShowVerifiedActual(t *testing.T) {
	f := buyer104MerchantLocal(t)
	var v struct {
		Data struct {
			Fingerprint string `json:"fingerprint"`
		}
	}
	w := f.buyer("GET", "", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"mode": 1, "mode_feerate": "", "expected_fingerprint": v.Data.Fingerprint})
	if w = f.buyer("PATCH", "", string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	f.configure(t, 7, "94")
	f.group(t, nil)
	if _, e := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "capability-actual104", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(t.Context(), 1, "capability-actual104")
	if e != nil {
		t.Fatal(e)
	}
	caps, e := f.s.ProviderFlow.OriginalOrderCapabilities(t.Context(), 1, []int64{o.ID})
	if e != nil || !caps[o.ID].Query {
		t.Fatal("authenticated104 birth lost original provider capability", caps, e)
	}
	r := httptest.NewRequest("GET", "/console/v1/merchant/orders", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.merchantToken})
	w = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var list struct {
		Data struct {
			Items []map[string]any `json:"items"`
		}
	}
	if json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Data.Items) != 1 {
		t.Fatal(w.Body.String())
	}
	row := list.Data.Items[0]
	if row["amount"] != "100" || row["realmoney"] != "106" || row["charge_verified104"] != true {
		t.Fatal("original M and verified actual C not distinct", row)
	}
}
