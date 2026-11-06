package paymentdefend

import "testing"

func TestEvaluateStaticMatchesOriginalCheckPayVerifyOpen(t *testing.T) {
	s := Defaults()
	s["pay_verify_check_uid"] = "1000|01001|1002"
	cases := []struct {
		mode string
		mid  int64
		want Trigger
	}{
		{"0", 1000, Trigger{Known: true}},
		{"3", 9, Trigger{Triggered: true, Known: true}},
		{"2", 1000, Trigger{Triggered: true, Known: true}},
		{"2", 1001, Trigger{Triggered: true, Known: true}}, // PHP loose in_array: "01001"==1001
		{"2", 1003, Trigger{Known: true}},
		{"1", 1003, Trigger{Known: true}}, // not in list: rules skipped
		{"1", 1000, Trigger{}},            // in list: conditional rules decide
		{"9", 1000, Trigger{}},
	}
	for _, c := range cases {
		if got := EvaluateStatic(c.mode, s, c.mid); got != c.want {
			t.Errorf("mode=%s mid=%d got %+v want %+v", c.mode, c.mid, got, c.want)
		}
	}
	empty := Defaults()
	if got := EvaluateStatic("2", empty, 1000); got != (Trigger{Known: true}) {
		t.Error("empty uid list must not trigger mode 2", got)
	}
	if got := EvaluateStatic("1", empty, 1000); got != (Trigger{}) {
		t.Error("empty uid list must leave mode 1 to its rules", got)
	}
}
