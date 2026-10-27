package backoffice

import (
	"encoding/json"
	"testing"
)

func TestGroupRoutingRawNormalizationPreservesExactIDsWithoutMutatingCaller(t *testing.T) {
	for _, encoded := range []string{`{"alipay":{"channel":9007199254740993,"type":"channel","rate":"99.50"}}`, `"{\"alipay\":{\"channel\":9007199254740993,\"type\":\"channel\",\"rate\":\"99.50\"}}"`} {
		in := map[string]json.RawMessage{"name": json.RawMessage(`"group"`), "info": json.RawMessage(encoded)}
		normalized, e := normalizeGroupPaymentInfo(in)
		if e != nil {
			t.Fatal(e)
		}
		var got map[string]map[string]any
		if e = json.Unmarshal(normalized["info"], &got); e != nil {
			t.Fatal(e)
		}
		if got["alipay"]["channel"] != "9007199254740993" || got["alipay"]["rate"] != "99.50" {
			t.Fatalf("lost exact original values: %#v", got)
		}
		if string(in["info"]) != encoded {
			t.Fatal("mutated caller raw input")
		}
		normalized["name"] = json.RawMessage(`"new"`)
		if string(in["name"]) != `"group"` {
			t.Fatal("returned same map")
		}
	}
	for _, raw := range []string{`{"alipay":{"channel":1,"type":"channel","channel":2}}`, `{"alipay":{"channel":"1","type":"channel","private_key":"secret"}}`, `{"alipay":{"rate":"99","rate":"98"}}`, `null`} {
		if _, e := normalizeGroupPaymentInfo(map[string]json.RawMessage{"info": json.RawMessage(raw)}); e == nil {
			t.Fatal("accepted malformed group info", raw)
		}
	}
}
