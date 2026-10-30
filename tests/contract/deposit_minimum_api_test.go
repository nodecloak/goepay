package contract_test

import (
	"context"
	"testing"

	"goepay/internal/compat"
)

// epay_pro: 当前平台要求最低缴纳保证金 N 元，否则无法调用接口发起支付. With a minimum
// set and nothing held, the merchant payment API refuses the order and names
// the reason; with no minimum the same request creates the order.
func TestPaymentAPIRefusesMerchantBelowDepositMinimum(t *testing.T) {
	s, pool, _ := fixture(t)
	h := s.Handler()
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"user_deposit_min","value":"10.00"}')`); e != nil {
		t.Fatal(e)
	}
	order := compat.Params{"pid": "1", "out_trade_no": "deposit-min-1", "money": "1.00", "name": "test", "type": "sandbox"}
	_, refused := call(t, h, "POST", "/mapi.php", signed(order, "contract-secret"))
	if refused["code"] != float64(-1) || refused["msg"] != "保证金低于平台最低要求，暂不能发起支付" {
		t.Fatalf("below the deposit minimum: %v", refused)
	}
	var count int
	if e := pool.QueryRow(ctx, "SELECT count(*) FROM orders WHERE out_trade_no='deposit-min-1'").Scan(&count); e != nil || count != 0 {
		t.Fatalf("refused order was stored: count=%d err=%v", count, e)
	}
	if _, e := pool.Exec(ctx, `UPDATE backoffice_records SET body='{"key":"user_deposit_min","value":"0"}' WHERE resource='settings' AND body->>'key'='user_deposit_min'`); e != nil {
		t.Fatal(e)
	}
	order["out_trade_no"] = "deposit-min-2"
	_, created := call(t, h, "POST", "/mapi.php", signed(order, "contract-secret"))
	if created["code"] != float64(1) || created["trade_no"] == nil {
		t.Fatalf("no minimum: %v", created)
	}
}
