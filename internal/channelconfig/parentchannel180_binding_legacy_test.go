package channelconfig

import (
	adapter "goepay/internal/adapters/payment"
	"testing"
)

func TestParentChannel180BindingOneSidePreservesUntouchedLegacyIdentity(t *testing.T) {
	for _, kind := range []string{"web", "mini"} {
		t.Run(kind, func(t *testing.T) {
			origin := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1", "2"}, Config: map[string]string{"appid": "base-app", "appmchid": "base-merchant", "appkey": "0123456789abcdef0123456789abcdef", "enable_web_oauth": "true", "oauth_appsecret": "synthetic-legacy-web", "enable_mini_launch": "true", "mini_secret": "synthetic-legacy-mini", "mini_path": "pages/legacy"}}
			profile := IdentityProfileSpec{ID: 81, Revision: 1, ChannelID: 7, Enabled: true, Provider: "wxpay", Kind: kind, AppID: "selected-app", AccountMode: "direct", MerchantIDs: IdentityMerchantIDs{1}, Config: map[string]string{"appsecret": "synthetic-selected-secret"}}
			if kind == "mini" {
				profile.Path = "pages/selected"
			}
			factory, err := NewParentChannelFactory(origin, ParentBuildOptions{IdentityProfiles: []IdentityProfileSpec{profile}, OAuthEnabled: true})
			if err != nil {
				t.Fatal(err)
			}
			_, raw, err := factory.BootstrapSnapshot(7)
			if err != nil {
				t.Fatal(err)
			}
			change := adapter.ParentChannelAccountChange{ExpectedBindings: factory.ParentWeixinCatalog().Fingerprint}
			if kind == "web" {
				change.AppWXMP = &profile.ID
			} else {
				change.AppWXA = &profile.ID
			}
			bound, next, err := factory.ChangeSnapshot(7, raw, change)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "web" && (bound.Mini == nil || bound.Mini.ApplicationID() != "base-app") || kind == "mini" && (bound.Web == nil || bound.Web.ApplicationID() != "base-app") {
				t.Fatal("binding one side cleared the untouched legacy identity")
			}
			zero := int64(0)
			if kind == "web" {
				change.AppWXMP = &zero
			} else {
				change.AppWXA = &zero
			}
			cleared, _, err := factory.ChangeSnapshot(7, next, change)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "web" && cleared.Web != nil || kind == "mini" && cleared.Mini != nil {
				t.Fatal("explicit unbind retained the selected identity")
			}
		})
	}
}
