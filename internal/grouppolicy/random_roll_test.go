package grouppolicy

import "testing"

func TestOriginalRandomRollSentinelRetainsExactOriginalType(t *testing.T) {
	for _, raw := range []string{`{"alipay":{"channel":-3,"type":"roll","rate":"99.50"}}`, `{"alipay":{"channel":"-3","type":"roll","rate":"99.50"}}`} {
		info, e := ParseInfo([]byte(raw))
		if e != nil || string(info["alipay"].RoutingChoice().Kind()) != "random_roll" {
			t.Fatal("original random-roll branch rejected", e)
		}
		canonical, e := info.CanonicalJSON()
		if e != nil || string(canonical) != `{"alipay":{"channel":"-3","rate":"99.50","type":"roll"}}` {
			t.Fatal("original negative roll ID or retained share changed", string(canonical), e)
		}
	}
	for _, raw := range []string{`{"alipay":{"channel":-3,"type":"channel"}}`, `{"alipay":{"channel":-3,"type":""}}`, `{"sandbox":{"channel":-3,"type":"roll"}}`} {
		if _, e := ParseInfo([]byte(raw)); e == nil {
			t.Fatal("random-roll accepted another scope", raw)
		}
	}
}
