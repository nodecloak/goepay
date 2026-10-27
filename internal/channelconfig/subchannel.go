package channelconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"io"
	"regexp"
	"sort"
	"strconv"
)

// PaymentSubchannelTemplate is accepted only inside the authenticated encrypted
// import. Slots explicitly replace a closed set of account fields; URLs, modes,
// products and provider IDs always come from the original parent.
type PaymentSubchannelTemplate struct {
	ChannelID        int64             `json:"channel_id,string"`
	Revision         int64             `json:"revision,string"`
	ConfigSlots      map[string]string `json:"config_slots"`
	CertificateSlots map[string]string `json:"certificate_slots,omitempty"`
}

var childLogicalName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

func (s *PaymentSubchannelTemplate) UnmarshalJSON(raw []byte) error {
	if len(raw) > 8192 {
		return ErrImport
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return ErrImport
	}
	seen := map[string]bool{}
	for d.More() {
		v, e := d.Token()
		key, ok := v.(string)
		if e != nil || !ok || seen[key] {
			return ErrImport
		}
		seen[key] = true
		var b json.RawMessage
		if d.Decode(&b) != nil || bytes.Equal(b, []byte("null")) {
			return ErrImport
		}
		switch key {
		case "channel_id", "revision":
			var text string
			if json.Unmarshal(b, &text) != nil {
				return ErrImport
			}
			n, e := strconv.ParseInt(text, 10, 64)
			if e != nil || n <= 0 || strconv.FormatInt(n, 10) != text {
				return ErrImport
			}
			if key == "channel_id" {
				s.ChannelID = n
			} else {
				s.Revision = n
			}
		case "config_slots", "certificate_slots":
			m, e := decodeChildSlots(b)
			if e != nil {
				return e
			}
			if key == "config_slots" {
				s.ConfigSlots = m
			} else {
				s.CertificateSlots = m
			}
		default:
			return ErrImport
		}
	}
	if _, e = d.Token(); e != nil || s.ChannelID <= 0 || s.Revision <= 0 || len(s.ConfigSlots)+len(s.CertificateSlots) == 0 {
		return ErrImport
	}
	return nil
}
func decodeChildSlots(b []byte) (map[string]string, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	v, e := d.Token()
	if e != nil || v != json.Delim('{') {
		return nil, ErrImport
	}
	out := map[string]string{}
	for d.More() {
		k, e := d.Token()
		key, ok := k.(string)
		if e != nil || !ok || out[key] != "" {
			return nil, ErrImport
		}
		var value string
		if d.Decode(&value) != nil || !childLogicalName.MatchString(value) {
			return nil, ErrImport
		}
		out[key] = value
	}
	if _, e = d.Token(); e != nil || len(out) > 24 {
		return nil, ErrImport
	}
	return out, nil
}

type childFactory struct {
	parent              Channel
	spec                PaymentSubchannelTemplate
	options             BuildOptions
	schema              adapter.SubchannelSchema
	aliasOrigin         int64
	aliasParentSnapshot []byte
	restoreOnly         bool
}

func (f childFactory) ParentSubchannelOrigin() (int64, []byte) {
	return f.aliasOrigin, append([]byte(nil), f.aliasParentSnapshot...)
}

func (f childFactory) DeriveParentSubchannel(parentID int64, raw []byte) (adapter.SubchannelFactory, error) {
	if f.aliasOrigin != 0 || parentID <= 0 || parentID == f.parent.ID || len(raw) == 0 || len(raw) > 1048576 {
		return nil, ErrImport
	}
	origin, err := parentFactoryOrigin(f.parent, ParentBuildOptions{BuildOptions: f.options})
	if err != nil {
		return nil, err
	}
	parent, err := origin.decodeParent(parentID, raw)
	if err != nil || parent.ID != parentID || parent.Plugin != f.parent.Plugin {
		return nil, ErrImport
	}
	spec := f.spec
	spec.ChannelID = parentID
	derived, err := BuildPaymentSubchannelFactories(Import{Version: 1, Channels: []Channel{parent}, PaymentSubchannelTemplates: []PaymentSubchannelTemplate{spec}}, f.options)
	if err != nil {
		return nil, err
	}
	alias, ok := derived[parentID].(childFactory)
	if !ok {
		return nil, ErrImport
	}
	alias.aliasOrigin = f.parent.ID
	alias.aliasParentSnapshot = append([]byte(nil), raw...)
	return alias, nil
}

