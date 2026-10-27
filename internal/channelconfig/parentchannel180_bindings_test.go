package channelconfig

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	adapter "goepay/internal/adapters/payment"
	"strings"
	"testing"
)

func TestParentChannel180UnknownWeixinBindingCannotDisappearIntoSnapshot(t *testing.T) {
	origin := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1", "2"}, Config: map[string]string{"appid": "base-app", "appmchid": "base-merchant", "appkey": "0123456789abcdef0123456789abcdef"}}
	f, err := NewParentChannelFactory(origin, ParentBuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := f.BootstrapSnapshot(7)
	if err != nil {
		t.Fatal(err)
	}
	id := int64(88)
	if _, _, err = f.ChangeSnapshot(7, raw, adapter.ParentChannelAccountChange{AppWXMP: &id}); err == nil {
		t.Fatal("unregistered profile silently accepted as successful binding")
	}
	if _, _, err = f.ChangeSnapshot(7, raw, adapter.ParentChannelAccountChange{AppWXA: &id}); err == nil {
		t.Fatal("unregistered mini profile silently accepted as successful binding")
	}
}

func TestParentChannel180V3MiniBindingUsesRealMiniMetadata(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	platform, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&platform.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	origin := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1", "2"}, Config: map[string]string{"wechat_api_version": "v3", "appid": "base-app", "appmchid": "base-merchant", "v3_merchant_serial": "MERCHANT_180", "v3_api_key": "0123456789abcdef0123456789abcdef"}, Certificates: map[string]string{"v3_merchant_key": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(private)})), "v3_verifier_PLATFORM_180": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}))}}
	mini := IdentityProfileSpec{ID: 82, Revision: 1, ChannelID: 7, Enabled: true, Provider: "wxpay", Kind: "mini", AppID: "mini-app-A", AccountMode: "direct", Path: "pages/pay", MerchantIDs: IdentityMerchantIDs{1}, Config: map[string]string{"appsecret": "synthetic-mini-secret-A"}}
	factory, err := NewParentChannelFactory(origin, ParentBuildOptions{BuildOptions: BuildOptions{AllowLive: true}, IdentityProfiles: []IdentityProfileSpec{mini}, OAuthEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := factory.BootstrapSnapshot(7)
	if err != nil {
		t.Fatal(err)
	}
	execution, _, err := factory.ChangeSnapshot(7, raw, adapter.ParentChannelAccountChange{AppWXA: &mini.ID, ExpectedBindings: factory.ParentWeixinCatalog().Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	selected, variant, err := execution.PaymentSelection("applet", true, 1)
	if err != nil || variant != "mini" {
		t.Fatal(err)
	}
	launcher, ok := selected.(adapter.MiniLaunchProvider)
	if !ok {
		t.Fatal("V3 mini binding lacks real launch adapter")
	}
	metadata, ok := launcher.MiniProgram()
	if !ok || metadata.AppID != mini.AppID || metadata.Path != mini.Path {
		t.Fatal("V3 mini binding lost selected app page", metadata)
	}
	material, err := launcher.MiniLaunch(adapter.MiniLaunchRequest{TradeNo: "42", Amount: 100, CompleteURL: "https://pay.example.invalid/oauth/mini/complete/wxpay/7/42/?state=" + strings.Repeat("a", 64)})
	if err != nil || material.MaterialType != "wxapp" || !strings.Contains(material.Material, mini.AppID) {
		t.Fatal(material, err)
	}
}

func TestParentChannel180MiniAssociationDoesNotEnableUnselectedJSAPIProduct(t *testing.T) {
	origin := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "base-app", "appmchid": "base-merchant", "appkey": "0123456789abcdef0123456789abcdef"}}
	mini := IdentityProfileSpec{ID: 82, Revision: 1, ChannelID: 7, Enabled: true, Provider: "wxpay", Kind: "mini", AppID: "mini-app-A", AccountMode: "direct", Path: "pages/pay", MerchantIDs: IdentityMerchantIDs{1}, Config: map[string]string{"appsecret": "synthetic-mini-secret-A"}}
	factory, err := NewParentChannelFactory(origin, ParentBuildOptions{IdentityProfiles: []IdentityProfileSpec{mini}})
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := factory.BootstrapSnapshot(7)
	if err != nil {
		t.Fatal(err)
	}
	bound, next, err := factory.ChangeSnapshot(7, raw, adapter.ParentChannelAccountChange{AppWXA: &mini.ID, ExpectedBindings: factory.ParentWeixinCatalog().Fingerprint})
	if err != nil {
		t.Fatal("association should be savable before enabling JSAPI", err)
	}
	selected, _, err := bound.PaymentSelection("applet", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range selected.Capabilities().Methods {
		if method == "jsapi" || method == "applet" {
			t.Fatal("association enabled unselected payment product")
		}
	}
	products := []string{"1", "2"}
	enabled, _, err := factory.ChangeSnapshot(7, next, adapter.ParentChannelAccountChange{AppTypes: &products})
	if err != nil {
		t.Fatal(err)
	}
	selected, _, err = enabled.PaymentSelection("applet", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	metadata, ok := selected.(adapter.MiniLaunchProvider).MiniProgram()
	if !ok || metadata.AppID != mini.AppID {
		t.Fatal("later product edit lost existing association")
	}
}

func TestParentChannel180BindingsFreezeRealAppIDsAndPrivateProfiles(t *testing.T) {
	origin := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1", "2"}, Config: map[string]string{"appid": "base-app", "appmchid": "base-merchant", "appkey": "0123456789abcdef0123456789abcdef"}}
	web := IdentityProfileSpec{ID: 81, Revision: 1, ChannelID: 7, Enabled: true, Provider: "wxpay", Kind: "web", AppID: "web-app-A", AccountMode: "direct", MerchantIDs: IdentityMerchantIDs{1}, Config: map[string]string{"appsecret": "synthetic-web-secret-A"}}
	mini := IdentityProfileSpec{ID: 82, Revision: 1, ChannelID: 7, Enabled: true, Provider: "wxpay", Kind: "mini", AppID: "mini-app-A", AccountMode: "direct", Path: "pages/pay", MerchantIDs: IdentityMerchantIDs{1}, Config: map[string]string{"appsecret": "synthetic-mini-secret-A"}}
	options := ParentBuildOptions{IdentityProfiles: []IdentityProfileSpec{web, mini}, OAuthEnabled: true}
	f, err := NewParentChannelFactory(origin, options)
	if err != nil {
		t.Fatal(err)
	}
	_, oldRaw, err := f.BootstrapSnapshot(7)
	if err != nil {
		t.Fatal(err)
	}
	legacy, _ := json.Marshal(struct {
		Version int     `json:"version"`
		Channel Channel `json:"channel"`
	}{1, f.Bootstrap()})
	if !bytes.Equal(oldRaw, legacy) {
		t.Fatal("unbound snapshot encoding changed")
	}
	execution, raw, err := f.ChangeSnapshot(7, oldRaw, adapter.ParentChannelAccountChange{AppWXMP: &web.ID, AppWXA: &mini.ID, ExpectedBindings: f.ParentWeixinCatalog().Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method       string
		mini         bool
		app, variant string
	}{{"native", false, "base-app", ""}, {"jsapi", false, "web-app-A", "web"}, {"jsapi", true, "mini-app-A", "mini"}, {"applet", true, "mini-app-A", "mini"}} {
		p, variant, err := execution.PaymentSelection(tc.method, tc.mini, 1)
		if err != nil {
			t.Fatal(err)
		}
		if p.(adapter.ApplicationIdentity).ApplicationID() != tc.app || variant != tc.variant {
			t.Fatal("actual SDK not selected", tc, variant)
		}
	}
	if _, _, err := execution.PaymentSelection("jsapi", false, 2); err == nil {
		t.Fatal("profile merchant allowlist expanded")
	}
	if execution.Web == nil || execution.Web.ApplicationID() != web.AppID || execution.Mini == nil || execution.Mini.ApplicationID() != mini.AppID {
		t.Fatal("identity SDK not tied to same frozen applications")
	}
	view, err := f.Describe(7, raw)
	if err != nil || view.AppWXMP != web.ID || view.AppWXA != mini.ID {
		t.Fatal("original selectors absent", err)
	}
	public, _ := json.Marshal(view)
	catalog, _ := json.Marshal(f.ParentWeixinCatalog())
	for _, secret := range []string{"synthetic-web-secret-A", "synthetic-mini-secret-A", origin.Config["appkey"]} {
		if strings.Contains(string(public), secret) || strings.Contains(string(catalog), secret) {
			t.Fatal("binding credential escaped public DTO")
		}
	}
	web.Config["appsecret"] = "external-map-mutation"
	web.AppID = "web-app-B"
	web.Revision = 2
	web.MerchantIDs = IdentityMerchantIDs{2}
	rotated, err := NewParentChannelFactory(origin, ParentBuildOptions{IdentityProfiles: []IdentityProfileSpec{web, mini}, OAuthEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := rotated.Restore(7, raw)
	if err != nil {
		t.Fatal(err)
	}
	p, variant, err := restored.PaymentSelection("jsapi", false, 1)
	if err != nil || p.(adapter.ApplicationIdentity).ApplicationID() != "web-app-A" || variant != "web" {
		t.Fatal("old query SDK drifted to current profile", err)
	}
	if restored.PaymentAllowed("web", 1) {
		t.Fatal("new writes ignored current merchant removal")
	}
	startup, err := PrepareParentStartup(Import{Version: 1, Channels: []Channel{origin}, IdentityProfiles: []IdentityProfileSpec{web, mini}}, []adapter.ParentStartupAccount{{ChannelID: 7, SourceID: 7, PayType: "wxpay", Snapshot: raw}})
	if err != nil {
		t.Fatal("bound snapshot cannot restart", err)
	}
	factories, err := startup.BuildFactories(ParentBuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factories[7].Restore(7, raw); err != nil {
		t.Fatal("restarted factory lost frozen bindings", err)
	}
}
