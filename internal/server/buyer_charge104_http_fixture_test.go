package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuyerCharge104ExportDisposedActualHTTPFixture(t *testing.T) {
	destination := os.Getenv("GOEPAY_BUYER104_HTTP_FIXTURE_PATH")
	if destination == "" {
		t.Skip("explicit synthetic public HTTP fixture export path required")
	}
	if !filepath.IsAbs(destination) || !strings.HasSuffix(filepath.ToSlash(destination), "/web/e2e/fixtures/buyer-charge104-http.json") {
		t.Fatal("scoped public fixture path required")
	}
	f := buyer104MerchantLocal(t)
	first := channel168Data(t, f.buyer("GET", "", ""))
	body, _ := json.Marshal(map[string]any{"mode": 1, "mode_feerate": "3.125", "expected_fingerprint": first["fingerprint"]})
	partial := channel168Data(t, f.buyer("PATCH", "", string(body)))
	global := channel168Data(t, f.buyerMinimum104("GET", "global", ""))
	parent := channel168Data(t, f.buyerMinimum104("GET", "parents/7", ""))
	f.configure(t, 7, "94")
	f.group(t, nil)
	if _, e := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "public-fixture104", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(t.Context(), 1, "public-fixture104")
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Store.MarkPaid(t.Context(), o.TradeNo, "synthetic-fixture104"); e != nil {
		t.Fatal(e)
	}
	refund, e := f.s.Store.ReserveRefund(t.Context(), 1, o.OutTradeNo, "public-remainder104", 100)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Store.ResolveRefund(t.Context(), refund.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	request := httptest.NewRequest("GET", "/console/v1/merchant/orders", nil)
	request.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.merchantToken})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, request)
	list := channel168Data(t, w)
	items, ok := list["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatal(list)
	}
	row := items[0].(map[string]any)
	if row["amount"] != "100" || row["realmoney"] != "103" || row["refunded_amount"] != "100" || row["charge_verified104"] != true {
		t.Fatal("actual public dual amount missing", row)
	}
	output, e := json.MarshalIndent(map[string]any{"synthetic_only": true, "production": false, "real_funds": false, "source": "actual migrated local PostgreSQL and HTTP handlers; schema/session disposed by test cleanup", "merchant_default": first, "merchant_partial": partial, "global": global, "parent": parent, "order_list": list}, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	for _, private := range []string{f.token, f.csrf, f.merchantToken, f.merchantCSRF, "proof_hash", "SourceProof", "RouteProof"} {
		if strings.Contains(string(output), private) {
			t.Fatal("private material in public fixture")
		}
	}
	if e = os.WriteFile(destination, append(output, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
}