func (f childFactory) Schema() adapter.SubchannelSchema {
	s := f.schema
	s.Fields = append([]adapter.SubchannelField(nil), s.Fields...)
	return s
}
func childConfigFields(c Channel) map[string]bool {
	if c.Plugin == "alipay" {
		return map[string]bool{"appid": false, "appmchid": false, "appsecret": true, "appkey": true}
	}
	if c.Plugin == "wxpay" && c.Config["wechat_api_version"] == "v3" {
		return map[string]bool{"appid": false, "appmchid": false, "v3_merchant_serial": false, "v3_api_key": true, "oauth_appsecret": true}
	}
	if c.Plugin == "wxpay" {
		return map[string]bool{"appid": false, "appmchid": false, "appkey": true, "oauth_appsecret": true, "mini_appid": false, "mini_secret": true}
	}
	return nil
}
func childCertificateFields(c Channel) map[string]bool {
	if c.Plugin == "alipay" {
		return map[string]bool{"app_cert_path": true, "alipay_cert_path": true, "root_cert_path": true}
	}
	if c.Config["wechat_api_version"] == "v3" {
		return map[string]bool{"v3_merchant_key": true}
	}
	return map[string]bool{"sslcert_path": true, "sslkey_path": true}
}
func cloneChildChannel(c Channel) Channel {
	out := c
	out.AppTypes = append([]string(nil), c.AppTypes...)
	out.Config = map[string]string{}
	out.Certificates = map[string]string{}
	for k, v := range c.Config {
		out.Config[k] = v
	}
	for k, v := range c.Certificates {
		out.Certificates[k] = v
	}
	return out
}
func BuildPaymentSubchannelFactories(imp Import, options BuildOptions) (map[int64]adapter.SubchannelFactory, error) {
	return buildPaymentSubchannelFactories(imp, options, nil)
}

func buildPaymentSubchannelFactories(imp Import, options BuildOptions, restoreOnly map[int64]bool) (map[int64]adapter.SubchannelFactory, error) {
	out := map[int64]adapter.SubchannelFactory{}
	if imp.Version != 1 || len(imp.PaymentSubchannelTemplates) > 1000 {
		return nil, ErrImport
	}
	parents := map[int64]Channel{}
	for _, c := range imp.Channels {
		if c.ID <= 0 || parents[c.ID].ID != 0 {
			return nil, ErrImport
		}
		parents[c.ID] = cloneChildChannel(c)
	}
	for _, original := range imp.PaymentSubchannelTemplates {
		spec := original
		spec.ConfigSlots = map[string]string{}
		spec.CertificateSlots = map[string]string{}
		for k, v := range original.ConfigSlots {
			spec.ConfigSlots[k] = v
		}
		for k, v := range original.CertificateSlots {
			spec.CertificateSlots[k] = v
		}
		c, ok := parents[spec.ChannelID]
		if !ok || out[c.ID] != nil || spec.Revision <= 0 || !directIdentityChannel(c) || !supportedCollectionMode(c) {
			return nil, ErrImport
		}
		if !restoreOnly[c.ID] {
			if _, e := BuildWithOptions(Import{Version: 1, Channels: []Channel{c}}, options); e != nil {
				return nil, ErrImport
			}
		}
		fields := []adapter.SubchannelField{}
		used := map[string]bool{}
		for which, slots := range []map[string]string{spec.ConfigSlots, spec.CertificateSlots} {
			allowed := childConfigFields(c)
			if which == 1 {
				allowed = childCertificateFields(c)
			}
			for target, logical := range slots {
				secret, ok := allowed[target]
				if !ok || !childLogicalName.MatchString(logical) {
					return nil, ErrImport
				}
				if prior, exists := used[logical]; exists {
					if prior != secret {
						return nil, ErrImport
					}
					continue
				}
				used[logical] = secret
				fields = append(fields, adapter.SubchannelField{Name: logical, Label: childFieldLabel(c, target, which == 1), Secret: secret, Required: true})
			}
		}
		if len(fields) == 0 || len(fields) > 24 {
			return nil, ErrImport
		}
		sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
		schemaParent := cloneChildChannel(c)
		delete(schemaParent.Config, "costrate")
		for target, slot := range spec.ConfigSlots {
			schemaParent.Config[target] = "[" + slot + "]"
		}
		for target, slot := range spec.CertificateSlots {
			schemaParent.Certificates[target] = "[" + slot + "]"
		}
		raw, e := json.Marshal(struct {
			Parent Channel
			Spec   PaymentSubchannelTemplate
		}{schemaParent, spec})
		if e != nil {
			return nil, ErrImport
		}
		hash := sha256.Sum256(raw)
		out[c.ID] = childFactory{parent: c, spec: spec, options: options, restoreOnly: restoreOnly[c.ID], schema: adapter.SubchannelSchema{ParentID: c.ID, Revision: spec.Revision, PayType: c.Plugin, Fields: fields, Fingerprint: hex.EncodeToString(hash[:])}}
	}
	return out, nil
}

type childAccountSnapshot struct {
	Version  int
	ParentID int64
	Schema   string
	Channel  Channel
}

