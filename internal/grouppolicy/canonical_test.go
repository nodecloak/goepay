package grouppolicy

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestOriginalInfoCanonicalJSONPreservesNumbersBeforeGenericDecoding(t *testing.T) {
	for _, raw := range []string{`{"alipay":{"channel":9007199254740993,"type":"channel","rate":"99.50"}}`, `{"alipay":{"channel":"9007199254740993","type":"channel","rate":"99.50"}}`} {
		info, e := ParseInfo([]byte(raw))
		if e != nil {
			t.Fatal(e)
		}
		encoded, e := info.CanonicalJSON()
		if e != nil {
			t.Fatal(e)
		}
		var fields map[string]map[string]any
		if e = json.Unmarshal(encoded, &fields); e != nil {
			t.Fatal(e)
		}
		if fields["alipay"]["channel"] != "9007199254740993" || fields["alipay"]["type"] != "channel" || fields["alipay"]["rate"] != "99.50" {
			t.Fatal("generic JSON rounded original id or rate")
		}
		again, e := ParseInfo(encoded)
		if e != nil || !reflect.DeepEqual(info, again) {
			t.Fatal("canonical roundtrip changed policy")
		}
	}
}

func TestOriginalInfoCanonicalJSONRetainsLegacyShapeAndSentinels(t *testing.T) {
	for _, raw := range []string{`{}`, `{"alipay":{}}`, `{"alipay":{"rate":""}}`, `{"alipay":{"rate":"0.00"}}`, `{"alipay":{"channel":0,"type":"roll"}}`, `{"wxpay":{"channel":-5,"type":"channel","rate":"99"}}`} {
		info, e := ParseInfo([]byte(raw))
		if e != nil {
			t.Fatal(e)
		}
		encoded, e := info.CanonicalJSON()
		if e != nil {
			t.Fatal(e)
		}
		again, e := ParseInfo(encoded)
		if e != nil || !reflect.DeepEqual(info, again) {
			t.Fatal("legacy shape changed")
		}
		if raw == `{"alipay":{"rate":""}}` && string(encoded) != raw {
			t.Fatal("rate-only config gained routing fields")
		}
	}
	if _, e := (Info)(nil).CanonicalJSON(); e == nil {
		t.Fatal("nil policy encoded as a valid default")
	}
	if _, e := (Info{"unknown": PaymentPolicy{}}).CanonicalJSON(); e == nil {
		t.Fatal("unknown method encoded")
	}
}
