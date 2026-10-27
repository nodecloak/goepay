package payment

import adapter "goepay/internal/adapters/payment"

// InstallParentAccountFactories is trusted process wiring. Every cloned flow
// keeps the root policy by reference instead of copying an affirmative flag.
func (g *ProviderFlow) InstallParentAccountFactories(factories map[int64]adapter.ParentAccountFactory) error {
	if g == nil {
		return ErrInvalid
	}
	copy := make(map[int64]adapter.ParentAccountFactory, len(factories))
	for id, factory := range factories {
		if id <= 0 || factory == nil || factory.OriginID() != id || g.Providers[id] == nil && !parentRestoreOnlyFactory(factory) || g.Providers[id] != nil && g.Providers[id].ID() != factory.PayType() {
			return ErrInvalid
		}
		copy[id] = factory
	}
	g.ParentFactories = copy
	g.parentLive = func() bool { return g.AllowLive }
	if g.parentOperator == nil {
		g.parentOperator = func(mid, source int64) bool { return parentHasChannel(g.MerchantChannels[mid], source) }
	}
	return nil
}
func parentRestoreOnlyFactory(f adapter.ParentAccountFactory) bool {
	marker, ok := f.(interface{ ParentRestoreOnly() bool })
	return ok && marker.ParentRestoreOnly()
}

// InstallParentOperatorLookup is trusted process wiring, not a request field.
func (g *ProviderFlow) InstallParentOperatorLookup(current func(int64, int64) bool) error {
	if g == nil || current == nil {
		return ErrInvalid
	}
	g.parentOperator = current
	return nil
}
