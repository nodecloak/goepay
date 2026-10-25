package payment

import (
	adapter "goepay/internal/adapters/payment"
	"testing"
	"time"
)

func TestSettlementAuto73FiniteAuthorityAndExplicitCostBoundary(t *testing.T) {
	at := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	good := SettlementAutoInput{GenerationAuthorizationID: 1, ValidFrom: at, ExpiresAt: at.Add(time.Hour), MaxItemGross: 100, MaxRunGross: 200, MaxDayGross: 300, MaxDayItems: 3, CostPolicy: "provider_managed_excluded", Channels: []SettlementAutoChannel{{MerchantID: 1, ChannelID: 7}}}
	if e := validateSettlementAutoInput(good); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*SettlementAutoInput){
		func(v *SettlementAutoInput) { v.GenerationAuthorizationID = 0 }, func(v *SettlementAutoInput) { v.ExpectedAuthorizationID = -1 },
		func(v *SettlementAutoInput) { v.CostPolicy = "free" }, func(v *SettlementAutoInput) { v.CostPolicy = "" },
		func(v *SettlementAutoInput) { v.ExpiresAt = at.Add(32 * 24 * time.Hour) }, func(v *SettlementAutoInput) { v.ExpiresAt = at },
		func(v *SettlementAutoInput) { v.MaxItemGross = 0 }, func(v *SettlementAutoInput) { v.MaxRunGross = 99 }, func(v *SettlementAutoInput) { v.MaxDayGross = 199 }, func(v *SettlementAutoInput) { v.MaxDayItems = 101 },
		func(v *SettlementAutoInput) { v.Channels = nil }, func(v *SettlementAutoInput) { v.Channels = []SettlementAutoChannel{{MerchantID: 0, ChannelID: 7}} },
		func(v *SettlementAutoInput) {
			v.Channels = []SettlementAutoChannel{{MerchantID: 1, ChannelID: 7}, {MerchantID: 1, ChannelID: 8}}
		},
	} {
		v := good
		change(&v)
		if e := validateSettlementAutoInput(v); e == nil {
			t.Fatal("unsafe auto authority accepted", v)
		}
	}
}

type autoScopeProvider73 struct {
	batchTestProvider
	kind, account, product string
}

func (p *autoScopeProvider73) ID() string                      { return p.kind }
func (p *autoScopeProvider73) TransferAccountIdentity() string { return p.account }
func (p *autoScopeProvider73) TransferProductIdentity() string { return p.product }
func (p *autoScopeProvider73) TransferTypes() []string         { return []string{p.kind} }

func TestSettlementAuto73RequiresConcreteProductAndStrongPayerScope(t *testing.T) {
	for _, p := range []*autoScopeProvider73{{kind: "wxpay", account: "synthetic-mch", product: "wechat.v2.promotion.transfers"}, {kind: "alipay", account: "seller:2088000000000073", product: "alipay.fund.trans.toaccount.transfer"}} {
		scope, e := settlementAutoProviderScope(p)
		if e != nil || scope.AppID != "batch-test-app" || scope.Product != p.product {
			t.Fatal(scope, e)
		}
	}
	for _, p := range []adapter.Provider{nil, &batchTestProvider{}, &autoScopeProvider73{kind: "alipay", account: "batch-test-app", product: "alipay.fund.trans.toaccount.transfer"}, &autoScopeProvider73{kind: "alipay", account: "seller:", product: "alipay.fund.trans.uni.transfer"}, &autoScopeProvider73{kind: "wxpay", account: "mch", product: "bank"}} {
		if _, e := settlementAutoProviderScope(p); e == nil {
			t.Fatal("unproven auto payer/product accepted")
		}
	}
}

func TestSettlementAuto73CatalogRequiresAdmissionAndAlwaysMasksPayer(t *testing.T) {
	g := ProviderFlow{Providers: map[int64]adapter.Provider{7: &autoScopeProvider73{kind: "wxpay", account: "1234", product: "wechat.v2.promotion.transfers"}}}
	if items := g.SettlementAutoWalletChannels(); len(items) != 0 {
		t.Fatal("unadmitted wallet exposed as eligible", items)
	}
	g.MerchantChannels = map[int64][]int64{2: {7, 7}, 1: {7}, 0: {7}, 3: {8}}
	items := g.SettlementAutoWalletChannels()
	if len(items) != 1 || items[0].PayerMasked != "***" || len(items[0].MerchantIDs) != 2 || items[0].MerchantIDs[0] != "1" || items[0].MerchantIDs[1] != "2" {
		t.Fatal("catalog lost explicit admission or leaked payer", items)
	}
}