func (f childFactory) resolved(values map[string]string) (Channel, error) {
	if f.restoreOnly {
		return Channel{}, ErrImport
	}
	if len(values) != len(f.schema.Fields) {
		return Channel{}, ErrImport
	}
	for _, field := range f.schema.Fields {
		v, ok := values[field.Name]
		if !ok || len(v) == 0 || len(v) > 65536 || bytes.IndexByte([]byte(v), 0) >= 0 {
			return Channel{}, ErrImport
		}
	}
	c := cloneChildChannel(f.parent)
	for target, key := range f.spec.ConfigSlots {
		c.Config[target] = values[key]
	}
	for target, key := range f.spec.CertificateSlots {
		c.Certificates[target] = values[key]
	}
	return c, nil
}
func (f childFactory) build(c Channel) (adapter.SubchannelExecution, error) {
	var out adapter.SubchannelExecution
	imp := Import{Version: 1, Channels: []Channel{c}}
	payments, e := BuildWithOptions(imp, f.options)
	if e != nil {
		return out, ErrImport
	}
	var web map[int64]adapter.WebOAuthProvider
	var mini map[int64]adapter.MiniOAuthProvider
	if f.options.EnableSubchannelOAuth {
		web, e = BuildOAuth(imp)
		if e != nil {
			return out, ErrImport
		}
		mini, e = BuildMiniOAuth(imp)
		if e != nil {
			return out, ErrImport
		}
	}
	out.SchemaFingerprint = f.schema.Fingerprint
	out.Payment, out.Web, out.Mini = payments[c.ID], web[c.ID], mini[c.ID]
	if out.Payment == nil {
		return adapter.SubchannelExecution{}, ErrImport
	}
	return out, nil
}
func (f childFactory) Build(values map[string]string) (adapter.SubchannelExecution, error) {
	c, e := f.resolved(values)
	if e != nil {
		return adapter.SubchannelExecution{}, e
	}
	return f.build(c)
}
func (f childFactory) Snapshot(values map[string]string) ([]byte, error) {
	c, e := f.resolved(values)
	if e != nil {
		return nil, e
	}
	if _, e = f.build(c); e != nil {
		return nil, e
	}
	raw, e := json.Marshal(childAccountSnapshot{Version: 1, ParentID: c.ID, Schema: f.schema.Fingerprint, Channel: c})
	if e != nil || len(raw) > 262144 {
		return nil, ErrImport
	}
	return raw, nil
}

// Restore accepts only the authenticated decrypted immutable account material
// supplied by Store. Current factory options retain all runtime write gates;
// inherited original credentials are restored from the sealed version itself.
func (f childFactory) Restore(snapshot []byte) (adapter.SubchannelExecution, error) {
	var s childAccountSnapshot
	d := json.NewDecoder(bytes.NewReader(snapshot))
	d.DisallowUnknownFields()
	if len(snapshot) > 262144 || d.Decode(&s) != nil || s.Version != 1 || s.ParentID != f.parent.ID || s.Channel.ID != f.parent.ID || s.Channel.Plugin != f.parent.Plugin || len(s.Schema) != 64 || !directIdentityChannel(s.Channel) || !supportedCollectionMode(s.Channel) {
		return adapter.SubchannelExecution{}, ErrImport
	}
	if _, e := d.Token(); e != io.EOF {
		return adapter.SubchannelExecution{}, ErrImport
	}
	// Account credentials stay original. Costrate is an inherited current parent
	// estimate for new quotes; existing orders consume their immutable quote.
	delete(s.Channel.Config, "costrate")
	if rate, present := f.parent.Config["costrate"]; present {
		s.Channel.Config["costrate"] = rate
	}
	out, e := f.build(s.Channel)
	if e != nil {
		return out, e
	}
	out.SchemaFingerprint = s.Schema
	return out, nil
}

func childFieldLabel(c Channel, target string, certificate bool) string {
	if certificate {
		switch target {
		case "v3_merchant_key":
			return "商户私钥 PEM"
		case "sslcert_path":
			return "退款商户证书 PEM"
		case "sslkey_path":
			return "退款商户私钥 PEM"
		case "app_cert_path":
			return "应用证书 PEM"
		case "alipay_cert_path":
			return "支付宝公钥证书 PEM"
		case "root_cert_path":
			return "支付宝根证书 PEM"
		}
	}
	switch target {
	case "appid":
		if c.Plugin == "wxpay" {
			return "微信应用 ID"
		}
		return "支付宝应用 ID"
	case "appmchid":
		if c.Plugin == "wxpay" {
			return "微信商户号"
		}
		return "支付宝收款商户 ID"
	case "appkey":
		if c.Plugin == "wxpay" {
			return "APIv2 密钥"
		}
		return "支付宝公钥"
	case "appsecret":
		return "应用私钥"
	case "oauth_appsecret":
		return "网页授权应用密钥"
	case "mini_appid":
		return "小程序应用 ID"
	case "mini_secret":
		return "小程序应用密钥"
	case "v3_merchant_serial":
		return "商户证书序列号"
	case "v3_api_key":
		return "APIv3 密钥"
	}
	return target
}
