package backoffice

import "testing"

func TestMerchantSwitchSettingValidation(t *testing.T) {
	for _, k := range []string{"user_totp_open", "subchannel_open", "user_profitsharing", "user_transfer_batch", "pay_domain_open"} {
		for _, v := range []string{"0", "1"} {
			if known, err := validateMerchantSwitchSetting(k, v); !known || err != nil {
				t.Error("rejected", k, v, err)
			}
		}
		for _, v := range []string{"", "2", "true", "x"} {
			if known, err := validateMerchantSwitchSetting(k, v); !known || err == nil {
				t.Error("accepted", k, v)
			}
		}
	}
	for _, v := range []string{"0", "1", "2"} {
		if known, err := validateMerchantSwitchSetting("verifytype", v); !known || err != nil {
			t.Error("verifytype rejected", v, err)
		}
	}
	for _, v := range []string{"", "3", "true", "01"} {
		if known, err := validateMerchantSwitchSetting("verifytype", v); !known || err == nil {
			t.Error("verifytype accepted", v)
		}
	}
	for _, k := range []string{"onecode_domains", "pay_domain_forbid"} {
		for _, v := range []string{"", "a.com", "a.com\nb.com"} {
			if known, err := validateMerchantSwitchSetting(k, v); !known || err != nil {
				t.Error("domain list rejected", k, v, err)
			}
		}
		for _, v := range []string{string(make([]byte, 2049)), "a\x00b"} {
			if known, err := validateMerchantSwitchSetting(k, v); !known || err == nil {
				t.Error("domain list accepted oversize/nul", k)
			}
		}
	}
	if known, _ := validateMerchantSwitchSetting("transfer_wxpay", "1"); known {
		t.Error("claimed an unrelated key")
	}
}

// The four parity switches must ride the default-ON MerchantFeatureVisible gate
// so an unset deployment keeps the feature available.
func TestMerchantSwitchDefaultOnKeysAreVisibleKeys(t *testing.T) {
	visible := map[string]bool{}
	for _, k := range MerchantFeatureVisibleKeys {
		visible[k] = true
	}
	for _, k := range MerchantSwitchDefaultOnKeys {
		if !visible[k] {
			t.Error("default-on switch not registered as a visible feature key:", k)
		}
	}
}
