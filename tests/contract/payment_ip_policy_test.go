package contract_test

import (
	"context"
	"goepay/internal/compat"
	"net/url"
	"testing"
)

func TestSignedBuyerAndTransportIPPoliciesBeforeOrderCreation(t *testing.T) {
	for _, tc := range []struct{ name, blocked, claimed string }{
		{"signed buyer", "203.0.113.8", "203.0.113.8"},
		{"signed IPv6 CIDR", "2001:db8::/32", "2001:0db8:0:0:0:0:0:8"},
		{"transport cannot be overridden", "192.0.2.1", "203.0.113.9"},
		{"malformed signed IP", "203.0.113.8", "not-an-ip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, pool, _ := fixture(t)
			if _, e := pool.Exec(context.Background(), "INSERT INTO backoffice_records(resource,body) VALUES('blacklist',jsonb_build_object('kind','ip','value',$1::text,'enabled',true))", tc.blocked); e != nil {
				t.Fatal(e)
			}
			p := compat.Params{"pid": "1", "type": "sandbox", "out_trade_no": "ip-policy", "name": "test", "money": "1.00", "clientip": tc.claimed, "sign_type": "MD5"}
			p["sign"] = compat.SignMD5(p, "contract-secret")
			v := url.Values{}
			for k, x := range p {
				v.Set(k, x)
			}
			_, out := call(t, s.Handler(), "POST", "/mapi.php", v)
			if out["code"] != float64(-1) {
				t.Fatal("restricted IP accepted", out)
			}
			var n int
			if e := pool.QueryRow(context.Background(), "SELECT count(*) FROM orders").Scan(&n); e != nil || n != 0 {
				t.Fatal("policy rejected after creating order", n, e)
			}
		})
	}
}
