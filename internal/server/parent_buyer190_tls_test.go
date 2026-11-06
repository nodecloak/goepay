package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
)

// Exercise both independently authenticated sources through real XML signatures,
// mTLS and HTTP callbacks. The peer records the amount actually sent on the wire.
func TestParentBuyer190ActualChildTLSChargeCallbackAndRefundAcrossPolicyRotation(t *testing.T) {
	for _, tc := range []struct {
		name, rate  string
		mode        int
		charge, net int64
	}{
		{"full", "", 1, 10200, 10000},
		{"partial", "0.25", 1, 10025, 9825},
		{"merchant", "", 0, 10000, 9800},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setup, _, child := child189Setup(t)
			f, ctx := setup.f, t.Context()
			merchant := buyer190MerchantOn(t, f)
			setMerchant := func(mode int, rate string) {
				t.Helper()
				state := channel168Data(t, merchant.buyer("GET", "", ""))
				body, _ := json.Marshal(map[string]any{"mode": mode, "mode_feerate": rate, "expected_fingerprint": state["fingerprint"]})
				if w := merchant.buyer("PATCH", "", string(body)); w.Code != 200 {
					t.Fatal("merchant fee policy", w.Code)
				}
			}
			setMinimum := func(value string) {
				t.Helper()
				state := channel168Data(t, f.buyerMinimum104("GET", "parents/"+setup.id, ""))
				body, _ := json.Marshal(map[string]any{"payfee_lessthan": "1.00", "payfee_mincost": value, "expected_fingerprint": state["fingerprint"]})
				if w := f.buyerMinimum104("PATCH", "parents/"+setup.id, string(body)); w.Code != 200 {
					t.Fatal("parent minimum", w.Code)
				}
			}
			setMerchant(tc.mode, tc.rate)
			setMinimum("2.00")
			no := "combined190-" + tc.name
			child189Pay(t, f, child["id"].(string), no)
			o, err := f.s.Store.Order(ctx, 1, no)
			if err != nil {
				t.Fatal(err)
			}
			if c, err := f.s.Store.AuthenticatedOrderCharge104(ctx, o); err != nil || c != tc.charge || o.Amount != 10000 || o.MerchantAmount != tc.net || o.PlatformFee != 200 || setup.wireCharge(o.TradeNo) != fmt.Sprint(tc.charge) {
				t.Fatal("combined sources changed original/charged/net/fee or actual wire", c, o.Amount, o.MerchantAmount, o.PlatformFee, setup.wireCharge(o.TradeNo), err)
			}
			// Change both current fee policy and the independent parent SDK. The
			// original child's account and actual charge must stay unchanged.
			setMerchant(0, "")
			setMinimum("3.00")
			parent180RotateOutgoing(t, setup)
			callback := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "child189-A", "mch_id": "child189-merchant-A", "out_trade_no": o.TradeNo, "transaction_id": "W180" + o.TradeNo, "total_fee": fmt.Sprint(tc.charge + 1), "trade_type": "NATIVE", "openid": "synthetic190-buyer"}
			send := func() int {
				r := httptest.NewRequest("POST", "/callbacks/providers/"+setup.id+"/"+o.TradeNo, bytes.NewReader(parent180SignedXML(callback, "abcdef0123456789abcdef0123456789")))
				r.Header.Set("Content-Type", "application/xml")
				w := httptest.NewRecorder()
				f.s.Handler().ServeHTTP(w, r)
				return w.Code
			}
			if status := send(); status != 400 {
				t.Fatal("wrong charge callback accepted", status)
			}
			pending, err := f.s.Store.Order(ctx, 1, no)
			if err != nil || pending.Status != "pending" {
				t.Fatal("wrong callback changed state", pending.Status, err)
			}
			callback["total_fee"] = fmt.Sprint(tc.charge)
			if status := send(); status != 200 {
				t.Fatal("original charge/child callback rejected", status)
			}
			if _, err = f.s.ProviderFlow.Query(ctx, 1, o.TradeNo); err != nil {
				t.Fatal("original child/charge query", err)
			}
			if _, err = f.s.ProviderFlow.Refund(ctx, 1, o.TradeNo, "full-original190-"+tc.name, tc.charge); err != nil {
				t.Fatal("original actual-charge refund", err)
			}
			if setup.creates.Load() != 1 || setup.queries.Load() != 1 || setup.refunds.Load() != 1 || setup.bad.Load() != 0 {
				t.Fatal("unexpected real local peer traffic", setup.creates.Load(), setup.queries.Load(), setup.refunds.Load(), setup.bad.Load())
			}
			var imbalance int
			if err = f.p.QueryRow(ctx, `SELECT count(*) FROM (SELECT journal_id FROM ledger_entries GROUP BY journal_id HAVING sum(amount)<>0) x`).Scan(&imbalance); err != nil || imbalance != 0 {
				t.Fatal("unbalanced combined settlement/refund", imbalance, err)
			}
		})
	}
}
