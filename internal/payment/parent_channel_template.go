package payment

import (
	"encoding/hex"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"sort"
)

type ParentChannelTemplate struct {
	source      int64
	kind        string
	factory     adapter.ParentAccountFactory
	binding     RuntimeChannel
	merchants   []int64
	fingerprint string
}
type ParentChannelTemplateView struct {
	SourceChannelID int64                        `json:"source_channel_id,string"`
	Name            string                       `json:"name"`
	PayType         string                       `json:"pay_type"`
	Fields          []adapter.ParentChannelField `json:"fields"`
	Fingerprint     string                       `json:"fingerprint"`
	MerchantCount   int                          `json:"merchant_count"`
}

func (t ParentChannelTemplate) Public() ParentChannelTemplateView {
	return ParentChannelTemplateView{SourceChannelID: t.source, Name: fmt.Sprintf("原授权通道 #%d", t.source), PayType: t.kind, Fields: t.factory.Fields(), Fingerprint: t.fingerprint, MerchantCount: len(t.merchants)}
}

// Runtime source and the operator allowlist are supplied by trusted server
// wiring. Request JSON cannot construct the private template facts.
func (g ProviderFlow) ParentChannelTemplate(id int64, allowlist map[int64][]int64) (ParentChannelTemplate, error) {
	var t ParentChannelTemplate
	factory := g.ParentFactories[id]
	p := g.Providers[id]
	if g.Store == nil || id <= 0 || factory == nil || p == nil && !parentRestoreOnlyFactory(factory) || factory.OriginID() != id || p != nil && factory.PayType() != p.ID() {
		return t, ErrInvalid
	}
	var binding RuntimeChannel
	if p != nil {
		var e error
		binding, e = g.RuntimeChannel(id)
		if e != nil {
			return t, e
		}
	}
	t = ParentChannelTemplate{source: id, kind: factory.PayType(), factory: factory, binding: binding, merchants: []int64{}}
	for mid, ids := range allowlist {
		if mid <= 0 {
			continue
		}
		for _, source := range ids {
			if source == id {
				t.merchants = append(t.merchants, mid)
				break
			}
		}
	}
	sort.Slice(t.merchants, func(i, j int) bool { return t.merchants[i] < t.merchants[j] })
	if len(t.merchants) > 1000 || len(factory.TemplateCommitment()) != 32 {
		return ParentChannelTemplate{}, ErrInvalid
	}
	hash, e := g.Store.referralDigest("parentchannel180/template", []any{t.source, t.kind, binding.identity, factory.TemplateCommitment(), t.merchants, factory.Fields()})
	if e != nil {
		return ParentChannelTemplate{}, e
	}
	t.fingerprint = hex.EncodeToString(hash)
	return t, nil
}
