package grouppolicy

import (
	"strings"
	"testing"
)

func TestOriginalInfoRetainsRatesAndExactRuntimeRoutingIDs(t *testing.T) {
	p, e := ParseInfo([]byte(`{"alipay":{"rate":"99.50","channel":"9007199254740993","type":"channel"},"wxpay":{"rate":"0","channel":7,"type":"roll"},"sandbox":{"rate":"100"}}`))
	if e != nil {
		t.Fatal(e)
	}
	if rate, yes := p["alipay"].RetainedBPS(); !yes || rate != 9950 {
		t.Fatal("original retained share lost")
	}
	if c := p["alipay"].RoutingChoice(); c.Kind() != Channel || c.ID() != 9007199254740993 {
		t.Fatal("runtime channel identifier rounded")
	}
	if _, yes := p["wxpay"].RetainedBPS(); yes {
		t.Fatal("zero must inherit")
	}
	if c := p["wxpay"].RoutingChoice(); c.Kind() != Roll || c.ID() != 7 {
		t.Fatal("roll confused with channel")
	}
	if rate, yes := p["sandbox"].RetainedBPS(); !yes || rate != 10000 {
		t.Fatal("full retained share lost")
	}
	for _, raw := range []string{`{"alipay":{"channel":9223372036854775807,"type":"channel"}}`, `{"alipay":{"channel":"9223372036854775807","type":"channel"}}`} {
		out, e := ParseInfo([]byte(raw))
		if e != nil || out["alipay"].RoutingChoice().ID() != 9223372036854775807 {
			t.Fatal("int64 runtime id not preserved")
		}
	}
}

func TestOriginalInfoZeroRateInheritanceFirstAndLegacyRateOnly(t *testing.T) {
	for _, raw := range []string{`{}`, `{"alipay":{}}`, `{"alipay":{"rate":""}}`, `{"alipay":{"rate":"0.00"}}`} {
		p, e := ParseInfo([]byte(raw))
		if e != nil {
			t.Fatal(raw, e)
		}
		if _, yes := p["alipay"].RetainedBPS(); yes {
			t.Fatal("invented retained rate")
		}
		if p["alipay"].RoutingChoice().Kind() != Inherit {
			t.Fatal("inheritance turned into a route")
		}
	}
	for _, raw := range []string{`{"alipay":{"channel":-5,"type":"channel","rate":"99"}}`, `{"wxpay":{"channel":"-5","type":"channel"}}`} {
		p, e := ParseInfo([]byte(raw))
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range p {
			if v.RoutingChoice().Kind() != First || v.RoutingChoice().ID() != 0 {
				t.Fatal("first-available sentinel became a real id")
			}
		}
	}
}

func TestOriginalInfoRejectsAmbiguousUnsupportedAndMalformedSettings(t *testing.T) {
	for _, raw := range []string{`[]`, `null`, `{} {}`, `{"fakepay":{"rate":"99"}}`, `{"unionpay":{}}`, `{"alipay":[]}`, `{"alipay":null}`, `{"alipay":{},"alipay":{}}`, `{"alipay":{"rate":"99","rate":"98"}}`, `{"alipay":{"rate":99}}`, `{"alipay":{"rate":null}}`, `{"alipay":{"rate":"100.01"}}`, `{"alipay":{"rate":"-1"}}`, `{"alipay":{"rate":"1e2"}}`, `{"alipay":{"rate":"0.001"}}`, `{"alipay":{"channel":1}}`, `{"alipay":{"type":"channel"}}`, `{"alipay":{"channel":1,"type":""}}`, `{"alipay":{"channel":1,"type":"unknown"}}`, `{"alipay":{"channel":"01","type":"channel"}}`, `{"alipay":{"channel":1.0,"type":"channel"}}`, `{"alipay":{"channel":1e0,"type":"channel"}}`, `{"alipay":{"channel":null,"type":"channel"}}`, `{"alipay":{"channel":"9223372036854775808","type":"channel"}}`, `{"alipay":{"channel":-2,"type":"roll"}}`, `{"alipay":{"channel":-3,"type":"channel"}}`, `{"alipay":{"channel":-5,"type":"roll"}}`, `{"alipay":{"channel":1,"channel":2,"type":"channel"}}`, `{"alipay":{"channel":1,"type":"channel","type":"roll"}}`, `{"alipay":{"rate":"99","extra":"unknown"}}`, `{"sandbox":{"channel":7,"type":"channel"}}`, strings.Repeat(" ", 4097)} {
		if _, e := ParseInfo([]byte(raw)); e == nil {
			t.Fatal("invalid original info accepted", raw)
		}
	}
}

// The original group info is keyed by every pre_type name, so the ported
// pay types carry rates and channel choices exactly like alipay and wxpay.
func TestOriginalInfoAcceptsTheOriginalPayTypeCatalog(t *testing.T) {
	for _, kind := range []string{"qqpay", "bank", "jdpay", "paypal", "douyinpay", "ecny"} {
		p, e := ParseInfo([]byte(`{"` + kind + `":{"channel":3,"type":"channel","rate":"98.5"}}`))
		if e != nil {
			t.Fatal(kind, e)
		}
		if bps, ok := p[kind].RetainedBPS(); !ok || bps != 9850 {
			t.Fatal(kind, "rate", bps, ok)
		}
		if p[kind].RoutingChoice().Kind() != Channel || p[kind].RoutingChoice().ID() != 3 {
			t.Fatal(kind, "channel choice")
		}
	}
}
