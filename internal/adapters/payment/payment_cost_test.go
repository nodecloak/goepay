package payment

import "testing"

type costIdentity71 struct {
	Provider
	id, app, account string
}

func (p costIdentity71) ID() string                      { return p.id }
func (p costIdentity71) ApplicationID() string           { return p.app }
func (p costIdentity71) TransferAccountIdentity() string { return p.account }

func TestPaymentCost71RequiresStrongPhysicalAccountAndCanonicalRevision(t *testing.T) {
	for _, p := range []costIdentity71{{id: "alipay", app: "app", account: "app"}, {id: "alipay", app: "app", account: "seller:"}, {id: "wxpay", app: "app"}, {id: "wxpay", app: "", account: "mch"}, {id: "other", app: "app", account: "mch"}, {id: "wxpay", app: "app", account: "mch\n"}} {
		if _, e := NewPaymentCostEstimate(p, "0"); e == nil {
			t.Fatal("weak/unsupported identity accepted", p.id)
		}
	}
	for _, p := range []costIdentity71{{id: "wxpay", app: "app", account: "mch"}, {id: "alipay", app: "app", account: "seller:2088000000000071"}} {
		a, e := NewPaymentCostEstimate(p, "0.500000")
		if e != nil {
			t.Fatal(e)
		}
		b, e := NewPaymentCostEstimate(p, "0.5")
		if e != nil || a != b {
			t.Fatal("canonical config revision differs", a, b, e)
		}
	}
}

func TestPaymentCost71ExactConfiguredRateParsing(t *testing.T) {
	for _, tc := range []struct {
		raw, canonical string
		units          int64
	}{
		{"0", "0", 0}, {"0.000000", "0", 0}, {"0.5", "0.5", 500000}, {"0.500000", "0.5", 500000}, {"0.000001", "0.000001", 1}, {"12.345678", "12.345678", 12345678}, {"100", "100", 100000000}, {"100.000000", "100", 100000000},
	} {
		got, err := ParsePaymentCostRate(tc.raw)
		if err != nil || got.Units != tc.units || got.Canonical != tc.canonical {
			t.Fatal(tc, got, err)
		}
	}
	for _, raw := range []string{"", " ", "00", "01", ".5", "1.", "1e-3", "+1", "-1", "NaN", "Infinity", "100.000001", "1.0000001", "0.5\n", "0x1", "1,5", "１"} {
		if _, err := ParsePaymentCostRate(raw); err == nil {
			t.Fatal("invalid or unknown cost accepted", raw)
		}
	}
}
