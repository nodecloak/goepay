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

// A-01: api.php?act=settle returns the authenticated merchant's own settlement
// records (pre_settle equivalent), code=1, offset/limit capped at 50, scoped.
func TestLegacySettleReturnsMerchantSettlementRecords(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, e := pool.Exec(ctx, "INSERT INTO ledger_requests(kind,merchant_id,external_no,amount) VALUES('settlements',1,$1,100)", fmt.Sprintf("settle-%d", i)); e != nil {
			t.Fatal(e)
		}
	}
	// A settlement record for another merchant must not leak.
	if _, e := pool.Exec(ctx, "INSERT INTO ledger_requests(kind,merchant_id,external_no,amount) VALUES('settlements',2,'foreign-settle',100)"); e != nil {
		t.Fatal(e)
	}
	_, out := call(t, s.Handler(), "GET", "/api.php", url.Values{"act": {"settle"}, "pid": {"1"}, "key": {"contract-secret"}})
	if out["code"] != float64(1) || out["msg"] != "查询结算记录成功！" {
		t.Fatalf("settle shape %v", out)
	}
	rows, ok := out["data"].([]any)
	if !ok || len(rows) != 3 {
		t.Fatalf("settle records not scoped to merchant: %v", out["data"])
	}
	for _, r := range rows {
		if r.(map[string]any)["uid"] != float64(1) {
			t.Fatalf("foreign settlement leaked: %v", r)
		}
	}
	// limit cap 50.
	_, capped := call(t, s.Handler(), "GET", "/api.php", url.Values{"act": {"settle"}, "pid": {"1"}, "key": {"contract-secret"}, "limit": {"9999"}})
	if capped["code"] != float64(1) {
		t.Fatalf("settle limit cap rejected %v", capped)
	}
}

// A-06: legacy api.php auth failures return code -3 with the original messages.
func TestLegacyApiAuthErrorsUseOriginalCodesAndMessages(t *testing.T) {
	s, _, _ := fixture(t)
	h := s.Handler()
	for _, tc := range []struct {
		name string
		v    url.Values
		msg  string
	}{
		{"unknown pid", url.Values{"act": {"query"}, "pid": {"9999"}, "key": {"x"}}, "商户ID不存在"},
		{"wrong key", url.Values{"act": {"query"}, "pid": {"1"}, "key": {"wrong"}}, "商户密钥错误"},
		{"rsa-only", url.Values{"act": {"query"}, "pid": {"2"}, "key": {"contract-rsa-secret"}}, "该商户只能使用RSA签名类型"},
	} {
		_, out := call(t, h, "GET", "/api.php", tc.v)
		if out["code"] != float64(-3) || out["msg"] != tc.msg {
			t.Fatalf("%s: want code=-3 msg=%q, got %v", tc.name, tc.msg, out)
		}
	}
}

// A-07: a v2 query on an unpaid order returns endtime present-but-empty, mirroring
// the legacy branch, rather than omitting the key.
func TestV2QueryUnpaidOrderReturnsEmptyEndtime(t *testing.T) {
	s, _, key := fixture(t)
	ctx := context.Background()
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "unpaid-endtime", Name: "test", Type: "sandbox", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	p := compat.Params{"pid": "2", "trade_no": o.TradeNo, "timestamp": fmt.Sprint(time.Now().Unix()), "sign_type": "RSA"}
	p["sign"], _ = compat.SignRSA(p, key)
	v := url.Values{}
	for k, x := range p {
		v.Set(k, x)
	}
	_, out := call(t, s.Handler(), "POST", "/api/pay/query", v)
	if out["code"] != float64(0) {
		t.Fatalf("v2 query failed %v", out)
	}
	end, present := out["endtime"]
	if !present || end != "" {
		t.Fatalf("v2 unpaid endtime must be present and empty, got present=%v value=%v", present, end)
	}
}
