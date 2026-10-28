package channelconfig

import (
	"crypto/sha256"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"sort"
)

func (f *ParentChannelFactory) OriginID() int64 {
	if f == nil {
		return 0
	}
	return f.origin.ID
}
func (f *ParentChannelFactory) PayType() string {
	if f == nil {
		return ""
	}
	return ChannelPayType(f.origin)
}
func (f *ParentChannelFactory) BootstrapSnapshot(id int64) (adapter.ParentChannelExecution, []byte, error) {
	return f.BuildSnapshot(id, f.Bootstrap())
}
func (f *ParentChannelFactory) decodeParent(id int64, raw []byte) (Channel, error) {
	in, err := f.decodeParentWithBindings(id, raw)
	return in.Channel, err
}
func applyParentEdits(target map[string]string, edits map[string]adapter.ParentChannelValueEdit) error {
	for key, edit := range edits {
		switch edit.Operation {
		case "keep":
			if edit.Value != nil {
				return ErrImport
			}
		case "clear":
			if edit.Value != nil {
				return ErrImport
			}
			delete(target, key)
		case "replace":
			if edit.Value == nil {
				return ErrImport
			}
			target[key] = *edit.Value
		default:
			return ErrImport
		}
	}
	return nil
}
func (f *ParentChannelFactory) editParent(id int64, c Channel, in adapter.ParentChannelAccountChange, original ...*parentWXBindings) (adapter.ParentChannelExecution, []byte, error) {
	for key := range in.Config {
		if !parentConfigKeys(c.Plugin)[key] {
			return adapter.ParentChannelExecution{}, nil, ErrImport
		}
	}
	for key := range in.Certificates {
		if !parentCertificateKey(c.Plugin, key) {
			return adapter.ParentChannelExecution{}, nil, ErrImport
		}
	}
	if c.Config == nil {
		c.Config = map[string]string{}
	}
	if c.Certificates == nil {
		c.Certificates = map[string]string{}
	}
	if in.AppTypes != nil {
		c.AppTypes = append([]string{}, (*in.AppTypes)...)
	}
	if in.Mode != nil {
		mode := *in.Mode
		c.Mode = &mode
	}
	if e := applyParentEdits(c.Config, in.Config); e != nil {
		return adapter.ParentChannelExecution{}, nil, e
	}
	if e := applyParentEdits(c.Certificates, in.Certificates); e != nil {
		return adapter.ParentChannelExecution{}, nil, e
	}
	var bindings *parentWXBindings
	if len(original) > 1 {
		return adapter.ParentChannelExecution{}, nil, ErrImport
	}
	if len(original) == 1 {
		bindings = original[0]
	}
	bindings, err := f.editWXBindings(c, bindings, in)
	if err != nil {
		return adapter.ParentChannelExecution{}, nil, err
	}
	return f.buildParentSnapshot(id, c, bindings)
}
func (f *ParentChannelFactory) NewSnapshot(id int64, in adapter.ParentChannelAccountChange) (adapter.ParentChannelExecution, []byte, error) {
	if f == nil {
		return adapter.ParentChannelExecution{}, nil, ErrImport
	}
	c := Channel{ID: id, Plugin: f.origin.Plugin, Config: map[string]string{}, Certificates: map[string]string{}}
	for _, key := range []string{"wechat_api_version", "sign_type", "pay_type"} {
		if value := f.origin.Config[key]; value != "" {
			c.Config[key] = value
		}
	}
	return f.editParent(id, c, in)
}
func (f *ParentChannelFactory) ChangeSnapshot(id int64, raw []byte, in adapter.ParentChannelAccountChange) (adapter.ParentChannelExecution, []byte, error) {
	snapshot, e := f.decodeParentWithBindings(id, raw)
	if e != nil {
		return adapter.ParentChannelExecution{}, nil, e
	}
	return f.editParent(id, snapshot.Channel, in, snapshot.Bindings)
}
func (f *ParentChannelFactory) CopySnapshot(from, id int64, raw []byte, in adapter.ParentChannelAccountChange) (adapter.ParentChannelExecution, []byte, error) {
	snapshot, e := f.decodeParentWithBindings(from, raw)
	if e != nil || id <= 0 || from == id {
		return adapter.ParentChannelExecution{}, nil, ErrImport
	}
	return f.editParent(id, snapshot.Channel, in, snapshot.Bindings)
}
func (f *ParentChannelFactory) Describe(id int64, raw []byte) (adapter.ParentChannelAccountView, error) {
	snapshot, e := f.decodeParentWithBindings(id, raw)
	if e != nil {
		return adapter.ParentChannelAccountView{}, e
	}
	c := snapshot.Channel
	out := adapter.ParentChannelAccountView{Plugin: c.Plugin, AppTypes: append([]string{}, c.AppTypes...), Fields: []adapter.ParentChannelFieldState{}}
	if snapshot.Bindings != nil {
		if snapshot.Bindings.Web != nil {
			out.AppWXMP = snapshot.Bindings.Web.ID
		}
		if snapshot.Bindings.Mini != nil {
			out.AppWXA = snapshot.Bindings.Mini.ID
		}
		if snapshot.Bindings.Web != nil || snapshot.Bindings.Mini != nil {
			out.WeixinBindingState = &adapter.ParentWeixinBindingState{Web: f.wxBindingSummary(snapshot.Bindings.Web), Mini: f.wxBindingSummary(snapshot.Bindings.Mini)}
		}
	}
	if c.Mode != nil {
		out.Mode = *c.Mode
	}
	fields := f.Fields()
	known := map[string]bool{}
	for _, field := range fields {
		known[field.Namespace+"/"+field.Name] = true
	}
	for key := range c.Certificates {
		if !known["certificates/"+key] {
			fields = append(fields, adapter.ParentChannelField{Name: key, Namespace: "certificates", PEM: true})
		}
	}
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Namespace != fields[j].Namespace {
			return fields[i].Namespace < fields[j].Namespace
		}
		return fields[i].Name < fields[j].Name
	})
	for _, field := range fields {
		value := c.Config[field.Name]
		if field.Namespace == "certificates" {
			value = c.Certificates[field.Name]
		}
		state := adapter.ParentChannelFieldState{ParentChannelField: field, Present: value != ""}
		if !field.Secret {
			state.Value = value
		}
		out.Fields = append(out.Fields, state)
	}
	return out, nil
}

var _ adapter.ParentAccountFactory = (*ParentChannelFactory)(nil)

func (f *ParentChannelFactory) TemplateCommitment() []byte {
	if f == nil {
		return nil
	}
	raw, _ := json.Marshal(f.origin)
	if len(f.profileCatalog) > 0 {
		raw, _ = json.Marshal(struct {
			Origin   Channel
			Bindings string
		}{f.origin, f.ParentWeixinCatalog().Fingerprint})
	}
	h := sha256.Sum256(raw)
	return h[:]
}
