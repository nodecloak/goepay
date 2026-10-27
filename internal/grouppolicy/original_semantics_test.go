package grouppolicy

import "testing"

func TestOriginalChannelZeroClosesIndependentlyOfZeroRetainedRate(t *testing.T) {
	for _, raw := range []string{`{"alipay":{"channel":0,"type":"","rate":"0"}}`, `{"alipay":{"channel":"0","type":"channel","rate":""}}`, `{"wxpay":{"channel":0,"type":"roll","rate":"99"}}`} {
		info, e := ParseInfo([]byte(raw))
		if e != nil {
			t.Fatal(e)
		}
		for _, p := range info {
			if string(p.RoutingChoice().Kind()) != "disabled" {
				t.Fatal("original closed channel became an inherited route", p.RoutingChoice())
			}
		}
	}
	info, e := ParseInfo([]byte(`{"alipay":{"rate":"0"}}`))
	if e != nil {
		t.Fatal(e)
	}
	if info["alipay"].RoutingChoice().Kind() != Inherit {
		t.Fatal("legacy rate-only route changed")
	}
	if _, set := info["alipay"].RetainedBPS(); set {
		t.Fatal("zero rate no longer inherits")
	}
}

func TestOriginalRandomAndSequentialSentinelsRemainExplicitPolicies(t *testing.T) {
	for _, sample := range []struct{ raw, kind string }{{`{"alipay":{"channel":-1,"type":"channel"}}`, "random"}, {`{"wxpay":{"channel":"-4","type":"channel"}}`, "sequential"}, {`{"alipay":{"channel":"-1","type":""}}`, "random"}} {
		info, e := ParseInfo([]byte(sample.raw))
		if e != nil {
			t.Fatal("original available-channel sentinel rejected", sample.raw, e)
		}
		for _, p := range info {
			if string(p.RoutingChoice().Kind()) != sample.kind {
				t.Fatal("sentinel became a real channel", p.RoutingChoice())
			}
		}
		encoded, e := info.CanonicalJSON()
		if e != nil {
			t.Fatal(e)
		}
		again, e := ParseInfo(encoded)
		if e != nil || len(again) != len(info) {
			t.Fatal("original policy did not roundtrip", e)
		}
	}
}
