package contract_test

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
)

func TestSequence169SignedHTTPDifferentGroupsShareExactParentsAndReplayOriginalIntent(t *testing.T) {
	s, pool, key := fixture(t)
	ctx := context.Background()
	const target int64 = 9007199254740993
	a, b := &groupRoutingProvider152{kind: "alipay"}, &groupRoutingProvider152{kind: "alipay"}
	s.CheckoutKey = bytes.Repeat([]byte{69}, 32)
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: a, target: b}, AllowLive: true, CallbackBaseURL: "https://synthetic.example.invalid"}
	s.MerchantChannels = map[int64][]int64{1: {target, 7}, 2: {target, 7}}
	for _, mid := range []int64{1, 2} {
		var gid int64
		if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"rate_bps":0,"info":{"alipay":{"channel":"-4","type":"channel","rate":"99.50"}}}') RETURNING id`).Scan(&gid); e != nil {
			t.Fatal(e)
		}
		if _, e := pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=$2`, gid, mid); e != nil {
			t.Fatal(e)
		}
	}
	digest := func() string {
		t.Helper()
		var out string
		if e := pool.QueryRow(ctx, `SELECT md5(COALESCE((SELECT jsonb_agg(to_jsonb(x) ORDER BY id)::text FROM orders x),'[]')||COALESCE((SELECT jsonb_agg(to_jsonb(x) ORDER BY channel_id)::text FROM provider_channel_last_used x),'[]')||COALESCE((SELECT jsonb_agg(to_jsonb(x) ORDER BY merchant_id,out_trade_no)::text FROM provider_route_assignments x),'[]'))`).Scan(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	for _, v2 := range []bool{false, true} {
		pid, path, want, code := "1", "/mapi.php", int64(7), float64(1)
		if v2 {
			pid, path, want, code = "2", "/api/pay/create", target, 0
		}
		params := compat.Params{"pid": pid, "out_trade_no": "sequence169-http-" + pid, "name": "synthetic169", "money": "1.00", "type": "alipay", "method": "native"}
		var values url.Values
		if v2 {
			params["timestamp"], params["sign_type"] = fmt.Sprint(time.Now().Unix()), "RSA"
			signature, e := compat.SignRSA(params, key)
			if e != nil {
				t.Fatal(e)
			}
			params["sign"] = signature
			values = url.Values{}
			for k, v := range params {
				values.Set(k, v)
			}
		} else {
			values = signed(params, "contract-secret")
		}
		_, out := call(t, s.Handler(), "POST", path, values)
		if out["code"] != code {
			t.Fatal("signed sequence request failed", v2, out)
		}
		trade, ok := out["trade_no"].(string)
		if !ok {
			t.Fatal("missing trade", out)
		}
		var actual int64
		if e := pool.QueryRow(ctx, `SELECT channel_id FROM provider_attempts p JOIN orders o ON o.id=p.order_id WHERE o.trade_no=$1`, trade).Scan(&actual); e != nil || actual != want {
			t.Fatal("separate groups did not share global original parent use", v2, actual, want, e)
		}
		before := digest()
		if _, out = call(t, s.Handler(), "POST", path, values); out["code"] != code || digest() != before {
			t.Fatal("signed replay rewrote order, route or global use", v2, out)
		}
	}
	if a.creates.Load() != 1 || b.creates.Load() != 1 {
		t.Fatal("same order replay resent provider write", a.creates.Load(), b.creates.Load())
	}
	before := digest()
	if _, e := pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info,alipay,channel}','"0"') WHERE id=(SELECT base_group_id FROM merchants WHERE id=1)`); e != nil {
		t.Fatal(e)
	}
	_, out := call(t, s.Handler(), "POST", "/mapi.php", signed(compat.Params{"pid": "1", "out_trade_no": "sequence169-http-1", "name": "synthetic169", "money": "1.00", "type": "alipay", "method": "native"}, "contract-secret"))
	if out["code"] != float64(1) || digest() != before || a.creates.Load() != 1 {
		t.Fatal("changed group repriced/rerouted original order", out)
	}
}
