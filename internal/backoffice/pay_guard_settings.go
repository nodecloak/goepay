package backoffice

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Original admin/set.php 支付相关 商品屏蔽 / 扫码页面 / 回调 / 联系QQ / 强制收银台
// settings:
//
//	blockname               商品屏蔽关键词（多个用|隔开）
//	blockalert              触发屏蔽时的提示内容（默认"该商品禁止出售"）
//	blackmsg                黑名单拦截提示内容（默认"系统异常无法完成付款"）
//	pageordername "0"/"1"   扫码页面隐藏真实商品名称（显示onlinepay/自定义）
//	notifyordername "0"/"1" 回调参数去除真实商品名称（name固定为product）
//	forceqq "0"/"1"         未填写联系QQ的商户禁止收款
//	pay_force_cashier_pids  强制跳转收银台的商户ID（多个用|隔开）
//
// pageordername / notifyordername / forceqq are read by the payment page and
// callback builders; see payment.PaymentDisplayPolicy and notification.URL.
func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options,
				Option{Label: "商品屏蔽关键词", Value: "blockname"},
				Option{Label: "商品屏蔽显示内容", Value: "blockalert"},
				Option{Label: "黑名单拦截提示内容", Value: "blackmsg"},
				Option{Label: "扫码页面隐藏商品名称", Value: "pageordername"},
				Option{Label: "回调参数去除商品名称", Value: "notifyordername"},
				Option{Label: "未填联系QQ禁止支付", Value: "forceqq"},
				Option{Label: "强制跳转收银台商户ID", Value: "pay_force_cashier_pids"},
			)
		}
	}
	definitions["settings"] = d
}

var payGuardPidList = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

// validatePayGuardSetting reports whether key is one of the商品屏蔽/扫码/回调/
// QQ/收银台 settings and, if so, whether value is legal. Empty is allowed for
// the text fields (feature off / use default message) exactly as the original
// treats an unset $conf value.
func validatePayGuardSetting(key, value string) (bool, error) {
	switch key {
	case "pageordername", "notifyordername", "forceqq":
		if value != "0" && value != "1" {
			return true, ErrInvalid
		}
		return true, nil
	case "blockname", "blockalert", "blackmsg":
		if !utf8.ValidString(value) || len(value) > 1000 || strings.ContainsAny(value, "\x00\r\n") {
			return true, ErrInvalid
		}
		return true, nil
	case "pay_force_cashier_pids":
		if value == "" {
			return true, nil
		}
		seen := map[int64]bool{}
		for _, part := range strings.Split(value, "|") {
			if !payGuardPidList.MatchString(part) {
				return true, ErrInvalid
			}
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil || seen[id] {
				return true, ErrInvalid
			}
			seen[id] = true
		}
		return true, nil
	}
	return false, nil
}
