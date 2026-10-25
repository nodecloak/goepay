package payment

import (
	"context"
	"testing"
)

func TestOriginalGroupPaymentTypeRatesParserRejectsAmbiguousOrUnsupportedInputs(t *testing.T) {
	for _, raw := range []string{`[]`, `null`, `{"alipay":{"rate":null}}`, `{"alipay":{"rate":"99","rate":"98"}}`, `{"alipay":{"rate":"99"},"alipay":{"rate":"98"}}`, `{"alipay":{"rate":"99","channel":1}}`, `{"wxpay":{"rate":"100.01"}}`, `{"wxpay":{"rate":"1e2"}}`, `{"wxpay":{"rate":"0.001"}}`, `{"bank":{"rate":"99"}}`, `{} {}`} {
		if _, e := ParseGroupRetainedRates([]byte(raw)); e == nil {
			t.Fatal("ambiguous rate accepted", raw)
		}
	}
	rates, e := ParseGroupRetainedRates([]byte(`{"alipay":{"rate":"99.50"},"wxpay":{"rate":"100"},"sandbox":{"rate":"0"}}`))
	if e != nil || rates["alipay"] != 9950 || rates["wxpay"] != 10000 || len(rates) != 2 {
		t.Fatal(rates, e)
	}
}

func TestOriginalGroupPaymentTypeRatesRetainedRoundingAndHistoricalSnapshot(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 1411, 38, "platform")
	if _, e := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"info":{"alipay":{"rate":"99.50"},"wxpay":{"rate":"98.75"}}}'::jsonb WHERE resource='groups'`); e != nil {
		t.Fatal(e)
	}
	var original Order
	for _, tc := range []struct {
		kind           string
		fee, net, rate int64
	}{{"alipay", 50, 9951, 50}, {"wxpay", 125, 9876, 125}, {"sandbox", 38, 9963, 38}} {
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1411, OutTradeNo: tc.kind, Type: tc.kind, Amount: 10001})
		if e != nil || o.FeeRateBPS != tc.rate || o.MerchantAmount != tc.net || o.PlatformFee != tc.fee {
			t.Fatalf("retained %s %+v %v", tc.kind, o, e)
		}
		if tc.kind == "alipay" {
			original = o
		}
	}
	if _, e := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info,alipay,rate}','"90.00"') WHERE resource='groups'`); e != nil {
		t.Fatal(e)
	}
	same, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1411, OutTradeNo: "alipay", Type: "alipay", Amount: 10001})
	if e != nil || same.TradeNo != original.TradeNo || same.MerchantAmount != 9951 {
		t.Fatalf("repriced historical order %+v %v", same, e)
	}
	if e = s.MarkPaid(ctx, original.TradeNo, "local-rates141-paid"); e != nil {
		t.Fatal(e)
	}
	refund, e := s.ReserveRefund(ctx, 1411, "alipay", "local-rates141-refund", 10001)
	if e != nil || refund.MerchantAmount != 9951 || refund.FeeAmount != 50 {
		t.Fatalf("refund repriced original retained amount and platform fee %+v %v", refund, e)
	}
	if e = s.ResolveRefund(ctx, refund.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	balance, e := s.Balance(ctx, 1411)
	if e != nil || balance.Available != 0 || balance.Reserved != 0 {
		t.Fatalf("refund did not consume the original net exactly %+v %v", balance, e)
	}
}

func TestOriginalGroupPaymentTypeRatesEmptyZeroAndFullRetained(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 1412, 100, "platform")
	if _, e := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"info":{"alipay":{"rate":""},"wxpay":{"rate":"0"},"sandbox":{"rate":"100"}}}'::jsonb WHERE resource='groups'`); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		kind string
		rate int64
	}{{"alipay", 100}, {"wxpay", 100}, {"sandbox", 0}} {
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1412, OutTradeNo: tc.kind, Type: tc.kind, Amount: 9007199254740993})
		net, fee, _ := calculateFee(9007199254740993, tc.rate)
		if e != nil || o.FeeRateBPS != tc.rate || o.MerchantAmount != net || o.PlatformFee != fee {
			t.Fatalf("exact override %s %+v %v", tc.kind, o, e)
		}
	}
}
