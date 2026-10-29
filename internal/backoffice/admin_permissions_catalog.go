package backoffice

import "sort"

// adminMenuPermissionKeys is the set of grantable per-menu permission keys for a
// sub-administrator (migration 000132). It mirrors the server package catalog
// (internal/server/admin_menu_permissions.go) — the server is the enforcement
// authority; this copy only validates the editor's input so an invalid or
// super-only key is never stored. Keeping the list here avoids a server↔
// backoffice import cycle; a server test asserts the two stay in sync.
var adminMenuPermissionKeys = map[string]bool{
	"Dashboard": true, "OrderList": true, "RefundList": true, "BlackList": true,
	"DailyStat": true, "UserTradeStat": true, "ChannelStat": true,
	"ProvinceStat": true, "BuyerStat": true, "PsReceiver": true, "PsOrder": true,
	"PsInternal": true, "TransferList": true, "TransferStat": true,
	"TransferChannelStat": true, "UserTransferStat": true, "SettleList": true,
	"BatchSettle": true, "MerchantList": true, "GroupList": true, "GroupBuy": true,
	"Record": true, "InternalTransfer": true, "Domain": true, "InviteCode": true,
	"PayChannel": true, "PayType": true, "PayRoll": true, "PayWeixin": true,
	"ApplyChannel": true, "ApplyMerchant": true, "SubChannelManage": true,
	"MchRisk": true, "ComplainList": true, "ComplainCharts": true,
	"ComplainTask": true, "Risk": true, "GetToken": true, "OnecodeStyle": true,
	"Anounce": true, "SystemLog": true,
}

// normalizeAdminPermissions validates and canonicalises a grant set: every key
// must be in the catalog, duplicates are removed, and the result is sorted for
// a stable stored value. Returns false if any key is unknown.
func normalizeAdminPermissions(keys []string) ([]string, bool) {
	seen := map[string]bool{}
	out := []string{}
	for _, k := range keys {
		if !adminMenuPermissionKeys[k] {
			return nil, false
		}
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, true
}

// AdminPermissionKeySet returns a copy of the grantable per-menu permission key
// set for cross-package sync assertions (the server catalog must match).
func AdminPermissionKeySet() map[string]bool {
	out := make(map[string]bool, len(adminMenuPermissionKeys))
	for k := range adminMenuPermissionKeys {
		out[k] = true
	}
	return out
}
