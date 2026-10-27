package channelconfig

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"sort"

	adapter "goepay/internal/adapters/payment"
)

// These profiles exist only inside Store-authenticated account ciphertext.
// They are never read from generic application metadata or request credentials.
type parentWXBindings struct {
	Web     *IdentityProfileSpec `json:"web,omitempty"`
	Mini    *IdentityProfileSpec `json:"mini,omitempty"`
	WebSet  bool                 `json:"web_set,omitempty"`
	MiniSet bool                 `json:"mini_set,omitempty"`
}
type parentAccountSnapshot struct {
	Version  int               `json:"version"`
	Channel  Channel           `json:"channel"`
	Bindings *parentWXBindings `json:"weixin_bindings,omitempty"`
}

func (f *ParentChannelFactory) decodeParentWithBindings(id int64, raw []byte) (parentAccountSnapshot, error) {
	var in parentAccountSnapshot
	if f == nil || id <= 0 || len(raw) == 0 || len(raw) > 1048576 {
		return in, ErrImport
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || in.Version != 1 || in.Channel.ID != id || in.Channel.Plugin != f.origin.Plugin || in.Channel.Config == nil || !supportedCollectionMode(in.Channel) || !directIdentityChannel(in.Channel) {
		return in, ErrImport
	}
	if _, err := d.Token(); err != io.EOF {
		return in, ErrImport
	}
	for key := range in.Channel.Config {
		if !parentConfigKeys(in.Channel.Plugin)[key] {
			return in, ErrImport
		}
	}
	for key := range in.Channel.Certificates {
		if !parentCertificateKey(in.Channel.Plugin, key) {
			return in, ErrImport
		}
	}
	if in.Bindings != nil && in.Channel.Plugin != "wxpay" {
		return in, ErrImport
	}
	return in, nil
}

func cloneIdentitySpec(spec IdentityProfileSpec) (IdentityProfileSpec, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return IdentityProfileSpec{}, ErrImport
	}
	var out IdentityProfileSpec
	if json.Unmarshal(raw, &out) != nil {
		return out, ErrImport
	}
	return out, nil
}
func (f *ParentChannelFactory) installWXBindingCatalog(specs []IdentityProfileSpec) error {
	f.profileCatalog = map[int64]IdentityProfileSpec{}
	selected := []IdentityProfileSpec{}
	for _, spec := range specs {
		if spec.Provider != "wxpay" || spec.ChannelID != f.origin.ID {
			continue
		}
		if f.origin.Plugin != "wxpay" || spec.ID <= 0 || f.profileCatalog[spec.ID].ID != 0 {
			return ErrImport
		}
		cloned, err := cloneIdentitySpec(spec)
		if err != nil {
			return err
		}
		f.profileCatalog[spec.ID] = cloned
		selected = append(selected, cloned)
	}
	if len(selected) > 1000 {
		return ErrImport
	}
	_, err := BuildIdentityProfiles(Import{Version: 1, Channels: []Channel{f.origin}, IdentityProfiles: selected}, true)
	return err
}
func (f *ParentChannelFactory) ParentWeixinCatalog() adapter.ParentWeixinCatalog {
	out := adapter.ParentWeixinCatalog{Items: []adapter.ParentWeixinOption{}}
	if f == nil {
		return out
	}
	ids := []int64{}
	for id := range f.profileCatalog {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	private := []IdentityProfileSpec{}
	for _, id := range ids {
		spec := f.profileCatalog[id]
		private = append(private, spec)
		if spec.Enabled {
			out.Items = append(out.Items, adapter.ParentWeixinOption{ID: id, Revision: spec.Revision, Kind: spec.Kind, AppID: spec.AppID})
		}
	}
	raw, _ := json.Marshal(struct {
		Origin   int64
		Profiles []IdentityProfileSpec
	}{f.origin.ID, private})
	sum := sha256.Sum256(raw)
	out.Fingerprint = hex.EncodeToString(sum[:])
	return out
}
func (f *ParentChannelFactory) editWXBindings(c Channel, old *parentWXBindings, in adapter.ParentChannelAccountChange) (*parentWXBindings, error) {
	if in.AppWXMP == nil && in.AppWXA == nil {
		if in.ExpectedBindings != "" {
			return nil, ErrImport
		}
		return old, nil
	}
	if c.Plugin != "wxpay" || subtle.ConstantTimeCompare([]byte(in.ExpectedBindings), []byte(f.ParentWeixinCatalog().Fingerprint)) != 1 {
		return nil, ErrImport
	}
	next := parentWXBindings{}
	if old != nil {
		next = *old
	}
	apply := func(id *int64, kind string, dst **IdentityProfileSpec) error {
		if id == nil {
			return nil
		}
		if *id == 0 {
			*dst = nil
			return nil
		}
		spec, ok := f.profileCatalog[*id]
		if !ok || !spec.Enabled || spec.Kind != kind {
			return ErrImport
		}
		copy, err := cloneIdentitySpec(spec)
		if err != nil {
			return err
		}
		*dst = &copy
		return nil
	}
	if err := apply(in.AppWXMP, "web", &next.Web); err != nil {
		return nil, err
	}
	if err := apply(in.AppWXA, "mini", &next.Mini); err != nil {
		return nil, err
	}
	if in.AppWXMP != nil {
		next.WebSet = true
	}
	if in.AppWXA != nil {
		next.MiniSet = true
	}
	return &next, nil
}
func profileAllows(spec *IdentityProfileSpec, mid int64) bool {
	if spec == nil || mid <= 0 {
		return false
	}
	for _, id := range spec.MerchantIDs {
		if id == mid {
			return true
		}
	}
	return false
}

func (f *ParentChannelFactory) currentWXProfile(spec *IdentityProfileSpec) bool {
	if spec == nil {
		return false
	}
	current, ok := f.profileCatalog[spec.ID]
	if !ok || !current.Enabled {
		return false
	}
	sealedRaw, err := json.Marshal(spec)
	if err != nil {
		return false
	}
	currentRaw, err := json.Marshal(current)
	if err != nil {
		return false
	}
	a, b := sha256.Sum256(sealedRaw), sha256.Sum256(currentRaw)
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}
func (f *ParentChannelFactory) wxBindingSummary(spec *IdentityProfileSpec) *adapter.ParentWeixinBindingSummary {
	if spec == nil {
		return nil
	}
	return &adapter.ParentWeixinBindingSummary{ID: spec.ID, Revision: spec.Revision, AppID: spec.AppID, Current: f.currentWXProfile(spec)}
}

func (f *ParentChannelFactory) withWXBindings(base adapter.ParentChannelExecution, c Channel, bindings *parentWXBindings) (adapter.ParentChannelExecution, error) {
	if bindings == nil {
		return base, nil
	}
	if c.Plugin != "wxpay" {
		return adapter.ParentChannelExecution{}, ErrImport
	}
	variants := map[string]adapter.Provider{}
	specs := map[string]*IdentityProfileSpec{"web": bindings.Web, "mini": bindings.Mini}
	for kind, spec := range specs {
		if spec == nil {
			continue
		}
		if spec.ChannelID != f.origin.ID || spec.Provider != "wxpay" || !spec.Enabled || spec.Kind != kind {
			return adapter.ParentChannelExecution{}, ErrImport
		}
		profiles, err := BuildIdentityProfiles(Import{Version: 1, Channels: []Channel{f.origin}, IdentityProfiles: []IdentityProfileSpec{*spec}}, true)
		if err != nil {
			return adapter.ParentChannelExecution{}, err
		}
		profile := profiles[spec.ID]
		variant := cloneParentChannel(c)
		variant.Config["appid"] = spec.AppID
		// A selected application never inherits another application's page/AppID.
		for _, key := range []string{"enable_mini_launch", "mini_appid", "mini_path", "mini_program_id", "mini_secret", "enable_web_oauth", "oauth_appsecret"} {
			delete(variant.Config, key)
		}
		jsapi := false
		for _, product := range c.AppTypes {
			if product == "2" {
				jsapi = true
			}
		}
		if kind == "mini" && jsapi {
			variant.Config["enable_mini_launch"] = "true"
			variant.Config["mini_appid"] = spec.AppID
			variant.Config["mini_path"] = spec.Path
			variant.Config["mini_secret"] = spec.Config["appsecret"]
		}
		built, _, err := f.buildParentSnapshot(c.ID, variant, nil)
		if err != nil {
			return adapter.ParentChannelExecution{}, err
		}
		variants[kind] = built.Payment
		if f.options.OAuthEnabled {
			if kind == "web" {
				base.Web = parentWebOAuthGuard{WebOAuthProvider: profile.Web, allowed: parentOAuthAllowed(f.options, false)}
			} else {
				base.Mini = parentMiniOAuthGuard{MiniOAuthProvider: profile.Mini, allowed: parentOAuthAllowed(f.options, true)}
			}
		}
	}
	if bindings.Web == nil && bindings.WebSet {
		base.Web = nil
	}
	if bindings.Mini == nil && bindings.MiniSet {
		base.Mini = nil
	}
	original := base.Payment
	base.PaymentSelection = func(method string, mini bool, mid int64) (adapter.Provider, string, error) {
		kind := ""
		if method == "applet" || method == "jsapi" && mini {
			kind = "mini"
		} else if method == "jsapi" {
			kind = "web"
		}
		if spec := specs[kind]; spec != nil {
			if !profileAllows(spec, mid) {
				return nil, "", ErrImport
			}
			return variants[kind], kind, nil
		}
		return original, "", nil
	}
	base.PaymentAllowed = func(variant string, mid int64) bool {
		if variant == "" {
			return true
		}
		sealed := specs[variant]
		if !profileAllows(sealed, mid) {
			return false
		}
		current, ok := f.profileCatalog[sealed.ID]
		return ok && current.Enabled && current.Kind == variant && profileAllows(&current, mid)
	}
	base.PaymentFreshAllowed = func(variant string) bool {
		return variant == "" || f.currentWXProfile(specs[variant])
	}
	return base, nil
}
