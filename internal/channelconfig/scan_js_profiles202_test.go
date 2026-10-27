package channelconfig

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScanJSProfiles202SelectsOnlyExplicitWechatWebAndKeepsOAuthGateIndependent(t *testing.T) {
	imp := Import{Version: 1, Channels: []Channel{{ID: 7, Plugin: "wxpay", Config: map[string]string{"account_mode": "direct"}}}, IdentityProfiles: []IdentityProfileSpec{
		{ID: 202, Revision: 1, ChannelID: 7, Enabled: true, Provider: "wxpay", Kind: "web", AppID: "wx-native202", AccountMode: "direct", MerchantIDs: IdentityMerchantIDs{1}, Config: map[string]string{"appsecret": "synthetic-secret202"}},
		{ID: 203, Revision: 1, ChannelID: 99, Enabled: true, Provider: "alipay", Kind: "web", Config: map[string]string{"unusable-while-identity-disabled": "must-remain-unused"}},
		{ID: 204, Revision: 1, ChannelID: 99, Enabled: true, Provider: "wxpay", Kind: "mini"},
		{ID: 205, Revision: 1, ChannelID: 99, Enabled: false, Provider: "wxpay", Kind: "web"},
	}}
	profiles, err := BuildScanJSProfiles202(imp)
	if err != nil || len(profiles) != 1 || profiles[202].Signer == nil || profiles[202].Profile.ID != 202 || !profiles[202].Profile.AllowsMerchant(1) || profiles[202].Profile.AllowsMerchant(2) {
		t.Fatal("native scan selected wrong scope or consumed unrelated disabled-gate profiles", len(profiles), err)
	}
	if other, err := BuildIdentityProfiles(imp, false); err != nil || len(other) != 0 {
		t.Fatal("native builder changed identity default-off semantics", err)
	}
	raw, _ := json.Marshal(profiles)
	if strings.Contains(string(raw), "synthetic-secret202") {
		t.Fatal("runtime scan signer serialized secret")
	}
	before := profiles[202].Profile.Fingerprint
	imp.IdentityProfiles[0].Config["appsecret"] = "rotated-secret202"
	rotated, err := BuildScanJSProfiles202(imp)
	if err != nil || rotated[202].Profile.Fingerprint == before {
		t.Fatal("credential rotation did not invalidate current native binding", err)
	}
}
