package ledger

import "testing"

func TestAmountRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		cents int64
	}{{"0.01", 1}, {"1", 100}, {"1.2", 120}, {"92233720368547758.07", 9223372036854775807}} {
		got, err := Parse(tc.raw)
		if err != nil || got != tc.cents {
			t.Fatalf("Parse(%q)=%d,%v", tc.raw, got, err)
		}
		back, err := Parse(Format(got))
		if err != nil || back != got {
			t.Fatalf("roundtrip %d", got)
		}
	}
}
func TestRejectInvalidAmounts(t *testing.T) {
	for _, s := range []string{"", "-1", "+1", "1.001", "1e2", "NaN", " 1", "1.", ".1", "92233720368547758.08"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
