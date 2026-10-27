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

func TestMerchantCompatibilityInfoOrdersAndTransferReads(t *testing.T) {
	s, pool, key := fixture(t)
	ctx := context.Background()
	for mid := int64(1); mid <= 2; mid++ {
		for i := 0; i < 3; i++ {
			o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprintf("read-%d-%d", mid, i), Name: "test", Type: "sandbox", Amount: 100})
			if e != nil {
				t.Fatal(e)
			}
			if i < 2 {
				s.Store.MarkPaid(ctx, o.TradeNo, fmt.Sprintf("event-%d-%d", mid, i))
			}
		}
	}
	invoke := func(path string, p compat.Params) map[string]any {
		t.Helper()
		p["pid"] = "2"
		p["timestamp"] = fmt.Sprint(time.Now().Unix())
		p["sign_type"] = "RSA"
		p["sign"], _ = compat.SignRSA(p, key)
		v := url.Values{}
		for k, x := range p {
			v.Set(k, x)
		}
		_, out := call(t, s.Handler(), "POST", path, v)
		return out
	}
	out := invoke("/api/merchant/info", compat.Params{})
	if out["code"] != float64(0) || out["order_num"] != float64(3) || out["money"] != "2.00" || out["order_money_today"] != "2.00" {
		t.Fatal(out)
	}
	out = invoke("/api/merchant/orders", compat.Params{"offset": "1", "limit": "1", "status": "1"})
	if out["code"] != float64(0) {
		t.Fatal(out)
	}
	rows := out["data"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["pid"] != float64(2) {
		t.Fatal("orderlist scope", out)
	}
	signedResponse := compat.Params{}
	for k, x := range out {
		if k != "data" {
			signedResponse[k] = fmt.Sprint(x)
		}
	}
	if !compat.VerifyRSAWithProfile(signedResponse, signedResponse["sign"], &key.PublicKey, compat.ProfileSDKV2) {
		t.Fatal("array must be excluded from SDK signature")
	}
	_, legacy := call(t, s.Handler(), "GET", "/api.php", url.Values{"act": {"query"}, "pid": {"1"}, "key": {"contract-secret"}})
	if legacy["order_today"] != float64(2) || legacy["orders"] != float64(3) || legacy["key"] != "contract-secret" {
		t.Fatal("legacy pro fields", legacy)
	}
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"transfer_api_enabled":true}'::jsonb WHERE id=2`); e != nil {
		t.Fatal(e)
	}
	out = invoke("/api/transfer/balance", compat.Params{})
	if out["available_money"] != "2.00" {
		t.Fatal(out)
	}
	if _, e := pool.Exec(ctx, "INSERT INTO transfers(merchant_id,transfer_no,amount) VALUES(2,'pending-transfer',25)"); e != nil {
		t.Fatal(e)
	}
	out = invoke("/api/transfer/query", compat.Params{"out_biz_no": "pending-transfer"})
	if out["status"] != float64(0) || out["amount"] != "0.25" {
		t.Fatal(out)
	}
	out = invoke("/api/transfer/submit", compat.Params{"money": "1.00", "type": "alipay", "account": "test"})
	if out["code"] != float64(-1) {
		t.Fatal("unsupported transfer reported success")
	}
}
