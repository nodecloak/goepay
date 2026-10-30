package backoffice

import "testing"

func TestDepositSettingValidation(t *testing.T) {
	for _, v := range []string{"0", "1"} {
		if known, err := validateDepositSetting("user_deposit", v); !known || err != nil {
			t.Error("user_deposit rejected", v, err)
		}
	}
	for _, v := range []string{"", "2", "true", "x"} {
		if known, err := validateDepositSetting("user_deposit", v); !known || err == nil {
			t.Error("user_deposit accepted", v)
		}
	}
	for _, v := range []string{"", "0", "7", "30", "3660"} {
		if known, err := validateDepositSetting("user_deposit_day", v); !known || err != nil {
			t.Error("user_deposit_day rejected", v, err)
		}
	}
	for _, v := range []string{"-1", "3661", "1.5", "x", "01"} {
		if known, err := validateDepositSetting("user_deposit_day", v); !known || err == nil {
			t.Error("user_deposit_day accepted", v)
		}
	}
	for _, v := range []string{"", "0", "10", "99.99", "100.5"} {
		if known, err := validateDepositSetting("user_deposit_min", v); !known || err != nil {
			t.Error("user_deposit_min rejected", v, err)
		}
	}
	for _, v := range []string{"-1", "1.001", "abc", "."} {
		if known, err := validateDepositSetting("user_deposit_min", v); !known || err == nil {
			t.Error("user_deposit_min accepted", v)
		}
	}
	if known, _ := validateDepositSetting("reg_pay", "1"); known {
		t.Error("claimed an unrelated key")
	}
}

func TestYuanToCents(t *testing.T) {
	cases := map[string]int64{
		"":        0,
		"0":       0,
		"1":       100,
		"10":      1000,
		"9.99":    999,
		"100.5":   10050,
		"0.01":    1,
		"-1":      0, // malformed -> disabled default
		"1.001":   0,
		"abc":     0,
		"1234.56": 123456,
	}
	for in, want := range cases {
		if got := yuanToCents(in); got != want {
			t.Errorf("yuanToCents(%q)=%d want %d", in, got, want)
		}
	}
}

func TestDepositSettingsKeysRegistered(t *testing.T) {
	options := map[string]bool{}
	for _, f := range definitions["settings"].fields {
		if f.Name == "key" {
			for _, o := range f.Options {
				options[o.Value] = true
			}
		}
	}
	for _, k := range []string{"user_deposit", "user_deposit_min", "user_deposit_day"} {
		if !options[k] {
			t.Error("deposit settings key not registered as an option:", k)
		}
	}
}
