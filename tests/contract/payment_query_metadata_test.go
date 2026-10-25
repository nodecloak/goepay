package contract_test

import (
	"context"
	"fmt"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/url"
	"testing"
	"time"
)

func TestPaymentQueryBusinessTimeAndSettledRefundMetadata(t *testing.T) {
	s, pool, key := fixture(t)
	ctx := context.Background()
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "query-metadata", Name: "test", Type: "sandbox", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "local-test-paid"); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.SetOrderClientIP(ctx, o.ID, "192.0.2.1"); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "UPDATE orders SET created_at='2026-01-01 20:30:00+00',paid_at='2026-01-01 20:31:00+00' WHERE id=$1", o.ID); e != nil {
		t.Fatal(e)
	}
	query := func() map[string]any {
		_, out := call(t, s.Handler(), "GET", "/api.php", url.Values{"act": {"order"}, "pid": {"1"}, "key": {"contract-secret"}, "trade_no": {o.TradeNo}})
		return out
	}
	out := query()
	if out["addtime"] != "2026-01-02 04:30:00" || out["endtime"] != "2026-01-02 04:31:00" || out["clientip"] != "192.0.2.1" || out["status"] != float64(1) {
		t.Fatal(out)
	}
	// A pending/unknown refund must not advertise refunded funds.
	r, e := s.Store.ReserveRefund(ctx, 1, o.OutTradeNo, "query-refund-1", 25)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := query()["refundmoney"]; ok {
		t.Fatal("pending refund shown as completed")
	}
	if e = s.Store.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	out = query()
	if out["refundmoney"] != "0.25" || out["status"] != float64(1) {
		t.Fatal(out)
	}
	r, e = s.Store.ReserveRefund(ctx, 1, o.OutTradeNo, "query-refund-2", 75)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	out = query()
	if out["status"] != float64(2) {
		t.Fatal(out)
	}
	_, listing := call(t, s.Handler(), "GET", "/api.php", url.Values{"act": {"orders"}, "pid": {"1"}, "key": {"contract-secret"}, "status": {"2"}})
	rows, ok := listing["data"].([]any)
	if listing["code"] != float64(1) || !ok || len(rows) != 1 || rows[0].(map[string]any)["status"] != float64(2) {
		t.Fatal("refunded order list contract", listing)
	}
	if e = s.Store.CreateMerchant(ctx, payment.Merchant{ID: 3, Name: "other", APIKey: "foreign-contract-secret"}); e != nil {
		t.Fatal(e)
	}
	if _, foreign := call(t, s.Handler(), "GET", "/api.php", url.Values{"act": {"order"}, "pid": {"3"}, "key": {"foreign-contract-secret"}, "trade_no": {o.TradeNo}}); foreign["code"] == float64(1) {
		t.Fatal("cross-merchant metadata disclosed")
	}
	// V2 must include the same metadata in the actual SDK-verifiable response.
	v2, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "v2-query-metadata", Name: "中文订单", Type: "sandbox", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, v2.TradeNo, "v2-metadata-paid"); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO verified_payment_receipts(order_id,channel_id,provider_trade_no) VALUES($1,9,'upstream-v2')", v2.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO order_reporting(order_id,buyer,client_ip) VALUES($1,'verified-buyer','2001:db8::5')", v2.ID); e != nil {
		t.Fatal(e)
	}
	p := compat.Params{"pid": "2", "trade_no": v2.TradeNo, "timestamp": fmt.Sprint(time.Now().Unix()), "sign_type": "RSA"}
	p["sign"], _ = compat.SignRSA(p, key)
	v := url.Values{}
	for k, x := range p {
		v.Set(k, x)
	}
	_, out = call(t, s.Handler(), "POST", "/api/pay/query", v)
	if out["code"] != float64(0) || out["api_trade_no"] != "upstream-v2" || out["buyer"] != "verified-buyer" || out["clientip"] != "2001:db8::5" || out["endtime"] == nil {
		t.Fatal(out)
	}
	signed := compat.Params{}
	for k, x := range out {
		signed[k] = fmt.Sprint(x)
	}
	if !compat.VerifyRSAWithProfile(signed, signed["sign"], &key.PublicKey, compat.ProfileSDKV2) {
		t.Fatal("metadata response signature mismatch")
	}
}
