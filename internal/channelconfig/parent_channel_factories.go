package channelconfig

import adapter "goepay/internal/adapters/payment"

// The caller has already authenticated the explicit encrypted import. This
// closed registry performs only local constructor validation, never discovery.
func BuildParentChannelFactories(imp Import, options ParentBuildOptions) (map[int64]adapter.ParentAccountFactory, error) {
	if imp.Version != 1 {
		return nil, ErrImport
	}
	out := map[int64]adapter.ParentAccountFactory{}
	if len(options.IdentityProfiles) == 0 {
		options.IdentityProfiles = imp.IdentityProfiles
	}
	for _, channel := range imp.Channels {
		if channel.ID <= 0 || out[channel.ID] != nil {
			return nil, ErrImport
		}
		factory, e := NewParentChannelFactory(channel, options)
		if e != nil {
			return nil, e
		}
		out[channel.ID] = factory
	}
	return out, nil
}
