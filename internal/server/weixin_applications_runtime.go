package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/payment"
)

type weixinApplicationSource struct {
	ID          int64    `json:"id,string"`
	Fingerprint string   `json:"fingerprint"`
	MerchantIDs []string `json:"merchant_ids"`
}

// Supplied only by trusted process configuration, like ParentAccountFactory.
// Returning the interface preserves any host-owned transport wrapper when the
// credential catalog is cloned; HTTP input can never supply this factory.
type managedWeixinApplicationFactory interface {
	adapter.ParentAccountFactory
	ParentRestoreOnly() bool
	ManagedIdentityOrigin() channelconfig.Channel
	WithManagedWeixinProfiles([]channelconfig.IdentityProfileSpec) (adapter.ParentAccountFactory, error)
	WithoutWeixinProfileIDs([]int64) (adapter.ParentAccountFactory, error)
	ManagedIdentityProfiles([]channelconfig.IdentityProfileSpec, bool) (map[int64]channelconfig.IdentityProfile, error)
}

func (s *Server) weixinApplicationRoot() *Server {
	for s.subchannelPolicySource != nil {
		s = s.subchannelPolicySource
	}
	return s
}

func (s *Server) weixinApplicationScope() (payment.WeixinApplicationScope, []weixinApplicationSource, error) {
	root := s.weixinApplicationRoot()
	scope := payment.WeixinApplicationScope{ReservedIDs: map[int64]bool{}, SourceFingerprints: map[int64]string{}}
	for _, id := range root.WeixinImportedProfileIDs {
		scope.ReservedIDs[id] = true
	}
	factories := map[int64]adapter.ParentAccountFactory{}
	if root.ProviderFlow != nil {
		factories = root.ProviderFlow.ParentFactories
	}
	for _, f := range factories {
		if r, ok := f.(interface{ ReservedIdentityProfileIDs() []int64 }); ok {
			for _, id := range r.ReservedIdentityProfileIDs() {
				scope.ReservedIDs[id] = true
			}
		}
	}
	current := func(source int64) string {
		f, ok := factories[source].(managedWeixinApplicationFactory)
		if !ok || f.PayType() != "wxpay" || f.ParentRestoreOnly() {
			return ""
		}
		mids := []int64{}
		for mid, ids := range root.MerchantChannels {
			for _, id := range ids {
				if id == source {
					mids = append(mids, mid)
					break
				}
			}
		}
		sort.Slice(mids, func(i, j int) bool { return mids[i] < mids[j] })
		raw, _ := json.Marshal([]any{source, f.TemplateCommitment(), mids})
		hash := sha256.Sum256(raw)
		return hex.EncodeToString(hash[:])
	}
	scope.CurrentSource = current
	scope.Allows = func(mid, source int64) bool {
		for _, id := range root.MerchantChannels[mid] {
			if id == source {
				return true
			}
		}
		return false
	}
	scope.ValidateProfiles = func(apps []payment.WeixinApplicationSpec) error {
		healthy := []payment.WeixinApplicationSpec{}
		for _, p := range apps {
			if !scope.ReservedIDs[p.ID] {
				healthy = append(healthy, p)
			}
		}
		bySource, err := managedWeixinSpecs(healthy, scope)
		if err != nil {
			return err
		}
		for source, profiles := range bySource {
			f, ok := factories[source].(managedWeixinApplicationFactory)
			if !ok {
				return payment.ErrState
			}
			if _, err := f.WithManagedWeixinProfiles(profiles); err != nil {
				return payment.ErrInvalid
			}
		}
		return nil
	}
	ids := []int64{}
	for id := range factories {
		if current(id) != "" {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := []weixinApplicationSource{}
	for _, id := range ids {
		fp := current(id)
		scope.SourceFingerprints[id] = fp
		mids := []int64{}
		for mid := range root.MerchantChannels {
			if scope.Allows(mid, id) {
				mids = append(mids, mid)
			}
		}
		sort.Slice(mids, func(i, j int) bool { return mids[i] < mids[j] })
		list := []string{}
		for _, mid := range mids {
			list = append(list, strconv.FormatInt(mid, 10))
		}
		out = append(out, weixinApplicationSource{ID: id, Fingerprint: fp, MerchantIDs: list})
	}
	return scope, out, nil
}

func managedWeixinSpecs(apps []payment.WeixinApplicationSpec, scope payment.WeixinApplicationScope) (map[int64][]channelconfig.IdentityProfileSpec, error) {
	managed := map[int64][]channelconfig.IdentityProfileSpec{}
	for _, p := range apps {
		if p.Archived {
			continue
		}
		if scope.ReservedIDs[p.ID] {
			return nil, payment.ErrConflict
		}
		if !p.ExecutionReady() || scope.SourceFingerprints[p.SourceChannelID] == "" {
			continue
		}
		allowed := []int64{}
		for _, mid := range p.MerchantIDs {
			if scope.Allows != nil && scope.Allows(mid, p.SourceChannelID) {
				allowed = append(allowed, mid)
			}
		}
		if len(allowed) == 0 {
			continue
		}
		// Keep authenticated app scope bytes stable when an unrelated source
		// grant changes. Actual execution still intersects the live root grant;
		// an explicit vault scope edit changes revision/fingerprint and is stale.
		managed[p.SourceChannelID] = append(managed[p.SourceChannelID], managedWeixinProfile(p))
	}
	return managed, nil
}

func managedWeixinProfile(p payment.WeixinApplicationSpec) channelconfig.IdentityProfileSpec {
	kind := "web"
	if p.Kind == "mini_program" {
		kind = "mini"
	}
	return channelconfig.IdentityProfileSpec{ManagedVaultFingerprint: adapter.ManagedWeixinVaultOrigin + p.Fingerprint, ID: p.ID, Revision: p.Revision, ChannelID: p.SourceChannelID, Enabled: true, Provider: "wxpay", Kind: kind, AppID: p.AppID, AccountMode: "direct", Path: p.Path, MerchantIDs: append(channelconfig.IdentityMerchantIDs{}, p.MerchantIDs...), Config: map[string]string{"appsecret": p.AppSecret}}
}

// Only new catalogs/intent selection call this current vault resolver. An
// original payment restores its own sealed source through the base factory.
func (s *Server) managedWeixinRuntime(ctx context.Context) (*Server, error) {
	return s.managedWeixinRuntimeIDs(ctx, nil, false)
}

func (s *Server) managedWeixinRuntimeIDs(ctx context.Context, ids []int64, requiredManaged bool) (*Server, error) {
	root := s.weixinApplicationRoot()
	if root.Store == nil || root.Pool == nil {
		return s, nil
	}
	catalog, e := root.Store.ReadWeixinApplicationCatalog(ctx, ids)
	if e != nil {
		return nil, e
	}
	apps := catalog.Specs
	scope, _, e := root.weixinApplicationScope()
	if e != nil {
		return nil, e
	}
	blocked := map[int64]bool{}
	for _, id := range catalog.InvalidIDs {
		blocked[id] = true
	}
	for _, id := range catalog.InactiveIDs {
		blocked[id] = true
	}
	active := map[int64]bool{}
	healthy := []payment.WeixinApplicationSpec{}
	for _, p := range apps {
		if scope.ReservedIDs[p.ID] {
			blocked[p.ID] = true
			continue
		}
		healthy = append(healthy, p)
		active[p.ID] = true
	}
	if requiredManaged {
		for _, id := range ids {
			if !active[id] {
				blocked[id] = true
			}
		}
	}
	managed, e := managedWeixinSpecs(healthy, scope)
	if e != nil {
		return nil, e
	}
	if len(managed) == 0 && len(blocked) == 0 {
		return s, nil
	}
	if s.ProviderFlow == nil || root.ProviderFlow == nil {
		return nil, payment.ErrState
	}
	out := s.executionView()
	flow := *s.ProviderFlow
	flow.ParentFactories = make(map[int64]adapter.ParentAccountFactory, len(root.ProviderFlow.ParentFactories))
	for id, f := range root.ProviderFlow.ParentFactories {
		flow.ParentFactories[id] = f
	}
	out.IdentityProfiles = make(map[int64]channelconfig.IdentityProfile, len(root.IdentityProfiles))
	for id, p := range root.IdentityProfiles {
		out.IdentityProfiles[id] = p
	}
	for source, profiles := range managed {
		f, ok := root.ProviderFlow.ParentFactories[source].(managedWeixinApplicationFactory)
		if !ok {
			return nil, payment.ErrState
		}
		next, err := f.WithManagedWeixinProfiles(profiles)
		if err != nil {
			return nil, payment.ErrConflict
		}
		flow.ParentFactories[source] = next
		if root.IdentityProfileEnabled {
			builder, ok := next.(managedWeixinApplicationFactory)
			if !ok {
				return nil, payment.ErrState
			}
			built, err := builder.ManagedIdentityProfiles(profiles, true)
			if err != nil {
				return nil, payment.ErrInvalid
			}
			for id, p := range built {
				if _, exists := out.IdentityProfiles[id]; exists {
					return nil, payment.ErrConflict
				}
				out.IdentityProfiles[id] = p
			}
		}
	}
	if len(blocked) > 0 {
		bad := []int64{}
		for id := range blocked {
			bad = append(bad, id)
			delete(out.IdentityProfiles, id)
		}
		for source, factory := range flow.ParentFactories {
			if f, ok := factory.(managedWeixinApplicationFactory); ok {
				masked, err := f.WithoutWeixinProfileIDs(bad)
				if err != nil {
					return nil, payment.ErrConflict
				}
				flow.ParentFactories[source] = masked
			}
		}
	}
	out.ProviderFlow = &flow
	return out, nil
}
