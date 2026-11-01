package server

import (
	"encoding/json"
)

// Per-menu administrator permissions (migration 000132).
//
// The original epay_pro assigns each sub-administrator a per-menu permission
// array (route/admin.php system/getFullMenuList + addAdminUser/editAdminUser).
// GOEPAY reproduces that model: each menu leaf is a permission KEY, an admin may
// carry a granted-key set (console_users.permissions jsonb), and every admin
// endpoint is mapped to the key its page uses so the SERVER — not the web menu —
// decides access. The web reads the same catalog to hide ungranted menus.
//
// Backward compatibility: a NULL permissions column (every operator that existed
// before this migration) means "full operator access", exactly the pre-migration
// behaviour. Only an explicitly assigned (non-NULL) set narrows an operator.
// Super administrators are unaffected and always allowed.

// menuPermission is one grantable page.
type menuPermission struct {
	Key   string `json:"key"`   // stable identifier = menu node name
	Title string `json:"title"` // Chinese label shown in the permission tree
	Group string `json:"group"` // parent group title for tree rendering
}

// adminMenuCatalog is the ordered list of grantable admin pages. Keys match the
// menu node names in web/src/data/menu-admin.json so the web can map a granted
// key straight onto a menu node. Pages that are structurally super-only (system
// settings, admin users, cron, cleanup, update, plugins, app store, templates,
// addons) are intentionally ABSENT: an operator can never be granted them, so
// they are never offered in the tree.
var adminMenuCatalog = []menuPermission{
	{"Dashboard", "仪表盘", "仪表盘"},
	{"OrderList", "订单管理", "交易管理"},
	{"RefundList", "退款记录", "交易管理"},
	{"BlackList", "黑名单管理", "交易管理"},
	{"DailyStat", "每日交易统计", "交易管理"},
	{"UserTradeStat", "商户交易统计", "交易管理"},
	{"ChannelStat", "通道数据统计", "交易管理"},
	{"ProvinceStat", "地区数据统计", "交易管理"},
	{"BuyerStat", "支付用户统计", "交易管理"},
	{"PsReceiver", "分账规则", "分账管理"},
	{"PsOrder", "分账记录", "分账管理"},
	{"PsInternal", "站内分账规则", "分账管理"},
	{"TransferList", "付款记录", "转账付款"},
	{"TransferStat", "付款账号统计", "转账付款"},
	{"TransferChannelStat", "通道数据统计", "转账付款"},
	{"UserTransferStat", "商户代付统计", "转账付款"},
	{"SettleList", "结算管理", "结算管理"},
	{"BatchSettle", "批量结算", "结算管理"},
	{"MerchantList", "商户列表", "商户管理"},
	{"GroupList", "用户组设置", "商户管理"},
	{"GroupBuy", "用户组购买", "商户管理"},
	{"Record", "资金明细", "商户管理"},
	{"InternalTransfer", "站内转账", "商户管理"},
	{"Domain", "授权域名", "商户管理"},
	{"InviteCode", "邀请码管理", "商户管理"},
	{"PayChannel", "支付通道", "支付接口"},
	{"PayType", "支付方式", "支付接口"},
	{"PayRoll", "支付通道轮询", "支付接口"},
	{"PayWeixin", "公众号小程序", "支付接口"},
	{"ApplyChannel", "进件渠道配置", "进件管理"},
	{"ApplyMerchant", "进件商户管理", "进件管理"},
	{"SubChannelManage", "子通道管理", "进件管理"},
	{"MchRisk", "商户管控记录", "进件管理"},
	{"ComplainList", "投诉记录", "交易投诉"},
	{"ComplainCharts", "投诉统计", "交易投诉"},
	{"ComplainTask", "同步任务", "交易投诉"},
	{"Risk", "风控记录", "其他功能"},
	{"GetToken", "获取用户标识", "其他功能"},
	{"OnecodeStyle", "聚合收款码样式", "其他功能"},
	{"Anounce", "公告配置", "系统管理"},
	{"SystemLog", "登录日志", "系统管理"},
}

// adminMenuKeys is the set of catalog keys (used to reject unknown grants).
var adminMenuKeys = func() map[string]bool {
	m := make(map[string]bool, len(adminMenuCatalog))
	for _, p := range adminMenuCatalog {
		m[p.Key] = true
	}
	return m
}()

// superOnlyMenuKeys are menu pages an operator can never be granted. They are
// recorded here (not in the catalog) so the permission tree never offers them
// and so the "unmapped routes = super only" report is explicit.
var superOnlyMenuKeys = []string{
	"SystemSettings", "SystemCron", "AdminUser", "RuntimeLog",
	"SystemClean", "SystemUpdate", "AppStore", "PluginList",
	"TemplateConfig", "AddonConfig",
}

