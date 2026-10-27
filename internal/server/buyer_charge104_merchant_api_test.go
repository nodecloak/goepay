package server

import (
	"encoding/json"
	"goepay/internal/compat"
	"net/http/httptest"
	"testing"
)

func TestBuyerCharge104MerchantWireKeepsOriginalMoneyAndActualRefundStatus(t *testing.T) {
	f := buyer104MerchantLocal(t)
	v := channel168Data(t, f.buyer("GET", "", ""))
	body, _ := json.Marshal(map[string]any{"mode": 1, "mode_feerate": "", "expected_fingerprint": v["fingerprint"]})
	channel168Data(t, f.buyer("PATCH", "", string(body)))
	f.configure(t, 7, "94")
	f.group(t, nil)
	if _, e := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "wire-refund-C104", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(t.Context(), 1, "wire-refund-C104")
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Store.MarkPaid(t.Context(), o.TradeNo, "synthetic-wire104"); e != nil {
		t.Fatal(e)
	}
	refund, e := f.s.Store.ReserveRefund(t.Context(), 1, o.OutTradeNo, "refund-M104", 100)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Store.ResolveRefund(t.Context(), refund.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	request := httptest.NewRequest("GET", "/api/order/list", nil)
	read := func(status string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		f.s.merchantOrders(w, request, false, 1, compat.Params{"status": status})
		var got map[string]any
		if e = json.Unmarshal(w.Body.Bytes(), &got); e != nil {
			t.Fatal(e)
		}
		return got
	}
	got := read("1")
	items, ok := got["data"].([]any)
	if !ok || len(items) != 1 {
		t.Fatal("R==M<C disappeared from paid list", got)
	}
	item := items[0].(map[string]any)
	if item["money"] != "1.00" || item["refundmoney"] != "1.00" || item["status"] != float64(1) {
		t.Fatal("merchant wire changed M or erased C remainder", item)
	}
	got = read("2")
	if rows, ok := got["data"].([]any); !ok || len(rows) != 0 {
		t.Fatal("R==M incorrectly fully refunded", got)
	}
	if _, e = f.p.Exec(t.Context(), `ALTER TABLE order_customer_charges104 DISABLE TRIGGER USER;DELETE FROM order_customer_charges104;ALTER TABLE order_customer_charges104 ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if got = read(""); got["code"] != float64(-1) {
		t.Fatal("wire list returned unconfirmed actual source", got)
	}
}
