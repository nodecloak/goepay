package payment

import (
	"context"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"strings"
	"time"
)

type SettlementAutoChannel struct {
	MerchantID int64 `json:"merchant_id,string"`
	ChannelID  int64 `json:"channel_id,string"`
}

type SettlementAutoInput struct {
	ExpectedAuthorizationID   int64                   `json:"expected_authorization_id,string"`
	GenerationAuthorizationID int64                   `json:"generation_authorization_id,string"`
	ValidFrom                 time.Time               `json:"valid_from"`
	ExpiresAt                 time.Time               `json:"expires_at"`
	MaxItemGross              int64                   `json:"max_item_gross,string"`
	MaxRunGross               int64                   `json:"max_run_gross,string"`
	MaxDayGross               int64                   `json:"max_day_gross,string"`
	MaxDayItems               int64                   `json:"max_day_items,string"`
	CostPolicy                string                  `json:"cost_policy"`
	Channels                  []SettlementAutoChannel `json:"channels"`
}

// ValidateSettlementAutoInput validates the typed independent authority contract.
func ValidateSettlementAutoInput(v SettlementAutoInput) error { return validateSettlementAutoInput(v) }

func validateSettlementAutoInput(v SettlementAutoInput) error {
	// The shared bounds cover only settlement gross. They do not assert an
	// institution-enforced ceiling on charges to the upstream provider account.
	limits := SettlementPlanAuthorizationInput{ExpectedAuthorizationID: v.ExpectedAuthorizationID, ValidFrom: v.ValidFrom, ExpiresAt: v.ExpiresAt, MaxItemGross: v.MaxItemGross, MaxRunGross: v.MaxRunGross, MaxDayGross: v.MaxDayGross, MaxDayItems: v.MaxDayItems}
	if validateSettlementPlanAuthorization(limits) != nil || v.GenerationAuthorizationID <= 0 || v.CostPolicy != "provider_managed_excluded" || len(v.Channels) < 1 || len(v.Channels) > 100 {
		return ErrInvalid
	}
	seen := map[int64]bool{}
	for _, ch := range v.Channels {
		if ch.MerchantID <= 0 || ch.ChannelID <= 0 || seen[ch.MerchantID] {
			return ErrInvalid
		}
		seen[ch.MerchantID] = true
	}
	return nil
}

type settlementAutoChannelScope struct {
	SettlementAutoChannel
	Provider     string `json:"provider"`
	Product      string `json:"product"`
	AppID        string `json:"app_id"`
	AccountScope string `json:"account_scope"`
}

func settlementAutoSystemActor(a TransferBatchActor) bool {
	return a.Role == "system" && a.ID == 0 && a.Name == "settlement-auto"
}
func settlementAutoActorFor(ctx context.Context, a TransferBatchActor, id int64) bool {
	c, _ := ctx.Value(settlementAutoClaimKey{}).(*settlementAutoClaim)
	return settlementAutoSystemActor(a) && c != nil && c.RequestID == id && c.AuthorizationID > 0
}

func settlementAutoProviderScope(p adapter.Provider) (settlementAutoChannelScope, error) {
	var out settlementAutoChannelScope
	if p == nil {
		return out, ErrInvalid
	}
	scope, e := adapter.TransferAccountScope(p)
	if e != nil {
		return out, e
	}
	var parts []string
	if json.Unmarshal([]byte(scope), &parts) != nil || len(parts) != 4 || parts[0] != p.ID() || parts[1] == "" || parts[2] == "" || parts[3] != "account" || (p.ID() != "wxpay" && p.ID() != "alipay") || (p.ID() == "alipay" && (!strings.HasPrefix(parts[2], "seller:") || len(parts[2]) <= 7)) {
		return out, ErrInvalid
	}
	product, ok := p.(adapter.TransferProductIdentity)
	if !ok {
		return out, ErrInvalid
	}
	out.Product = product.TransferProductIdentity()
	if p.ID() == "alipay" {
		if out.Product != "alipay.fund.trans.toaccount.transfer" && out.Product != "alipay.fund.trans.uni.transfer" {
			return out, ErrInvalid
		}
	} else if out.Product != "wechat.v2.promotion.transfers" {
		return out, ErrInvalid
	}
	if _, ok = p.(adapter.TransferProvider); !ok {
		return out, ErrInvalid
	}
	caps, ok := p.(adapter.TransferCapabilities)
	if !ok {
		return out, ErrInvalid
	}
	found := false
	for _, typ := range caps.TransferTypes() {
		if typ == p.ID() {
			found = true
		}
	}
	if !found {
		return out, ErrInvalid
	}
	out.Provider, out.AppID, out.AccountScope = p.ID(), parts[1], scope
	return out, nil
}
