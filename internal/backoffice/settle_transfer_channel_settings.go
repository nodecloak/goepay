package backoffice

// Original admin/set.php 结算规则 per-channel结算开关 (settle_alipay/_wxpay/_qqpay/
// _bank) and 转账付款 per-channel转账通道开关 (transfer_wxpay/_qqpay). Each is a
// plain 0/1 switch the original console shows beside the global结算/转账开关;
// GOEPAY surfaces them in the same tabs so an administrator can turn a single
// settlement or transfer rail on or off exactly as in epay_pro.
func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options,
				Option{Label: "支付宝结算开关", Value: "settle_alipay"},
				Option{Label: "微信结算开关", Value: "settle_wxpay"},
				Option{Label: "QQ钱包结算开关", Value: "settle_qqpay"},
				Option{Label: "银行卡结算开关", Value: "settle_bank"},
				Option{Label: "微信转账接口通道", Value: "transfer_wxpay"},
				Option{Label: "QQ钱包转账接口通道", Value: "transfer_qqpay"},
			)
		}
	}
	definitions["settings"] = d
}

// validateSettleTransferChannelSetting reports whether key is one of the
// per-channel结算/转账开关 and, if so, whether value is a legal 0/1 switch.
func validateSettleTransferChannelSetting(key, value string) (bool, error) {
	switch key {
	case "settle_alipay", "settle_wxpay", "settle_qqpay", "settle_bank",
		"transfer_wxpay", "transfer_qqpay":
		if value != "0" && value != "1" {
			return true, ErrInvalid
		}
		return true, nil
	}
	return false, nil
}
