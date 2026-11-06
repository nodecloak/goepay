package contract_test

import (
	"sync"
	"testing"

	"goepay/internal/compat"
)

func TestAutomaticRefundHTTPConcurrentRetriesSendOnce(t *testing.T) {
	f, p, o := merchant174Runtime(t)
	const copies = 12
	replies := make(chan map[string]any, copies)
	var wg sync.WaitGroup
	for range copies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			replies <- merchant174Invoke(t, f, "/api/pay/refund", compat.Params{"out_trade_no": o.OutTradeNo, "money": "1.00"})
		}()
	}
	wg.Wait()
	close(replies)
	var number, id any
	for reply := range replies {
		if reply["code"] != float64(0) || reply["refund_state"] != "processing" || reply["out_refund_no"] == nil || reply["refund_no"] == nil {
			t.Fatal("concurrent API reply lost original refund", reply)
		}
		if number == nil {
			number = reply["out_refund_no"]
			id = reply["refund_no"]
		}
		if reply["out_refund_no"] != number || reply["refund_no"] != id {
			t.Fatal("same HTTP request allocated two refunds", number, id, reply)
		}
	}
	if p.refundWrites.Load() != 1 {
		t.Fatal("local provider called more than once", p.refundWrites.Load())
	}
	b, e := f.s.Store.Balance(t.Context(), o.MerchantID)
	if e != nil || b.Available != 900 || b.Reserved != 100 {
		t.Fatal("concurrent HTTP retry reserved twice", b, e)
	}
	var reservations, operations int
	if e = f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM refunds WHERE order_id=$1),(SELECT count(*) FROM provider_operations WHERE kind='refund' AND order_id=$1)`, o.ID).Scan(&reservations, &operations); e != nil || reservations != 1 || operations != 1 {
		t.Fatal("duplicate durable refund intent", reservations, operations, e)
	}
	report, e := f.s.Store.CheckLedger(t.Context())
	if e != nil || report.InvalidJournals != 0 || report.InvalidAccounts != 0 || report.InvalidProviderRefundResults != 0 {
		t.Fatal("refund retry violated ledger invariants", report, e)
	}
}
