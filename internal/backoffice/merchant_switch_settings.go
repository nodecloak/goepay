package backoffice

import (
	"strings"

	"goepay/internal/payment"
)

// Original admin/set.php 商户相关 / 转账付款 keys added in the wave-2 parity
// pass, labelled as epay_pro labels them. What reads each one:
//
//	user_totp_open      0/1  开启商户TOTP二次验证: merchant TOTP enrolment
//	                         (/security/totp prepare and confirm). Turning it
//	                         off never blocks disabling an existing binding.
//	subchannel_open     0/1  开启子通道管理功能: the merchant 支付通道 page and
//	                         its /payment-subchannels endpoint.
//	user_profitsharing  0/1  开启添加分账规则(进件): merchant writes that add a
//	                         分账规则 or 分账接收方 on /channel-sharing.
//	user_transfer_batch 0/1  用户中心批量代付功能: the merchant 批量付款 page and
//	                         its /transfer-batches endpoint.
//	pay_domain_open     0/1  开启授权支付域名添加: a merchant adding 授权域名.
//	verifytype          0/1  用户验证方式 (0 邮箱验证, 1 手机验证): the contact
//	                         a new merchant must verify to register.
//	onecode_domains     text 聚合收款码可选域名: one origin per line, offered on
//	                         聚合收款 beside the platform address.
//
// epay_pro's 未授权支付域名禁止支付 (pay_domain_forbid) is GOEPAY's existing
// enforce_domains setting, so it is not registered twice. The feature switches
// default ON: a missing row keeps today's behaviour and only an explicit "0"
// closes the feature. verifytype and onecode_domains change nothing until an
// administrator sets them.
func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options,
				Option{Label: "开启商户TOTP二次验证", Value: "user_totp_open"},
				Option{Label: "开启子通道管理功能", Value: "subchannel_open"},
				Option{Label: "开启添加分账规则(进件)", Value: "user_profitsharing"},
				Option{Label: "用户中心批量代付功能", Value: "user_transfer_batch"},
				Option{Label: "用户验证方式", Value: "verifytype"},
				Option{Label: "聚合收款码可选域名", Value: "onecode_domains"},
				Option{Label: "开启授权支付域名添加", Value: "pay_domain_open"},
			)
		}
	}
	definitions["settings"] = d
}

// MerchantSwitchDefaultOnKeys are the wave-2 merchant feature switches that
// default ON: a missing row means available, an explicit "0" closes the
// feature. They share the MerchantFeatureVisible gate and ride the session's
// merchant_features payload.
var MerchantSwitchDefaultOnKeys = []string{"user_totp_open", "subchannel_open", "user_profitsharing", "user_transfer_batch", "pay_domain_open"}

// onecodeDomainLimit caps how many extra 聚合收款 origins may be offered.
const onecodeDomainLimit = 20

// OnecodeDomainOrigins parses onecode_domains: one origin per line, blank lines
// skipped, each accepted by payment.CollectionOrigin (https, or http on
// loopback only; no path, query or credentials). Duplicates collapse to the
// first. At most 20 origins and 2KB in all.
func OnecodeDomainOrigins(raw string) ([]string, error) {
	if len(raw) > 2048 {
		return nil, ErrInvalid
	}
	out := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		origin, e := payment.CollectionOrigin(line)
		if e != nil {
			return nil, ErrInvalid
		}
		if !seen[origin] {
			seen[origin] = true
			out = append(out, origin)
		}
	}
	if len(out) > onecodeDomainLimit {
		return nil, ErrInvalid
	}
	return out, nil
}

// validateMerchantSwitchSetting reports whether key is one of these keys and,
// if so, whether value is legal: the switches and verifytype are 0/1;
// onecode_domains must parse with OnecodeDomainOrigins.
func validateMerchantSwitchSetting(key, value string) (bool, error) {
	switch key {
	case "user_totp_open", "subchannel_open", "user_profitsharing", "user_transfer_batch", "pay_domain_open", "verifytype":
		if value != "0" && value != "1" {
			return true, ErrInvalid
		}
		return true, nil
	case "onecode_domains":
		if _, e := OnecodeDomainOrigins(value); e != nil {
			return true, ErrInvalid
		}
		return true, nil
	}
	return false, nil
}
