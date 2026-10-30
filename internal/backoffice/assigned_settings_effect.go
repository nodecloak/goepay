package backoffice

import (
	"strings"
	"unicode/utf8"
)

// This file turns the assigned epay_pro settings into pure decision functions,
// each matching the original PHP semantics so the console settings actually
// take effect. Readers (Service.SettingValue / the payment create and checkout
// paths) resolve the stored string value; these functions decide from it.
//
// Original references (E:\GOEPAY\epay_release):
//   includes/lib/api/Pay.php:81,103   forceqq / blockname+blockalert on create
//   includes/lib/api/Pay.php:124      blackmsg on blacklist hit
//   includes/lib/Payment.php:87,122   pageordername hides the goods name
//   admin/set.php:374,378,386,394     the settings themselves
//
// Default messages come from install/install.sql where the original ships one.

// Default alert/messages the original falls back to when the setting is blank,
// exactly as the PHP `$conf['x'] ? $conf['x'] : 'default'` expressions do.
const (
	defaultBlockAlert = "该商品禁止出售"
	defaultBlackMsg   = "系统异常无法完成付款"
)

// BlockedOrderName reports whether an order's goods name hits a listed
// blockname keyword, and the alert text to show. blockname is a '|'-separated
// keyword list; a non-empty keyword contained in name triggers the block
// (strpos semantics). The alert is blockalert, or the default when blank —
// matching Pay.php:103-108. An empty keyword segment never matches (the
// original skips `!empty($rows)`), so a trailing '|' does not block everything.
func BlockedOrderName(blockname, blockalert, name string) (blocked bool, alert string) {
	for _, keyword := range strings.Split(blockname, "|") {
		if keyword == "" {
			continue
		}
		if strings.Contains(name, keyword) {
			if blockalert != "" {
				return true, blockalert
			}
			return true, defaultBlockAlert
		}
	}
	return false, ""
}

// BlacklistMessage is the message a blacklisted payer sees: blackmsg when set,
// otherwise the original default (Pay.php:124 `sysmsg('系统异常无法完成付款')`,
// configurable via the blackmsg setting).
func BlacklistMessage(blackmsg string) string {
	if blackmsg != "" {
		return blackmsg
	}
	return defaultBlackMsg
}

// ForceQQBlocksPayment reports whether a merchant with no contact QQ must be
// refused collection. forceqq="1" turns the rule on; the merchant is blocked
// only when its QQ is empty — Pay.php:81
// `if($conf['forceqq']==1 && empty($userrow['qq']))`.
func ForceQQBlocksPayment(forceqq, merchantQQ string) bool {
	return forceqq == "1" && strings.TrimSpace(merchantQQ) == ""
}

// CheckoutDisplayName is the goods name shown on the scan/checkout page. With
// pageordername="1" the real name is hidden: a configured override name is
// shown, else the literal "onlinepay" — Payment.php:87/122
// `if($conf['pageordername']==1)$order['name']=$ordername?$ordername:'onlinepay'`.
// With the setting off the real order name is shown unchanged.
func CheckoutDisplayName(pageordername, realName, overrideName string) string {
	if pageordername != "1" {
		return realName
	}
	if strings.TrimSpace(overrideName) != "" {
		return overrideName
	}
	return "onlinepay"
}

// ForcesCashier reports whether a merchant id is in the pay_force_cashier_pids
// list ('|'-separated merchant ids), so checkout routing must send that
// merchant to the cashier page. Matching is exact on the decimal id, so "10"
// does not match "100". The setting is validated to positive, de-duplicated
// decimal ids (validatePayGuardSetting), so a plain scan is sufficient.
func ForcesCashier(payForceCashierPids string, merchantID int64) bool {
	if merchantID <= 0 || payForceCashierPids == "" {
		return false
	}
	want := itoa(merchantID)
	for _, part := range strings.Split(payForceCashierPids, "|") {
		if part == want {
			return true
		}
	}
	return false
}

// itoa avoids importing strconv for a single positive-int conversion and keeps
// the comparison exact (no leading zeroes, since the id is a positive int64).
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// settlementAccountTypes maps the per-channel settle_* switches to the account
// type each one gates, in the original's display order. A missing/blank value
// means ON (the original treats an unset $conf switch as enabled for these
// rails that pre-date the per-channel toggles); only an explicit "0" disables.
var settlementAccountTypes = []struct{ key, accountType string }{
	{"settle_alipay", "alipay"},
	{"settle_wxpay", "wxpay"},
	{"settle_qqpay", "qqpay"},
	{"settle_bank", "bank"},
}

// transferChannelTypes maps the per-channel transfer_* switches to the transfer
// type each one offers. transfer_wxpay / transfer_qqpay / transfer_bank; blank
// means ON.
var transferChannelTypes = []struct{ key, transferType string }{
	{"transfer_wxpay", "wxpay"},
	{"transfer_qqpay", "qqpay"},
	{"transfer_bank", "bank"},
}

// SettlementAccountTypeAllowed reports whether a settlement account type may be
// chosen by a merchant and used by payouts, given the stored settle_* values.
// values maps setting key -> stored string ("0" disables, anything else — including
// a missing key, represented by absence — leaves the rail enabled). An account
// type with no settle_* switch (unknown rail) is left enabled, since the
// per-channel toggles only ever restrict the four rails the original lists.
func SettlementAccountTypeAllowed(values map[string]string, accountType string) bool {
	for _, m := range settlementAccountTypes {
		if m.accountType == accountType {
			return values[m.key] != "0"
		}
	}
	return true
}

// AllowedSettlementAccountTypes returns the enabled settlement account types in
// the original's order, for building the merchant's selectable list.
func AllowedSettlementAccountTypes(values map[string]string) []string {
	out := make([]string, 0, len(settlementAccountTypes))
	for _, m := range settlementAccountTypes {
		if values[m.key] != "0" {
			out = append(out, m.accountType)
		}
	}
	return out
}

// TransferTypeOffered reports whether a transfer rail (wxpay/qqpay) is offered,
// given the stored transfer_* values. Blank/missing means offered; only "0"
// hides it. A rail with no transfer_* switch is left offered.
func TransferTypeOffered(values map[string]string, transferType string) bool {
	for _, m := range transferChannelTypes {
		if m.transferType == transferType {
			return values[m.key] != "0"
		}
	}
	return true
}

// AllowedTransferTypes returns the offered transfer types in the original's order.
func AllowedTransferTypes(values map[string]string) []string {
	out := make([]string, 0, len(transferChannelTypes))
	for _, m := range transferChannelTypes {
		if values[m.key] != "0" {
			out = append(out, m.transferType)
		}
	}
	return out
}

// payGuardTextLimit bounds the stored text settings, matching
// validatePayGuardSetting; exposed so callers can defensively validate a value
// read from storage before using it in a decision.
func payGuardTextValid(v string) bool {
	return utf8.ValidString(v) && len(v) <= 1000 && !strings.ContainsAny(v, "\x00\r\n")
}
