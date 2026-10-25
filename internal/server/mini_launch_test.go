package server

import (
	"bytes"
	"github.com/jackc/pgx/v5/pgxpool"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"testing"
)

func TestMiniLaunchChannelRequiresMatchingTrustedPayerExchange(t *testing.T) {
	wx, e := provider.NewWechatV2(provider.WechatV2Config{AppID: "wxapp", MerchantID: "mch", APIKey: "0123456789abcdef0123456789abcdef", Methods: []string{"jsapi"}, MiniProgram: &provider.MiniProgramConfig{Path: "pages/pay/pay"}})
	if e != nil {
		t.Fatal(e)
	}
	oauth, e := provider.NewWechatMiniOAuth(provider.WechatOAuthConfig{AppID: "wxapp", AppSecret: "synthetic-mini-secret"})
	if e != nil {
		t.Fatal(e)
	}
	s := &Server{Pool: &pgxpool.Pool{}, Store: &payment.Store{}, CheckoutKey: bytes.Repeat([]byte{1}, 32), MerchantChannels: map[int64][]int64{1: {7}}, MiniOAuthProviders: map[int64]provider.MiniOAuthProvider{7: oauth}, ProviderFlow: &payment.ProviderFlow{AllowLive: true, CallbackBaseURL: "https://pay.example.invalid", Providers: map[int64]provider.Provider{7: wx}}}
	_, metadata, method, ok := s.miniLaunchChannelReady(1, 7)
	if !ok || method != "jsapi" || metadata.AppID != "wxapp" || metadata.Path != "pages/pay/pay" {
		t.Fatal(metadata, method, ok)
	}
	if _, _, _, ok = s.miniLaunchChannelReady(2, 7); ok {
		t.Fatal("merchant channel permission bypassed")
	}
	wrong, _ := provider.NewWechatMiniOAuth(provider.WechatOAuthConfig{AppID: "foreign-app", AppSecret: "synthetic-mini-secret"})
	s.MiniOAuthProviders[7] = wrong
	if _, _, _, ok = s.miniLaunchChannelReady(1, 7); ok {
		t.Fatal("cross application payer accepted")
	}
	s.MiniOAuthProviders[7] = oauth
	for _, setup := range []func(){func() { s.Sandbox = true }, func() { s.ProviderFlow.LocalSimulation = true }, func() { s.ProviderFlow.AllowLive = false }, func() { s.ProviderFlow.CallbackBaseURL = "http://pay.example.invalid" }, func() { s.CheckoutKey = nil }, func() { delete(s.MiniOAuthProviders, 7) }} {
		setup()
		if _, _, _, ok = s.miniLaunchChannelReady(1, 7); ok {
			t.Fatal("disabled mini capability accepted")
		}
		s.Sandbox = false
		s.ProviderFlow.LocalSimulation = false
		s.ProviderFlow.AllowLive = true
		s.ProviderFlow.CallbackBaseURL = "https://pay.example.invalid"
		s.CheckoutKey = bytes.Repeat([]byte{1}, 32)
		s.MiniOAuthProviders[7] = oauth
	}
}
