package channelconfig

import (
	"encoding/json"
	provider "goepay/internal/adapters/payment"
	"strings"
	"testing"
)

func TestPaymentCost71FactoryMissingExplicitZeroAndPhysicalRevision(t *testing.T) {
	build := func(rate *string, account, key string) (provider.Provider, error) {
		cfg := map[string]string{"appid": "cost71-app", "appmchid": account, "appkey": key}
		if rate != nil {
			cfg["costrate"] = *rate
		}
		out, e := Build(Import{Version: 1, Channels: []Channel{{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: cfg}}}, false)
		return out[7], e
	}
	key := "0123456789abcdef0123456789abcdef"
	p, e := build(nil, "cost71-merchant", key)
	if e != nil {
		t.Fatal(e)
	}
	if _, known := p.(provider.PaymentCostEstimator).PaymentCostEstimate(); known {
		t.Fatal("missing configuration invented zero cost")
	}
	zero, rate := "0", "0.500000"
	p, e = build(&zero, "cost71-merchant", key)
	if e != nil {
		t.Fatal(e)
	}
	z, known := p.(provider.PaymentCostEstimator).PaymentCostEstimate()
	if !known || z.RateUnits != 0 {
		t.Fatal(z, known)
	}
	p, e = build(&rate, "cost71-merchant", key)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := p.(provider.PaymentCostEstimator).PaymentCostEstimate()
	p, e = build(&rate, "cost71-merchant", "abcdef0123456789abcdef0123456789")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := p.(provider.PaymentCostEstimator).PaymentCostEstimate()
	p, e = build(&rate, "other-merchant", key)
	if e != nil {
		t.Fatal(e)
	}
	c, _ := p.(provider.PaymentCostEstimator).PaymentCostEstimate()
	if a.RateUnits != 500000 || a.Revision == "" || a.Revision != b.Revision || a.Revision == c.Revision || a.AccountScope == c.AccountScope {
		t.Fatal("estimate identity/revision", a, b, c)
	}
	raw, _ := json.Marshal(a)
	if string(raw) != "{}" || strings.Contains(string(raw), key) {
		t.Fatal("private estimate exposed", string(raw))
	}
	bad := ""
	if _, e = build(&bad, "cost71-merchant", key); e == nil {
		t.Fatal("explicit empty cost accepted")
	}
}
