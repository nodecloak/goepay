package backoffice

import (
	"context"
	"strconv"
	"strings"
)

// Original admin/set.php 商户相关 保证金 (deposit) settings. epay_pro exposes
// three keys on the 商户相关 tab (index-C6la0DtP.pretty.js:2711-2745):
//
//	user_deposit      "0"/"1"  是否开启保证金 (feature switch)
//	user_deposit_min  yuan     平台要求的最低保证金 (below it, API/pay is refused)
//	user_deposit_day  days     提取保证金要求最近 N 天内无订单且无投诉
//
// The merchant 保证金 page reads deposit_min to warn + pre-fill the shortfall
// and deposit_day to show the withdrawal condition; the withdrawal endpoint
// enforces deposit_day server-side (see payment.Deposit). user_deposit gates
// the merchant deposit page/endpoint — but it DEFAULTS ON so existing
// deployments that never set the key keep today's behaviour (the page stays
// available); only an explicit "0" turns it off.
func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options,
				Option{Label: "开启商户保证金", Value: "user_deposit"},
				Option{Label: "最低保证金金额（元）", Value: "user_deposit_min"},
				Option{Label: "提取保证金无订单无投诉天数", Value: "user_deposit_day"},
			)
		}
	}
	definitions["settings"] = d
}

// validateDepositSetting reports whether key is one of the three 保证金 settings
// and, if so, whether value is legal: user_deposit is a 0/1 switch;
// user_deposit_min is a non-negative yuan amount (at most two decimals, as
// settingStorage(money) stores it); user_deposit_day is a non-negative integer
// number of days. An empty value is accepted for the two numeric keys and read
// as the disabled default (0).
func validateDepositSetting(key, value string) (bool, error) {
	switch key {
	case "user_deposit":
		if value != "0" && value != "1" {
			return true, ErrInvalid
		}
		return true, nil
	case "user_deposit_day":
		if value == "" {
			return true, nil
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 0 || n > 3660 || strconv.FormatInt(n, 10) != value {
			return true, ErrInvalid
		}
		return true, nil
	case "user_deposit_min":
		if value == "" {
			return true, nil
		}
		// yuan with at most two decimals, non-negative, matching settingStorage(money).
		if !strings.ContainsAny(value, "0123456789") {
			return true, ErrInvalid
		}
		dot := strings.IndexByte(value, '.')
		whole, frac := value, ""
		if dot >= 0 {
			whole, frac = value[:dot], value[dot+1:]
		}
		if whole == "" || len(frac) > 2 {
			return true, ErrInvalid
		}
		for _, part := range []string{whole, frac} {
			for _, r := range part {
				if r < '0' || r > '9' {
					return true, ErrInvalid
				}
			}
		}
		return true, nil
	}
	return false, nil
}

// MerchantDepositSettings is the resolved 保证金 configuration the merchant
// deposit page and the withdrawal gate read together. Enabled follows the
// default-ON rule (only an explicit "0" disables it). MinCents is the platform
// minimum in integer minor units; Days is the no-order/no-complaint window.
type MerchantDepositSettings struct {
	Enabled  bool
	MinCents int64
	Days     int
}

// DepositSettings reads the three 保证金 settings and resolves them. A missing
// user_deposit row means enabled (default ON). Malformed numeric values are
// treated as 0 (disabled requirement), matching the original where an unset
// $conf value leaves the threshold off.
func (s *Service) DepositSettings(ctx context.Context) (MerchantDepositSettings, error) {
	values, err := s.SettingValues(ctx, "user_deposit", "user_deposit_min", "user_deposit_day")
	if err != nil {
		return MerchantDepositSettings{}, err
	}
	out := MerchantDepositSettings{Enabled: values["user_deposit"] != "0"}
	out.MinCents = yuanToCents(values["user_deposit_min"])
	if n, e := strconv.Atoi(values["user_deposit_day"]); e == nil && n > 0 {
		out.Days = n
	}
	return out, nil
}

// yuanToCents converts a stored yuan string (at most two decimals) to integer
// minor units, returning 0 for empty or malformed input (the disabled default).
func yuanToCents(value string) int64 {
	if value == "" {
		return 0
	}
	dot := strings.IndexByte(value, '.')
	whole, frac := value, ""
	if dot >= 0 {
		whole, frac = value[:dot], value[dot+1:]
	}
	if whole == "" || len(frac) > 2 {
		return 0
	}
	for _, part := range []string{whole, frac} {
		for _, r := range part {
			if r < '0' || r > '9' {
				return 0
			}
		}
	}
	for len(frac) < 2 {
		frac += "0"
	}
	n, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
