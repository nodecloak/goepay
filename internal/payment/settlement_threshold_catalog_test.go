package payment

import (
	"context"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

func TestSettlementThreshold74CatalogOnlyOffersOriginalGlobalAlipayChannel(t *testing.T) {
	s := testStore(t)
	run := thresholdRun74(t, s, 7431)
	g := ProviderFlow{Store: s, MerchantChannels: map[int64][]int64{7431: {7, 8, 9}}, Providers: map[int64]adapter.Provider{
		7: &autoScopeProvider73{kind: "alipay", account: "seller:2088000000000073", product: "alipay.fund.trans.toaccount.transfer"},
		8: &autoScopeProvider73{kind: "alipay", account: "seller:2088000000000074", product: "alipay.fund.trans.toaccount.transfer"},
		9: &autoScopeProvider73{kind: "wxpay", account: "local-mch", product: "wechat.v2.promotion.transfers"},
	}}
	items, e := g.SettlementAutoWalletChannelsForPlan(context.Background(), run.PlanID, settlementPlanAdmin)
	if e != nil || len(items) != 1 || items[0].ID != 7 || items[0].Provider != "alipay" {
		t.Fatal("threshold catalog offered a different product/account/channel", items, e)
	}
}
