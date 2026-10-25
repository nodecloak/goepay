package payment

import "context"

// A threshold plan cannot choose a different admitted wallet channel. Offer
// exactly the product fixed by its original generation scope in the console.
func (g ProviderFlow) SettlementAutoWalletChannelsForPlan(ctx context.Context, plan int64, actor SettlementPlanActor) ([]SettlementAutoWalletChannel, error) {
	if g.Store == nil || plan <= 0 || !settlementPlanAdminValid(actor) {
		return nil, ErrInvalid
	}
	p, e := g.Store.readSettlementPlan(ctx, g.Store.pool, plan)
	if e != nil {
		return nil, e
	}
	items := g.SettlementAutoWalletChannels()
	if p.Input.Mode != settlementThresholdMode {
		return items, nil
	}
	filtered := []SettlementAutoWalletChannel{}
	if !p.View.Enabled || p.View.LastAuthorizationID <= 0 {
		return filtered, nil
	}
	a, e := g.Store.readSettlementPlanAuthorization(ctx, g.Store.pool, p.View.LastAuthorizationID)
	if e != nil {
		return nil, e
	}
	if a.Scope.Threshold == nil || a.View.PlanRevision != p.View.Revision {
		return nil, ErrConflict
	}
	for _, item := range items {
		if item.ID == a.Scope.Threshold.ChannelID && item.Provider == "alipay" {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}
