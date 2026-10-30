package backoffice

import "testing"

func TestSettleTransferChannelSettingValidation(t *testing.T) {
	for _, k := range []string{"settle_alipay", "settle_wxpay", "settle_qqpay", "settle_bank", "transfer_wxpay", "transfer_qqpay", "transfer_bank"} {
		for _, v := range []string{"0", "1"} {
			if known, err := validateSettleTransferChannelSetting(k, v); !known || err != nil {
				t.Error("rejected", k, v, err)
			}
		}
		for _, v := range []string{"", "2", "true", "x"} {
			if known, err := validateSettleTransferChannelSetting(k, v); !known || err == nil {
				t.Error("accepted", k, v)
			}
		}
	}
	if known, _ := validateSettleTransferChannelSetting("settle_open", "1"); known {
		t.Error("claimed an unrelated key")
	}
}

func TestPayGuardSettingValidation(t *testing.T) {
	ok := [][2]string{
		{"pageordername", "0"}, {"notifyordername", "1"}, {"forceqq", "0"},
		{"blockname", "赌博|色情"}, {"blockalert", "该商品禁止出售"}, {"blackmsg", ""},
		{"blockname", ""}, {"pay_force_cashier_pids", ""}, {"pay_force_cashier_pids", "1001"},
		{"pay_force_cashier_pids", "1001|1002|3"},
	}
	for _, c := range ok {
		if known, err := validatePayGuardSetting(c[0], c[1]); !known || err != nil {
			t.Error("rejected", c, err)
		}
	}
	bad := [][2]string{
		{"pageordername", "2"}, {"notifyordername", "true"}, {"forceqq", ""},
		{"blockname", "a\nb"}, {"pay_force_cashier_pids", "1001|1001"},
		{"pay_force_cashier_pids", "0"}, {"pay_force_cashier_pids", "1001|"},
		{"pay_force_cashier_pids", "a"}, {"pay_force_cashier_pids", "01"},
	}
	for _, c := range bad {
		if known, err := validatePayGuardSetting(c[0], c[1]); !known || err == nil {
			t.Error("accepted", c)
		}
	}
	if known, _ := validatePayGuardSetting("pay_minmoney", "1"); known {
		t.Error("claimed an unrelated key")
	}
}

func TestMerchantFeatureSettingValidation(t *testing.T) {
	for _, k := range []string{"internal_transfer", "recharge", "onecode", "complain_open", "applyments_open", "mchrisk_open", "reg_pay"} {
		for _, v := range []string{"0", "1"} {
			if known, err := validateMerchantFeatureSetting(k, v); !known || err != nil {
				t.Error("rejected", k, v, err)
			}
		}
		for _, v := range []string{"", "2", "true"} {
			if known, err := validateMerchantFeatureSetting(k, v); !known || err == nil {
				t.Error("accepted", k, v)
			}
		}
	}
	for _, v := range []string{"", "1", "100"} {
		if known, err := validateMerchantFeatureSetting("reg_pay_uid", v); !known || err != nil {
			t.Error("reg_pay_uid rejected", v, err)
		}
	}
	for _, v := range []string{"0", "-1", "1.5", "x", "01"} {
		if known, err := validateMerchantFeatureSetting("reg_pay_uid", v); !known || err == nil {
			t.Error("reg_pay_uid accepted", v)
		}
	}
	for _, v := range []string{"", "0", "10", "9.99", "100.5"} {
		if known, err := validateMerchantFeatureSetting("reg_pay_price", v); !known || err != nil {
			t.Error("reg_pay_price rejected", v, err)
		}
	}
	for _, v := range []string{"-1", "1.001", "abc", "."} {
		if known, err := validateMerchantFeatureSetting("reg_pay_price", v); !known || err == nil {
			t.Error("reg_pay_price accepted", v)
		}
	}
	if known, _ := validateMerchantFeatureSetting("transfer_wxpay", "1"); known {
		t.Error("claimed an unrelated key")
	}
}

// Every assigned key must be registered as a selectable settings key option so
// the console surfaces it exactly as epay_pro's settings page does.
func TestAssignedSettingsKeysRegistered(t *testing.T) {
	want := []string{
		"settle_alipay", "settle_wxpay", "settle_qqpay", "settle_bank",
		"transfer_wxpay", "transfer_qqpay", "transfer_bank",
		"blockname", "blockalert", "blackmsg", "pageordername", "notifyordername",
		"forceqq", "pay_force_cashier_pids",
		"internal_transfer", "recharge", "onecode",
		"complain_open", "applyments_open", "mchrisk_open",
		"reg_pay", "reg_pay_price", "reg_pay_uid",
		"user_totp_open", "subchannel_open", "user_profitsharing", "user_transfer_batch",
		"verifytype", "onecode_domains", "pay_domain_open",
	}
	options := map[string]bool{}
	for _, f := range definitions["settings"].fields {
		if f.Name == "key" {
			for _, o := range f.Options {
				options[o.Value] = true
			}
		}
	}
	for _, k := range want {
		if !options[k] {
			t.Error("settings key not registered as an option:", k)
		}
	}
}
