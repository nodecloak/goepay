package contract_test

import (
	"context"
	"goepay/internal/compat"
	"testing"
)

func TestOriginalPaymentLimitsHTTPRejectBeforePersistAndPreserveReplay(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"pay_minmoney","value":"1.00"}'),('settings','{"key":"pay_maxmoney","value":"2.00"}')`); e != nil {
		t.Fatal(e)
	}
	h := s.Handler()
	request := func(no, money string) map[string]any {
		_, out := call(t, h, "POST", "/mapi.php", signed(compat.Params{"pid": "1", "out_trade_no": no, "money": money, "name": "local limits140", "type": "sandbox"}, "contract-secret"))
		return out
	}
	for _, p := range [][2]string{{"below", "0.99"}, {"above", "2.01"}} {
		if out := request(p[0], p[1]); out["code"] != float64(-1) {
			t.Fatal("original limit ignored", out)
		}
	}
	var orders int
	if e := pool.QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&orders); e != nil || orders != 0 {
		t.Fatalf("rejected before persistence %d %v", orders, e)
	}
	original := request("original", "1.00")
	if original["code"] != float64(1) {
		t.Fatal(original)
	}
	if _, e := pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"1.50"') WHERE resource='settings' AND body->>'key'='pay_minmoney'`); e != nil {
		t.Fatal(e)
	}
	same := request("original", "1.00")
	if same["code"] != float64(1) || same["trade_no"] != original["trade_no"] {
		t.Fatal("policy change broke original replay", same)
	}
	if out := request("new-low", "1.00"); out["code"] != float64(-1) {
		t.Fatal(out)
	}
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"pay_minmoney":"1.75","pay_maxmoney":"1.90"}'::jsonb WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	for _, p := range [][2]string{{"own-min", "1.74"}, {"own-max", "1.91"}} {
		if out := request(p[0], p[1]); out["code"] != float64(-1) {
			t.Fatal("own merchant bound ignored", out)
		}
	}
	if out := request("own-boundary", "1.75"); out["code"] != float64(1) {
		t.Fatal(out)
	}
	if e := pool.QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&orders); e != nil || orders != 2 {
		t.Fatalf("unexpected new orders %d %v", orders, e)
	}
	var journals int
	if e := pool.QueryRow(ctx, `SELECT count(*) FROM journals`).Scan(&journals); e != nil || journals != 0 {
		t.Fatal("creation moved funds", journals, e)
	}
}
