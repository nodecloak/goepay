package cryptopay

import "testing"

func TestAtomicAmountsAreExactAndBounded(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want int64
	}{{"1", 1000000}, {"0.000001", 1}, {"12.345678", 12345678}} {
		n, e := ParseAmount(c.raw)
		if e != nil || n != c.want {
			t.Fatalf("%s: %d %v", c.raw, n, e)
		}
		if FormatAmount(n) != c.raw && c.raw != "1" {
			t.Fatal(FormatAmount(n))
		}
	}
	for _, raw := range []string{"-1", "+1", "1e6", "0", "1.0000001", "NaN", "9223372036854.775808", " 1"} {
		if _, e := ParseAmount(raw); e == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
