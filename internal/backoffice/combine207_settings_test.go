package backoffice

import "testing"

func TestCombineSetting207Validation(t *testing.T) {
	for _, ok := range [][2]string{{"wxcombine_open", "0"}, {"wxcombine_open", "1"}, {"wxcombine_minmoney", ""}, {"wxcombine_minmoney", "0"}, {"wxcombine_minmoney", "100.00"}, {"wxcombine_submoney", "0.29"}} {
		if known, err := validateCombineSetting207(ok[0], ok[1]); !known || err != nil {
			t.Error("rejected", ok, err)
		}
	}
	for _, bad := range [][2]string{{"wxcombine_open", "true"}, {"wxcombine_open", "2"}, {"wxcombine_minmoney", "-1"}, {"wxcombine_minmoney", "1.001"}, {"wxcombine_submoney", "abc"}} {
		if known, err := validateCombineSetting207(bad[0], bad[1]); !known || err == nil {
			t.Error("accepted", bad)
		}
	}
	if known, _ := validateCombineSetting207("pay_minmoney", "x"); known {
		t.Error("claimed an unrelated key")
	}
}
