package payment

import "testing"

func TestGroupRoutingInfoRetainsExistingFeeSemanticsWithCompleteChoices(t *testing.T) {
	rates, e := ParseGroupRetainedRates([]byte(`{"alipay":{"rate":"99.50","channel":"9007199254740993","type":"channel"},"wxpay":{"rate":"0","channel":7,"type":"roll"},"sandbox":{"rate":"100"}}`))
	if e != nil || len(rates) != 2 || rates["alipay"] != 9950 || rates["sandbox"] != 10000 {
		t.Fatal("routing metadata changed original retained fees", rates, e)
	}
}
