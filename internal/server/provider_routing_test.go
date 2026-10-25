package server

import (
	provider "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"testing"
)

type routingProvider struct {
	provider.Provider
	kind    string
	methods []string
}

func (p routingProvider) ID() string { return p.kind }
func (p routingProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{Methods: p.methods}
}

func TestWebRouteUsesAnExplicitlyEnabledProduct(t *testing.T) {
	for _, tc := range []struct {
		kind, device, want string
		methods            []string
	}{
		{"alipay", "", "native", []string{"native"}},
		{"alipay", "", "wap", []string{"wap"}},
		{"alipay", "", "app", []string{"app"}},
		{"alipay", "mobile", "wap", []string{"page", "wap"}},
		{"alipay", "pc", "page", []string{"page", "wap"}},
		{"alipay", "", "native", []string{"scan", "preauth", "native"}},
		{"alipay", "", "preauth", []string{"scan", "preauth"}},
		{"wxpay", "mobile", "h5", []string{"h5"}},
		{"wxpay", "", "native", []string{"native", "jsapi"}},
	} {
		for _, method := range []string{"", "web", "jump"} {
			s := &Server{ProviderFlow: &payment.ProviderFlow{Providers: map[int64]provider.Provider{7: routingProvider{kind: tc.kind, methods: tc.methods}}}, MerchantChannels: map[int64][]int64{1: {7}}}
			id, got, e := s.providerRoute(1, compat.Params{"type": tc.kind, "method": method, "device": tc.device})
			if e != nil || id != 7 || got != tc.want {
				t.Errorf("%s methods=%v request=%s device=%s route=%s err=%v", tc.kind, tc.methods, method, tc.device, got, e)
			}
		}
	}
	s := &Server{ProviderFlow: &payment.ProviderFlow{Providers: map[int64]provider.Provider{7: routingProvider{kind: "alipay", methods: []string{"scan"}}}}, MerchantChannels: map[int64][]int64{1: {7}}}
	if _, _, e := s.providerRoute(1, compat.Params{"type": "alipay", "method": "web"}); e == nil {
		t.Fatal("default web silently became a scan charge")
	}
}
