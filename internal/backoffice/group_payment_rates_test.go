package backoffice

import (
	"context"
	"fmt"
	"testing"
)

func TestOriginalGroupPaymentTypeRatesSchemaAndStrictValidation(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	schema, e := s.Schema(admin, "groups")
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, f := range schema.Fields {
		if f.Name == "info" && f.Type == "json" {
			found = true
		}
	}
	if !found {
		t.Fatal("original payment type rate field missing")
	}
	for i, info := range []string{`{}`, `{"alipay":{"rate":"99.50"},"wxpay":{"rate":""}}`, `{"sandbox":{"rate":"100"}}`, `{"alipay":{"rate":"0"}}`, `{"bank":{"rate":"99"},"qqpay":{"rate":"98.5"},"douyinpay":{"rate":""}}`} {
		if _, e = s.Create(ctx, admin, "groups", input(fmt.Sprintf(`{"name":"rates-%d","info":%s}`, i, info))); e != nil {
			t.Fatal(info, e)
		}
	}
	for _, info := range []string{`[]`, `{"fakepay":{"rate":"99"}}`, `{"alipay":{"rate":"100.01"}}`, `{"alipay":{"rate":"0.001"}}`, `{"alipay":{"rate":"-1"}}`, `{"alipay":{"rate":99.5}}`, `{"alipay":{"rate":null}}`, `{"alipay":{"rate":"99","rate":"98"}}`, `{"alipay":{"rate":"99","channel":1}}`} {
		if _, e = s.Create(ctx, admin, "groups", input(fmt.Sprintf(`{"name":"invalid","info":%s}`, info))); e == nil {
			t.Fatal("unsupported or malformed rate accepted", info)
		}
	}
}
