package grouppolicy

import "testing"

func TestGroupTransferFeeLiteralZeroAndPreciseOverrides(t *testing.T) {
	for _, tc := range []struct {
		value                string
		override, settlement bool
		rate                 int64
	}{
		{"", false, false, 0}, {"_", false, false, 0},
		{"0", true, true, 0}, {"0.00", true, false, 0},
		{"1.25", true, false, 125}, {"100.00", true, false, 10000},
	} {
		value, err := ParseTransferFeeOverride(tc.value)
		if err != nil || value.Override != tc.override || value.UseSettlement != tc.settlement || value.RateBPS != tc.rate {
			t.Fatal("wrong group fee interpretation", tc, value, err)
		}
	}
	for _, value := range []string{"-1", "100.01", "1.001", " 1", "true", "bad"} {
		if _, err := ParseTransferFeeOverride(value); err == nil {
			t.Fatal("malformed group fee accepted", value)
		}
	}
}

func TestGroupRedpackFeatureStrictInheritance(t *testing.T) {
	for _, value := range []string{"", "_", "0", "1"} {
		feature, err := ParseRedpackFeatureOverride(value)
		if err != nil || feature.Override != (value == "0" || value == "1") || feature.Enabled != (value == "1") {
			t.Fatal("wrong redpack feature interpretation", value, feature, err)
		}
	}
	for _, value := range []string{"true", "false", "2", "01", " 1"} {
		if _, err := ParseRedpackFeatureOverride(value); err == nil {
			t.Fatal("malformed group redpack feature accepted", value)
		}
	}
}
