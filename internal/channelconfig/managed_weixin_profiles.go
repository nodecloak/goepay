package channelconfig

import (
	"bytes"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"sort"
)

func cloneManagedIdentitySpec(p IdentityProfileSpec) IdentityProfileSpec {
	out := p
	if p.MerchantIDs != nil {
		out.MerchantIDs = append(IdentityMerchantIDs{}, p.MerchantIDs...)
	}
	if p.Config != nil {
		out.Config = make(map[string]string, len(p.Config))
		for k, v := range p.Config {
			out.Config[k] = v
		}
	}
	if p.Certificates != nil {
		out.Certificates = make(map[string]string, len(p.Certificates))
		for k, v := range p.Certificates {
			out.Certificates[k] = v
		}
	}
	return out
}

func (f *ParentChannelFactory) ManagedIdentityOrigin() Channel { return f.Bootstrap() }
func (f *ParentChannelFactory) WithManagedWeixinProfiles(profiles []IdentityProfileSpec) (adapter.ParentAccountFactory, error) {
	return f.WithManagedIdentityProfiles(profiles)
}
func (f *ParentChannelFactory) WithoutWeixinProfileIDs(ids []int64) (adapter.ParentAccountFactory, error) {
	return f.WithoutIdentityProfileIDs(ids)
}

// Reserved IDs include disabled and other-provider/source imports. Public
// runnable catalogs intentionally omit those entries and cannot reserve them.
func (f *ParentChannelFactory) ReservedIdentityProfileIDs() []int64 {
	if f == nil {
		return nil
	}
	seen := map[int64]bool{}
	out := []int64{}
	for _, p := range f.options.IdentityProfiles {
		if p.ID > 0 && !seen[p.ID] {
			seen[p.ID] = true
			out = append(out, p.ID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// WithManagedIdentityProfiles returns a complete private copy. It preserves the
// original official transports and write/OAuth gates; no shared registry is
// modified and imported identities can never be replaced by a managed record.
func (f *ParentChannelFactory) WithManagedIdentityProfiles(managed []IdentityProfileSpec) (*ParentChannelFactory, error) {
	if f == nil {
		return nil, ErrImport
	}
	options := f.options
	options.managedProfiles = true
	options.IdentityProfiles = make([]IdentityProfileSpec, 0, len(f.options.IdentityProfiles)+len(managed))
	reserved := map[int64]bool{}
	for _, p := range f.options.IdentityProfiles {
		copy := cloneManagedIdentitySpec(p)
		options.IdentityProfiles = append(options.IdentityProfiles, copy)
		reserved[p.ID] = true
	}
	for _, p := range managed {
		if p.ID <= 0 || reserved[p.ID] || p.Provider != "wxpay" || p.ChannelID != f.origin.ID {
			return nil, ErrImport
		}
		copy := cloneManagedIdentitySpec(p)
		options.IdentityProfiles = append(options.IdentityProfiles, copy)
		reserved[p.ID] = true
	}
	next, e := parentFactoryOrigin(cloneParentChannel(f.origin), options)
	if e != nil {
		return nil, e
	}
	next.restoreOnly = f.restoreOnly
	return next, nil
}

// A quarantined/colliding current ID cannot silently fall back to an imported
// entry. Sealed original SDK restoration remains available for reconciliation.
func (f *ParentChannelFactory) WithoutIdentityProfileIDs(ids []int64) (*ParentChannelFactory, error) {
	next, e := f.WithManagedIdentityProfiles(nil)
	if e != nil {
		return nil, e
	}
	for _, id := range ids {
		delete(next.profileCatalog, id)
	}
	return next, nil
}

func (f *ParentChannelFactory) ManagedIdentityProfiles(profiles []IdentityProfileSpec, enabled bool) (map[int64]IdentityProfile, error) {
	if f == nil || !f.options.managedProfiles {
		return nil, ErrImport
	}
	for _, p := range profiles {
		current, ok := f.profileCatalog[p.ID]
		a, _ := json.Marshal(current)
		b, _ := json.Marshal(p)
		if !ok || !bytes.Equal(a, b) {
			return nil, ErrImport
		}
	}
	return buildIdentityProfiles(Import{Version: 1, Channels: []Channel{f.origin}, IdentityProfiles: profiles}, enabled, true)
}
