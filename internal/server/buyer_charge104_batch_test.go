package server

import (
	"encoding/json"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"testing"
)

func TestBuyerCharge104BatchRefundFreezesActualRemainingAfterOriginalFullyReturned(t *testing.T) {
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
	if out := f.pay(t, "remaining-batch104", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	o, e := f.s.Store.Order(t.Context(), 1, "remaining-batch104")
	if e != nil {
		t.Fatal(e)
	}
	c, e := f.s.Store.AuthenticatedOrderCharge104(t.Context(), o)
	if e != nil || c != 106 {
		t.Fatal("actual routed quote", c, e)
	}
	if e = f.s.Store.MarkPaid(t.Context(), o.TradeNo, "synthetic-no-funds104"); e != nil {
		t.Fatal(e)
	}
	r, e := f.s.Store.ReserveRefund(t.Context(), 1, o.OutTradeNo, "original-only104", 100)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Store.ResolveRefund(t.Context(), r.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	a := payment.OrderBusinessBatchActor{User: identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1}, Token: f.token}
	prepared, e := f.s.Store.PrepareOrderBusinessBatch(t.Context(), a, payment.OrderBusinessBatchInput{ExternalNo: "remaining-charge104", Action: "refund", Reason: "synthetic remaining actual fee", Items: []payment.OrderBusinessBatchSelection{{TradeNo: o.TradeNo}}})
	if e != nil {
		t.Fatal("R==M erased actual batch remainder", e)
	}
	selection, e := f.s.Store.OrderBusinessBatchFundsSelection(t.Context(), f.p, prepared.ID)
	if e != nil || len(selection.Items) != 1 || selection.Items[0].Amount != 6 {
		t.Fatal("confirmed batch funds snapshot lost actual6", selection, e)
	}
}
