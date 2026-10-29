package payment

import "testing"

// resolveRemainMoney reproduces epay_pro Settle.php remain_money handling in
// integer cents: "N%" keeps money*(1-N/100) (half-up, same as retainedByBPS);
// a fixed yuan string is subtracted; an out-of-range or empty value keeps 0.
func TestResolveRemainMoney(t *testing.T) {
	cases := []struct {
		remain   string
		eligible int64
		want     int64
		wantErr  bool
	}{
		{"", 10000, 0, false},
		{"   ", 10000, 0, false},
		{"0", 10000, 0, false},
		// Fixed yuan: 50.00 元 -> 5000 cents reserved.
		{"50.00", 10000, 5000, false},
		{"50", 10000, 5000, false},
		{"5", 400, 400, false}, // reserve clamped to eligible
		// 10%: keep 90% of 100.00 -> payable 90.00, reserve 10.00 = 1000 cents.
		{"10%", 10000, 1000, false},
		// 12.5% of 123.45: payable=round(12345*0.875)=10801.875-> half-up 10802,
		// reserve=12345-10802=1543.
		{"12.5%", 12345, 1543, false},
		// Zero eligible keeps nothing regardless of setting.
		{"10%", 0, 0, false},
		{"50.00", 0, 0, false},
		// Malformed numbers error (write-time validation also rejects these).
		{"abc", 10000, 0, true},
		{"1.234", 10000, 0, true},
	}
	for _, c := range cases {
		got, err := resolveRemainMoney(c.remain, c.eligible)
		if c.wantErr {
			if err == nil {
				t.Fatalf("resolveRemainMoney(%q,%d) expected error", c.remain, c.eligible)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("resolveRemainMoney(%q,%d)=%d,%v want %d", c.remain, c.eligible, got, err, c.want)
		}
	}
}

// retainedByBPS feeds the percentage branch; confirm the exact half-up boundary
// the original round(money*(1-rate/100),2) produced.
func TestResolveRemainMoneyPercentHalfUp(t *testing.T) {
	// 33.33% of 100.01 (10001 cents): keepRate=6667bps,
	// payable=10001/10000*6667 + (10001%10000*6667+5000)/10000
	//        = 6667 + (1*6667+5000)/10000 = 6667 + 1 = 6668; reserve=3333.
	got, err := resolveRemainMoney("33.33%", 10001)
	if err != nil || got != 3333 {
		t.Fatalf("got %d,%v want 3333", got, err)
	}
}

func TestValidRemainMoney(t *testing.T) {
	ok := []string{"", "50.00", "0", "10%", "12.5%", "99.99%"}
	bad := []string{"abc", "1.234", "100%", "0%", "-5", "101%"}
	for _, s := range ok {
		if !validRemainMoney(s) {
			t.Fatalf("validRemainMoney(%q) should be valid", s)
		}
	}
	for _, s := range bad {
		if validRemainMoney(s) {
			t.Fatalf("validRemainMoney(%q) should be invalid", s)
		}
	}
}
