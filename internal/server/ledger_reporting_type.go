package server

// 资金明细 original record types (原 pre_record.type).
//
// GOEPAY's immutable ledger records event_key + description, not the original
// localized type vocabulary. This projects a stable type CODE from the journal
// event_key prefix (and, where one prefix carries two directions, the
// description and the credited/debited account) so the console can show the
// original Chinese names and filter by them, exactly as the PHP 资金明细 did.
//
// The codes are an internal enum shared by the projected column and the list
// filter; the Chinese labels live in web/src/pro-console-fields.ts ledgerTerm().
// Keep this in sync with the changeUserMoney callers in epay_release:
//   订单收入 在线收款 订单服务费 订单冻结/解冻 订单退款 服务费退款 余额充值
//   代付 代付退回 自动结算 结算失败退回 订单分账 站内转账 购买会员 邀请购买会员
//   邀请返现 充值保证金 提取保证金 手动提现 后台加款 后台扣款 实名认证
//
// Anything unmatched falls through to 'other' so the row still lists (it then
// shows the localized description ledgerTerm already resolves).

// ledgerTypeExpression returns a SQL scalar producing the type code for a row.
// j/e/a are the journals/ledger_entries/accounts aliases of the ledger query.
const ledgerTypeExpression = `CASE
 WHEN j.event_key LIKE 'payment:%' AND a.kind='available' THEN 'order_income'
 WHEN j.event_key LIKE 'payment-fee:%' THEN 'order_fee'
 WHEN j.event_key LIKE 'order-income:%:freeze' THEN 'order_freeze'
 WHEN j.event_key LIKE 'order-income:%:unfreeze' THEN 'order_unfreeze'
 WHEN j.event_key LIKE 'refund:%' AND a.kind='available' THEN 'order_refund'
 WHEN j.event_key LIKE 'refund-fee:%' THEN 'refund_fee'
 WHEN j.event_key LIKE 'transfer:%' AND j.description LIKE 'transfer failed%' THEN 'transfer_return'
 WHEN j.event_key LIKE 'transfer:%:bank-return%' THEN 'transfer_return'
 WHEN j.event_key LIKE 'transfer:%' AND a.kind='available' AND e.amount>0 THEN 'transfer_return'
 WHEN j.event_key LIKE 'transfer:%' THEN 'transfer'
 WHEN j.event_key LIKE 'request:%' AND a.kind='settlement_reserve' THEN 'settle_auto'
 WHEN j.event_key LIKE 'request:%' AND a.kind='channel_sharing_reserve' THEN 'profit_sharing'
 WHEN j.event_key LIKE 'request:%:reserve' AND a.kind='reserved' THEN 'internal_transfer'
 WHEN j.event_key LIKE 'request:%:review' THEN 'internal_transfer'
 WHEN j.event_key LIKE 'deposit:%' AND j.description LIKE 'deposit hold%' THEN 'deposit_in'
 WHEN j.event_key LIKE 'deposit:%' AND j.description LIKE 'deposit release%' THEN 'deposit_out'
 WHEN j.event_key LIKE 'membership:%' THEN 'membership_buy'
 WHEN j.event_key LIKE 'membership-referral:%' THEN 'invite_reward'
 WHEN j.event_key LIKE 'certification233:%' THEN 'certification'
 WHEN j.description LIKE 'original payment fee referral reward%' THEN 'invite_reward'
 WHEN j.event_key LIKE 'admin-adjust:%' AND e.amount>0 THEN 'admin_credit'
 WHEN j.event_key LIKE 'admin-adjust:%' THEN 'admin_debit'
 ELSE 'other'
END`

// The settlement reserve and internal-transfer/profitsharing reserve both use
// 'request:%:reserve'; the reserve account kind separates them. Settlement
// reserves into settlement_reserve, channel sharing into channel_sharing_reserve,
// and a station transfer into the plain reserved account.

// ledgerTypeCodes is the set a 资金明细 type filter accepts. Each value is a SQL
// predicate over the j/e/a aliases, mirroring one branch of ledgerTypeExpression.
var ledgerTypeCodes = map[string]string{
	"order_income":      "j.event_key LIKE 'payment:%' AND a.kind='available'",
	"order_fee":         "j.event_key LIKE 'payment-fee:%'",
	"order_freeze":      "j.event_key LIKE 'order-income:%:freeze'",
	"order_unfreeze":    "j.event_key LIKE 'order-income:%:unfreeze'",
	"order_refund":      "j.event_key LIKE 'refund:%' AND a.kind='available'",
	"refund_fee":        "j.event_key LIKE 'refund-fee:%'",
	"transfer":          "j.event_key LIKE 'transfer:%' AND j.description NOT LIKE 'transfer failed%' AND NOT (j.event_key LIKE 'transfer:%:bank-return%') AND NOT (a.kind='available' AND e.amount>0)",
	"transfer_return":   "(j.event_key LIKE 'transfer:%' AND (j.description LIKE 'transfer failed%' OR j.event_key LIKE 'transfer:%:bank-return%' OR (a.kind='available' AND e.amount>0)))",
	"settle_auto":       "j.event_key LIKE 'request:%' AND a.kind='settlement_reserve'",
	"profit_sharing":    "j.event_key LIKE 'request:%' AND a.kind='channel_sharing_reserve'",
	"internal_transfer": "((j.event_key LIKE 'request:%:reserve' AND a.kind='reserved') OR j.event_key LIKE 'request:%:review')",
	"deposit_in":        "j.event_key LIKE 'deposit:%' AND j.description LIKE 'deposit hold%'",
	"deposit_out":       "j.event_key LIKE 'deposit:%' AND j.description LIKE 'deposit release%'",
	"membership_buy":    "j.event_key LIKE 'membership:%'",
	"invite_reward":     "(j.event_key LIKE 'membership-referral:%' OR j.description LIKE 'original payment fee referral reward%')",
	"certification":     "j.event_key LIKE 'certification233:%'",
	"admin_credit":      "j.event_key LIKE 'admin-adjust:%' AND e.amount>0",
	"admin_debit":       "j.event_key LIKE 'admin-adjust:%' AND e.amount<=0",
}
