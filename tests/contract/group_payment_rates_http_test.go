package contract_test

import (
	"context"
	"goepay/internal/compat"
	"testing"
)

func TestOriginalGroupPaymentTypeRatesHTTPPreservesGrossAndFreezesRetainedPercent(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	var group int64
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"local-rates141","enabled":true,"rate_bps":"100","info":{"sandbox":{"rate":"99.50"}}}') RETURNING id`).Scan(&group); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{group_id}',to_jsonb($1::bigint::text)) WHERE id=1`, group); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	h := s.Handler()
	base := compat.Params{"pid": "1", "out_trade_no": "local-type-rates141", "money": "100.01", "name": "local-only", "type": "sandbox"}
	_, first := call(t, h, "POST", "/mapi.php", signed(base, "contract-secret"))
	if first["code"] != float64(1) {
		t.Fatal(first)
	}
	order, e := s.Store.Order(ctx, 1, base["out_trade_no"])
	if e != nil || order.Amount != 10001 || order.MerchantAmount != 9951 || order.PlatformFee != 50 || order.FeeRateBPS != 50 {
		t.Fatalf("percent interpreted as platform fee %+v %v", order, e)
	}
	if _, e := pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info,sandbox,rate}','"90"') WHERE id=$1`, group); e != nil {
		t.Fatal(e)
	}
	_, again := call(t, h, "POST", "/mapi.php", signed(base, "contract-secret"))
	if again["code"] != float64(1) || again["trade_no"] != first["trade_no"] {
		t.Fatal("historical payment changed", again)
	}
	same, e := s.Store.Order(ctx, 1, base["out_trade_no"])
	if e != nil || same.MerchantAmount != 9951 || same.PlatformFee != 50 {
		t.Fatal("original snapshot changed", same, e)
	}
	base["out_trade_no"] = "new-type-rates141"
	_, next := call(t, h, "POST", "/mapi.php", signed(base, "contract-secret"))
	if next["code"] != float64(1) {
		t.Fatal(next)
	}
	newer, e := s.Store.Order(ctx, 1, base["out_trade_no"])
	if e != nil || newer.MerchantAmount != 9001 || newer.PlatformFee != 1000 {
		t.Fatal(newer, e)
	}
}
