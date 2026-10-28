package payment

import (
	"reflect"
	"testing"
)

func TestSplitCombine207MatchesOriginalCombinepaySubmoneys(t *testing.T) {
	on := Combine207{Open: "1", MinMoney: "100", SubMoney: "50"}
	cases := []struct {
		name  string
		c     Combine207
		total int64
		want  []int64
		ok    bool
	}{
		{"off", Combine207{Open: "0", MinMoney: "1", SubMoney: "1"}, 100000, nil, false},
		{"no-minimum", Combine207{Open: "1", SubMoney: "1"}, 100000, nil, false},
		{"below-minimum", on, 9999, nil, false},
		{"at-minimum-three-even-cap", on, 10000, []int64{3334, 3333, 3333}, true},
		// 150.00 / 3 = 50.00 is not above the 50.00 cap: stays 3.
		{"cap-boundary", on, 15000, []int64{5000, 5000, 5000}, true},
		// 150.01 / 3 = 50.00 (int), still 3; remainder 1 cent to the first.
		{"remainder", on, 15001, []int64{5001, 5000, 5000}, true},
		// 200.00 / 3 = 66.66 > 50 -> 4 x 50.00.
		{"grows", on, 20000, []int64{5000, 5000, 5000, 5000}, true},
		// Exact cents: PHP intval(0.29*100)=28; here the cap is 29 cents.
		{"exact-cents", Combine207{Open: "1", MinMoney: "0.01", SubMoney: "0.29"}, 87, []int64{29, 29, 29}, true},
		{"too-small-for-three", Combine207{Open: "1", MinMoney: "0.01", SubMoney: "1"}, 2, nil, false},
	}
	for _, c := range cases {
		got, ok := SplitCombine207(c.c, c.total)
		if ok != c.ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v %v want %v %v", c.name, got, ok, c.want, c.ok)
		}
	}
	// Capped at 50 sub-orders; shares still sum to the total.
	got, ok := SplitCombine207(Combine207{Open: "1", MinMoney: "1", SubMoney: "0.01"}, 1000001)
	var sum int64
	for _, v := range got {
		sum += v
		if v < 1 {
			t.Fatal("zero-amount sub-order", got)
		}
	}
	if !ok || len(got) != 50 || sum != 1000001 {
		t.Fatalf("50-cap split wrong: n=%d sum=%d ok=%v", len(got), sum, ok)
	}
}
