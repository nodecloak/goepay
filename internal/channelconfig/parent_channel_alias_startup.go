package channelconfig

import (
	"bytes"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"io"
)

// ParentStartupConfiguration separates registered permission origins from
// executable bootstrap accounts. An expired source stays a permission origin
// for its authenticated aliases; it never receives an alias's credentials.
type ParentStartupConfiguration struct {
	Import         Import
	IdentityImport Import
	origins        []Channel
	restoreOnly    map[int64]bool
	managed        bool
}

func (p ParentStartupConfiguration) HasRegisteredSources() bool { return len(p.origins) > 0 }
func (p ParentStartupConfiguration) HasManagedAccounts() bool   { return p.managed }
func (p ParentStartupConfiguration) BuildProviders(o BuildOptions) (map[int64]adapter.Provider, error) {
	return BuildWithOptions(p.Import, o)
}

// Registered closed templates remain available to restore independently sealed
// children even when the original bootstrap certificate has expired. Only
// Store-authenticated current aliases can derive a new executable template.
func (p ParentStartupConfiguration) BuildSubchannelFactories(o BuildOptions) (map[int64]adapter.SubchannelFactory, error) {
	imp := p.Import
	imp.Channels = append([]Channel(nil), p.origins...)
	return buildPaymentSubchannelFactories(imp, o, p.restoreOnly)
}
func (p ParentStartupConfiguration) MerchantAllowlist(raw string) (map[int64][]int64, error) {
	ids := map[int64]bool{}
	for _, c := range p.origins {
		ids[c.ID] = true
	}
	return merchantAllowlistRegistered(raw, func(id int64) bool { return ids[id] })
}
func (p ParentStartupConfiguration) BuildFactories(o ParentBuildOptions) (map[int64]adapter.ParentAccountFactory, error) {
	if len(o.IdentityProfiles) == 0 {
		o.IdentityProfiles = p.IdentityImport.IdentityProfiles
	}
	out := map[int64]adapter.ParentAccountFactory{}
	for _, c := range p.origins {
		var f *ParentChannelFactory
		var e error
		if p.restoreOnly[c.ID] {
			f, e = parentFactoryOrigin(c, o)
			if f != nil {
				f.restoreOnly = true
			}
		} else {
			f, e = NewParentChannelFactory(c, o)
		}
		if e != nil {
			return nil, e
		}
		out[c.ID] = f
	}
	return out, nil
}
func PrepareParentStartup(imp Import, records []adapter.ParentStartupAccount) (ParentStartupConfiguration, error) {
	var out ParentStartupConfiguration
	if imp.Version != 1 {
		return out, ErrImport
	}
	registered := map[int64]Channel{}
	for _, c := range imp.Channels {
		if c.ID <= 0 || registered[c.ID].ID != 0 {
			return out, ErrImport
		}
		registered[c.ID] = c
	}
	self := map[int64][]byte{}
	aliases := map[int64][]Channel{}
	seen := map[int64]bool{}
	for _, record := range records {
		origin, ok := registered[record.SourceID]
		if !ok || record.ChannelID <= 0 || seen[record.ChannelID] || record.PayType != origin.Plugin || len(record.Snapshot) == 0 || len(record.Snapshot) > 1048576 {
			return out, ErrImport
		}
		seen[record.ChannelID] = true
		var snapshot parentAccountSnapshot
		d := json.NewDecoder(bytes.NewReader(record.Snapshot))
		d.DisallowUnknownFields()
		if d.Decode(&snapshot) != nil || snapshot.Version != 1 || snapshot.Channel.ID != record.ChannelID || snapshot.Channel.Plugin != record.PayType {
			return out, ErrImport
		}
		if _, e := d.Token(); e != io.EOF {
			return out, ErrImport
		}
		// Validate real closed constructors even for aliases that are currently
		// disabled or archived. Their old histories still need exact restore.
		probe, e := parentFactoryOrigin(origin, ParentBuildOptions{IdentityProfiles: imp.IdentityProfiles})
		if e != nil {
			return out, e
		}
		if _, e = probe.Restore(record.ChannelID, record.Snapshot); e != nil {
			return out, e
		}
		if record.ChannelID == record.SourceID {
			self[record.SourceID] = record.Snapshot
		} else {
			aliases[record.SourceID] = append(aliases[record.SourceID], snapshot.Channel)
		}
	}
	resolved, e := WithParentStartupAccounts(imp, self)
	if e != nil {
		return out, e
	}
	out = ParentStartupConfiguration{Import: resolved, IdentityImport: resolved, origins: append([]Channel(nil), resolved.Channels...), restoreOnly: map[int64]bool{}, managed: len(records) > 0}
	out.Import.Channels = []Channel{}
	for _, c := range resolved.Channels {
		if _, e = NewParentChannelFactory(c, ParentBuildOptions{}); e != nil {
			if len(aliases[c.ID]) == 0 {
				return ParentStartupConfiguration{}, e
			}
			out.restoreOnly[c.ID] = true
			continue
		}
		out.Import.Channels = append(out.Import.Channels, c)
	}
	return out, nil
}