// routeMenuKey maps an admin request path (parts after the role) to the single
// menu permission key that gates it, or "" when the route is not gated by a
// grantable menu (then the caller applies the super-only fallback). parts is the
// full split path including role at index 0, e.g.
// ["admin","resources","orders"] or ["admin","reports","daily"].
func routeMenuKey(parts []string) string {
	if len(parts) < 2 {
		return ""
	}
	family := parts[1]
	sub := ""
	if len(parts) >= 3 {
		sub = parts[2]
	}
	switch family {
	// Dedicated list/detail consoles.
	case "orders", "combined-orders", "legacy-refund-history", "preauthorization":
		return "OrderList"
	case "refunds":
		return "RefundList"
	case "transfers", "transfer-batches", "transfer-batch-imports", "redpacks", "wxtrans":
		return "TransferList"
	case "channel-sharing":
		switch sub {
		case "channels", "rules", "receivers", "rule-overview":
			return "PsReceiver"
		case "orders", "returns", "finishes", "finish-preview", "payment", "order-overview":
			return "PsOrder"
		}
		return ""
	case "profitsharing-rules":
		return "PsInternal"
	case "settlement-batches":
		return "BatchSettle"
	case "settlement-plans", "settlement-plan-runs", "settlement-plan-authorizations", "settlement-payouts":
		return "SettleList"
	case "payment-channels":
		return "PayChannel"
	case "weixin-applications":
		return "PayWeixin"
	case "payment-subchannels":
		return "SubChannelManage"
	case "ledger":
		return "Record"
	case "payment-risk":
		return "Risk"
	case "complaint-sync-tasks":
		return "ComplainTask"
	case "identity-lookups", "identity-profile-lookups":
		return "GetToken"
	case "collection-styles", "collection-style-images":
		return "OnecodeStyle"
	// Reports.
	case "reports":
		switch sub {
		case "daily":
			return "DailyStat"
		case "merchants":
			return "UserTradeStat"
		case "channels":
			return "ChannelStat"
		case "provinces":
			return "ProvinceStat"
		case "buyers":
			return "BuyerStat"
		case "transfer-accounts":
			return "TransferStat"
		case "transfer-channels":
			return "TransferChannelStat"
		case "transfer-merchants":
			return "UserTransferStat"
		case "complaints":
			return "ComplainCharts"
		}
		return ""
	// Workflows.
	case "workflows":
		switch sub {
		case "settlement-recipient":
			if len(parts) == 3 || len(parts) == 4 && parts[3] == "allowed-types" {
				return "MerchantList"
			}
		case "settlements":
			return "SettleList"
		case "internal-transfers":
			return "InternalTransfer"
		case "memberships", "deposits", "recharges":
			return "GroupBuy"
		case "profitsharing-rules":
			return "PsInternal"
		case "settlement-plans", "settlement-auto", "settlement-payouts":
			return "SettleList"
		}
		return ""
	// Generic resource CRUD: map the resource name to its hosting page.
	case "resources", "manage":
		return resourceMenuKey(sub)
	// Dashboard metrics.
	case "dashboard":
		return "Dashboard"
	// Login-log viewing lives on the operations family.
	case "operations":
		switch sub {
		case "login-log":
			return "SystemLog"
		case "audit-log":
			return "SystemLog"
		}
		return ""
	}
	return ""
}

// resourceMenuKey maps a generic backoffice resource name to its menu page. A
// resource with no operator-grantable page returns "" (super-only fallback).
func resourceMenuKey(resource string) string {
	switch resource {
	case "merchants":
		return "MerchantList"
	case "groups":
		return "GroupList"
	case "domains":
		return "Domain"
	case "invitation-codes":
		return "InviteCode"
	case "announcements":
		return "Anounce"
	case "blacklist":
		return "BlackList"
	case "collection-styles":
		return "OnecodeStyle"
	case "pay-types":
		return "PayType"
	case "pay-rolls":
		return "PayRoll"
	case "weixin-apps":
		return "PayWeixin"
	case "channels":
		return "PayChannel"
	case "onboarding-channels":
		return "ApplyChannel"
	case "onboarding-applications", "enrollment":
		return "ApplyMerchant"
	case "subchannels":
		return "SubChannelManage"
	case "merchant-risk":
		return "MchRisk"
	case "complaints":
		return "ComplainList"
	case "complaint-tasks":
		return "ComplainTask"
	case "risk-records":
		return "Risk"
	case "profitsharing-rules":
		return "PsInternal"
	}
	return ""
}

// parsePermissions decodes a stored permissions jsonb value. nil (SQL NULL)
// means "unrestricted operator"; a decoded slice (even empty) means an explicit
// grant set. Unknown keys are dropped so a renamed/removed page cannot widen
// access.
func parsePermissions(raw []byte) (granted map[string]bool, restricted bool) {
	if raw == nil {
		return nil, false
	}
	var keys []string
	if json.Unmarshal(raw, &keys) != nil {
		// A malformed value fails closed to an empty grant set, never to
		// unrestricted access.
		return map[string]bool{}, true
	}
	granted = make(map[string]bool, len(keys))
	for _, k := range keys {
		if adminMenuKeys[k] {
			granted[k] = true
		}
	}
	return granted, true
}
