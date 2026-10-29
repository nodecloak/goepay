package channelconfig

import (
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"sort"
	"strings"
)

type ParentBuildOptions struct {
	BuildOptions
	OAuthEnabled     bool
	WebOAuthAllowed  func() bool
	MiniOAuthAllowed func() bool
	IdentityProfiles []IdentityProfileSpec
}
type ParentChannelFactory struct {
	origin         Channel
	options        ParentBuildOptions
	restoreOnly    bool
	profileCatalog map[int64]IdentityProfileSpec
}

func parentConfigKeys(kind string) map[string]bool {
	if spec, ok := adapter.LookupPlugin(kind); ok {
		out := map[string]bool{"costrate": true, "account_mode": true, "ecommerce": true, "mode": true, "pay_type": true}
		for _, in := range spec.Inputs {
			if !in.PEM {
				out[in.Key] = true
			}
		}
		return out
	}
	keys := []string{"appid", "appmchid", "costrate", "account_mode", "ecommerce", "mode", "enable_scan_pay", "enable_web_oauth", "enable_mini_launch", "mini_appid", "mini_path", "mini_program_id", "enable_channel_sharing"}
	if kind == "wxpay" {
		keys = append(keys, "wechat_api_version", "appkey", "oauth_appsecret", "mini_secret", "enable_bank_transfer", "bank_public_key", "h5_site_url", "h5_site_name", "v3_api_key", "v3_merchant_serial", "v3_encryption_serial")
	} else if kind == "alipay" {
		keys = append(keys, "appsecret", "appkey", "sign_type", "enable_bank_transfer", "bank_transfer_scene", "bank_transfer_scene_reports", "preauth_voucher_product_code", "enable_channel_sharing_return", "sharing_return_evidence")
	}
	out := map[string]bool{}
	for _, key := range keys {
		out[key] = true
	}
	return out
}
func parentCertificateKey(kind, key string) bool {
	if spec, ok := adapter.LookupPlugin(kind); ok {
		in, found := spec.Input(key)
		return found && in.PEM
	}
	if kind == "alipay" {
		return key == "app_cert_path" || key == "alipay_cert_path" || key == "root_cert_path"
	}
	if kind != "wxpay" {
		return false
	}
	if key == "sslcert_path" || key == "sslkey_path" || key == "publickey_path" || key == "v3_merchant_key" {
		return true
	}
	if strings.HasPrefix(key, "v3_verifier_") {
		serial := strings.TrimPrefix(key, "v3_verifier_")
		if len(serial) < 1 || len(serial) > 128 {
			return false
		}
		for _, c := range serial {
			if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '_' || c == '-') {
				return false
			}
		}
		return true
	}
	return false
}
func cloneParentChannel(c Channel) Channel {
	raw, _ := json.Marshal(c)
	var out Channel
	_ = json.Unmarshal(raw, &out)
	return out
}
func NewParentChannelFactory(origin Channel, options ParentBuildOptions) (*ParentChannelFactory, error) {
	f, e := parentFactoryOrigin(origin, options)
	if e != nil {
		return nil, e
	}
	if _, _, e = f.BuildSnapshot(origin.ID, f.origin); e != nil {
		return nil, e
	}
	return f, nil
}
func parentFactoryOrigin(origin Channel, options ParentBuildOptions) (*ParentChannelFactory, error) {
	if origin.ID <= 0 || origin.Config == nil || !KnownChannelPlugin(origin.Plugin) || !supportedCollectionMode(origin) || !directIdentityChannel(origin) {
		return nil, ErrImport
	}
	if spec, ok := adapter.LookupPlugin(origin.Plugin); ok && !spec.Serves(origin.Config["pay_type"]) {
		return nil, ErrImport
	}
	original := cloneParentChannel(origin)
	// Old imports may carry unused legacy flags. Retain only fields consumed by
	// the closed constructors; never turn a service-provider mode into direct.
	allowed := parentConfigKeys(original.Plugin)
	for key := range original.Config {
		if !allowed[key] {
			delete(original.Config, key)
		}
	}
	for key := range original.Certificates {
		if !parentCertificateKey(original.Plugin, key) {
			delete(original.Certificates, key)
		}
	}
	f := &ParentChannelFactory{origin: original, options: options}
	if err := f.installWXBindingCatalog(options.IdentityProfiles); err != nil {
		return nil, err
	}
	return f, nil
}
func (f *ParentChannelFactory) ParentRestoreOnly() bool { return f != nil && f.restoreOnly }
func (f *ParentChannelFactory) Bootstrap() Channel {
	if f == nil {
		return Channel{}
	}
	return cloneParentChannel(f.origin)
}
func (f *ParentChannelFactory) Fields() []adapter.ParentChannelField {
	if f == nil {
		return nil
	}
	if spec, ok := adapter.LookupPlugin(f.origin.Plugin); ok {
		out := []adapter.ParentChannelField{{Name: "costrate", Namespace: "config", Label: "成本费率（%）"}}
		for _, in := range spec.Inputs {
			ns := "config"
			if in.PEM {
				ns = "certificates"
			}
			field := adapter.ParentChannelField{Name: in.Key, Namespace: ns, Secret: in.Secret, PEM: in.PEM, Label: in.Label, Note: in.Note}
			for _, o := range in.Options {
				field.Options = append(field.Options, adapter.ParentChannelFieldOption{Value: o.Value, Label: o.Label})
			}
			out = append(out, field)
		}
		return out
	}
	out := []adapter.ParentChannelField{}
	for key := range parentConfigKeys(f.origin.Plugin) {
		if key == "account_mode" || key == "ecommerce" || key == "mode" {
			continue
		}
		secret := key == "appsecret" || key == "oauth_appsecret" || key == "mini_secret" || key == "v3_api_key" || (key == "appkey" && f.origin.Plugin == "wxpay")
		out = append(out, adapter.ParentChannelField{Name: key, Namespace: "config", Secret: secret})
	}
	for _, key := range []string{"sslcert_path", "sslkey_path", "publickey_path", "v3_merchant_key", "app_cert_path", "alipay_cert_path", "root_cert_path"} {
		if parentCertificateKey(f.origin.Plugin, key) {
			out = append(out, adapter.ParentChannelField{Name: key, Namespace: "certificates", Secret: key == "sslkey_path" || key == "v3_merchant_key", PEM: true})
		}
	}
	for key := range f.origin.Certificates {
		if strings.HasPrefix(key, "v3_verifier_") {
			out = append(out, adapter.ParentChannelField{Name: key, Namespace: "certificates", PEM: true})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out
}
func (f *ParentChannelFactory) BuildSnapshot(id int64, c Channel) (adapter.ParentChannelExecution, []byte, error) {
	return f.buildParentSnapshot(id, c, nil)
}
func (f *ParentChannelFactory) buildParentSnapshot(id int64, c Channel, bindings *parentWXBindings) (adapter.ParentChannelExecution, []byte, error) {
	var out adapter.ParentChannelExecution
	if f == nil || id <= 0 || c.Plugin != f.origin.Plugin || c.Config == nil || !supportedCollectionMode(c) || !directIdentityChannel(c) {
		return out, nil, ErrImport
	}
	c = cloneParentChannel(c)
	c.ID = id
	if _, plugin := adapter.LookupPlugin(c.Plugin); plugin && c.Config["pay_type"] != f.origin.Config["pay_type"] {
		return out, nil, ErrImport
	}
	allowed := parentConfigKeys(c.Plugin)
	for key, value := range c.Config {
		if !allowed[key] || len(value) > 131072 || strings.ContainsRune(value, 0) {
			return out, nil, ErrImport
		}
	}
	if len(c.Certificates) > 32 {
		return out, nil, ErrImport
	}
	for key, value := range c.Certificates {
		if !parentCertificateKey(c.Plugin, key) || len(value) > 131072 || strings.ContainsRune(value, 0) {
			return out, nil, ErrImport
		}
	}
	imp := Import{Version: 1, Channels: []Channel{c}}
	providers, e := BuildWithOptions(imp, f.options.BuildOptions)
	if e != nil {
		return out, nil, ErrImport
	}
	out.Payment = providers[id]
	if f.options.OAuthEnabled {
		web, e := BuildOAuth(imp)
		if e != nil {
			return out, nil, ErrImport
		}
		mini, e := BuildMiniOAuth(imp)
		if e != nil {
			return out, nil, ErrImport
		}
		out.Web = web[id]
		out.Mini = mini[id]
		if out.Web != nil {
			out.Web = parentWebOAuthGuard{WebOAuthProvider: out.Web, allowed: parentOAuthAllowed(f.options, false)}
		}
		if out.Mini != nil {
			out.Mini = parentMiniOAuthGuard{MiniOAuthProvider: out.Mini, allowed: parentOAuthAllowed(f.options, true)}
		}
	}
	out, e = f.withWXBindings(out, c, bindings)
	if e != nil {
		return adapter.ParentChannelExecution{}, nil, e
	}
	raw, e := json.Marshal(parentAccountSnapshot{Version: 1, Channel: c, Bindings: bindings})
	if e != nil || len(raw) > 1048576 {
		return adapter.ParentChannelExecution{}, nil, ErrImport
	}
	return out, raw, nil
}
func (f *ParentChannelFactory) Restore(id int64, raw []byte) (adapter.ParentChannelExecution, error) {
	in, e := f.decodeParentWithBindings(id, raw)
	if e != nil {
		return adapter.ParentChannelExecution{}, e
	}
	out, _, e := f.buildParentSnapshot(id, in.Channel, in.Bindings)
	return out, e
}
