package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

// An install that never saved 支付方式 still routes alipay and wxpay
// (payTypeCatalogOpen). The test page offers them too, like the original
// pre_type rows, under the reserved catalog typeid, and pays through the
// ordinary route.
func TestPublicTestPay203OffersCatalogDefaultWithoutPayTypeRecord(t *testing.T) {
	f := publicTestPay203Ready(t)
	if _, err := f.p.Exec(t.Context(), `DELETE FROM backoffice_records WHERE resource='pay-types'`); err != nil {
		t.Fatal(err)
	}
	init, cookies := publicTestPay203ReadInit(t, f)
	const want = "9000000000000000000"
	if len(init.PayTypes) != 1 || init.PayTypes[0].ID != want || init.PayTypes[0].Name != "alipay" || init.PayTypes[0].ShowName != "支付宝" {
		t.Fatalf("catalog default 支付宝 not offered: %+v", init.PayTypes)
	}
	body, _ := json.Marshal(map[string]string{"money": "1.23", "typeid": want, "csrf_token": init.CSRF})
	out := publicTestPay203Request(f, "POST", publicTestPay203Prefix+"/submit", string(body), cookies)
	if out.Code != http.StatusOK {
		t.Fatalf("catalog default did not reach the ordinary payment route: HTTP=%d", out.Code)
	}
	var kind string
	var amount int64
	if err := f.p.QueryRow(t.Context(), `SELECT type,amount FROM orders`).Scan(&kind, &amount); err != nil || kind != "alipay" || amount != 123 {
		t.Fatal("catalog default created the wrong order", kind, amount, err)
	}
	if f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 1 {
		t.Fatal("catalog default did not send exactly once")
	}
}

// A type an administrator saved and switched off stays closed: the default
// covers only types without any record, as in payTypeCatalogOpen.
func TestPublicTestPay203DisabledPayTypeRecordHidesCatalogDefault(t *testing.T) {
	f := publicTestPay203Ready(t)
	if _, err := f.p.Exec(t.Context(), `UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','false') WHERE resource='pay-types' AND id=700203`); err != nil {
		t.Fatal(err)
	}
	init, cookies := publicTestPay203ReadInit(t, f)
	if len(init.PayTypes) != 0 {
		t.Fatalf("switched-off 支付方式 still offered: %+v", init.PayTypes)
	}
	body, _ := json.Marshal(map[string]string{"money": "1.00", "typeid": "9000000000000000000", "csrf_token": init.CSRF})
	if out := publicTestPay203Request(f, "POST", publicTestPay203Prefix+"/submit", string(body), cookies); out.Code == http.StatusOK {
		t.Fatal("a closed type was paid through the catalog typeid")
	}
	var orders int
	if err := f.p.QueryRow(t.Context(), `SELECT count(*) FROM orders`).Scan(&orders); err != nil || orders != 0 {
		t.Fatal("closed type created an order", orders, err)
	}
}
