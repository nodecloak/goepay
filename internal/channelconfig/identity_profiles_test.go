package channelconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func miniLinkIdentityImport() Import {
	return Import{Version: 1, Channels: []Channel{{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "pay-app", "appmchid": "mch", "appkey": strings.Repeat("k", 32)}}}, IdentityProfiles: []IdentityProfileSpec{{ID: 31, Revision: 2, ChannelID: 7, Enabled: true, Provider: "wxpay", Kind: "mini", AppID: "identity-app", AccountMode: "direct", Path: "pages/identity/index", MerchantIDs: IdentityMerchantIDs{1000}, Config: map[string]string{"appsecret": "synthetic-mini-secret"}}}}
}

// A literal pre-increment representation catches a false flag changing old
// encrypted-spec bytes/fingerprints or adding fields to the old runtime DTO.
func TestIdentityProfilesMiniURLLinkDefaultOffPreservesExistingFingerprint(t *testing.T) {
	imp := miniLinkIdentityImport()
	profiles, err := BuildIdentityProfiles(imp, true)
	if err != nil {
		t.Fatal(err)
	}
	p := profiles[31]
	before := `{"Profile":{"id":"31","revision":"2","channel_id":"7","enabled":true,"provider":"wxpay","kind":"mini","app_id":"identity-app","account_mode":"direct","path":"pages/identity/index","merchant_ids":["1000"],"config":{"appsecret":"synthetic-mini-secret"}},"Channel":{"id":7,"plugin":"wxpay","app_types":["1"],"config":{"appid":"pay-app","appkey":"kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk","appmchid":"mch"},"certificates":null}}`
	wantHash := sha256.Sum256([]byte(before))
	if p.Fingerprint != hex.EncodeToString(wantHash[:]) || p.MiniLink != nil || p.URLLinkAvailable || p.Mini == nil || p.Mini.ApplicationID() != "identity-app" {
		t.Fatal("default-off flag changed old fingerprint or mini capability")
	}
	raw, err := json.Marshal(p)
	if err != nil || string(raw) != `{"id":"31","revision":"2","channel_id":"7","provider":"wxpay","kind":"mini","app_id":"identity-app","path":"pages/identity/index"}` {
		t.Fatal("default-off runtime JSON changed", err)
	}
	raw, err = json.Marshal(imp.IdentityProfiles[0])
	if err != nil || strings.Contains(string(raw), "enable_url_link") {
		t.Fatal("false spec flag was serialized")
	}
	var explicitFalse IdentityProfileSpec
	if json.Unmarshal([]byte(strings.TrimSuffix(string(raw), "}")+`,"enable_url_link":false}`), &explicitFalse) != nil || explicitFalse.EnableURLLink {
		t.Fatal("explicit false flag rejected")
	}
	imp.IdentityProfiles[0] = explicitFalse
	profiles, err = BuildIdentityProfiles(imp, true)
	if err != nil || profiles[31].Fingerprint != p.Fingerprint {
		t.Fatal("explicit false differs from omitted flag", err)
	}
}

func TestIdentityProfilesMiniURLLinkExplicitIndependentClientAndPrivateJSON(t *testing.T) {
	imp := miniLinkIdentityImport()
	imp.IdentityProfiles[0].EnableURLLink = true
	profiles, err := BuildIdentityProfiles(imp, false)
	if err != nil || len(profiles) != 0 {
		t.Fatal("total identity gate ignored", err)
	}
	profiles, err = BuildIdentityProfiles(imp, true)
	if err != nil {
		t.Fatal(err)
	}
	p := profiles[31]
	if !p.URLLinkAvailable || p.MiniLink == nil || p.MiniLink.ID() != "wxpay" || p.MiniLink.ApplicationID() != imp.IdentityProfiles[0].AppID || p.Mini == nil || p.Mini.ApplicationID() != imp.IdentityProfiles[0].AppID || p.Web != nil {
		t.Fatal("URL Link replaced mini SDK exchange or inherited payment application")
	}
	raw, err := json.Marshal(p)
	if err != nil || !strings.Contains(string(raw), `"url_link_available":true`) || strings.Contains(string(raw), "synthetic-mini-secret") || strings.Contains(string(raw), p.Fingerprint) || strings.Contains(string(raw), "MiniLink") || strings.Contains(string(raw), "merchant_ids") {
		t.Fatal("runtime profile omitted capability or serialized private material")
	}
	imp.IdentityProfiles[0].Enabled = false
	profiles, err = BuildIdentityProfiles(imp, true)
	if err != nil || len(profiles) != 0 {
		t.Fatal("disabled profile installed link client", err)
	}
}

func TestIdentityProfilesMiniURLLinkFlagAndApprovedDimensionsBindFingerprint(t *testing.T) {
	imp := miniLinkIdentityImport()
	off, err := BuildIdentityProfiles(imp, true)
	if err != nil {
		t.Fatal(err)
	}
	imp.IdentityProfiles[0].EnableURLLink = true
	on, err := BuildIdentityProfiles(imp, true)
	if err != nil {
		t.Fatal(err)
	}
	if on[31].Fingerprint == off[31].Fingerprint {
		t.Fatal("link flag omitted from profile binding")
	}
	for name, change := range map[string]func(*IdentityProfileSpec){
		"revision":      func(p *IdentityProfileSpec) { p.Revision++ },
		"secret":        func(p *IdentityProfileSpec) { p.Config = map[string]string{"appsecret": "rotated-mini-secret"} },
		"identity_path": func(p *IdentityProfileSpec) { p.Path = "pages/identity/other" },
		"app":           func(p *IdentityProfileSpec) { p.AppID = "other-identity-app" },
		"allowance":     func(p *IdentityProfileSpec) { p.MerchantIDs = IdentityMerchantIDs{1000, 1001} },
	} {
		t.Run(name, func(t *testing.T) {
			changed := miniLinkIdentityImport()
			changed.IdentityProfiles[0].EnableURLLink = true
			change(&changed.IdentityProfiles[0])
			after, err := BuildIdentityProfiles(changed, true)
			if err != nil || after[31].Fingerprint == on[31].Fingerprint {
				t.Fatal("link profile dimension omitted from binding", err)
			}
		})
	}
	imp.IdentityProfiles[0].EnableURLLink = false
	again, err := BuildIdentityProfiles(imp, true)
	if err != nil || again[31].Fingerprint != off[31].Fingerprint || again[31].MiniLink != nil || again[31].URLLinkAvailable {
		t.Fatal("revoked link flag retained capability or changed legacy binding", err)
	}
}

func TestIdentityProfilesMiniURLLinkStrictFlagDecoder(t *testing.T) {
	for _, raw := range []string{`{}`, `{"enable_url_link":false}`, `{"enable_url_link":true}`} {
		var spec IdentityProfileSpec
		if json.Unmarshal([]byte(raw), &spec) != nil || spec.EnableURLLink != strings.Contains(raw, ":true") {
			t.Fatal("canonical optional flag rejected")
		}
	}
	for _, raw := range []string{`{"enable_url_link":null}`, `{"enable_url_link":"true"}`, `{"enable_url_link":1}`, `{"enable_url_link":[]}`, `{"enable_url_link":true,"enable_url_link":false}`, `{"ENABLE_URL_LINK":true}`, `{"Enable_Url_Link":true}`, `{"enableURLLink":true}`, `{"enable_url_link":false,"ENABLE_URL_LINK":true}`} {
		var spec IdentityProfileSpec
		if json.Unmarshal([]byte(raw), &spec) == nil {
			t.Fatal("ambiguous URL Link flag accepted", raw)
		}
	}
}

func TestIdentityProfilesMiniURLLinkRejectsUnsupportedProfilesAndInvalidApprovedInputs(t *testing.T) {
	for name, change := range map[string]func(*IdentityProfileSpec){
		"web":            func(p *IdentityProfileSpec) { p.Kind = "web"; p.Path = "" },
		"partner":        func(p *IdentityProfileSpec) { p.AccountMode = "partner" },
		"ecommerce":      func(p *IdentityProfileSpec) { p.AccountMode = "ecommerce" },
		"missing_path":   func(p *IdentityProfileSpec) { p.Path = "" },
		"ambiguous_path": func(p *IdentityProfileSpec) { p.Path = "pages//identity/index" },
		"invalid_app":    func(p *IdentityProfileSpec) { p.AppID = "identity app" },
		"invalid_secret": func(p *IdentityProfileSpec) { p.Config = map[string]string{"appsecret": "secret with space"} },
	} {
		t.Run(name, func(t *testing.T) {
			imp := miniLinkIdentityImport()
			imp.IdentityProfiles[0].EnableURLLink = true
			change(&imp.IdentityProfiles[0])
			if _, err := BuildIdentityProfiles(imp, true); err == nil || strings.Contains(err.Error(), "secret with space") {
				t.Fatal("unsupported or invalid link profile accepted or leaked")
			}
		})
	}
	c := certificateChannel(t)
	for _, kind := range []string{"web", "mini"} {
		path := ""
		if kind == "mini" {
			path = "pages/identity/index"
		}
		imp := Import{Version: 1, Channels: []Channel{c}, IdentityProfiles: []IdentityProfileSpec{{ID: 31, Revision: 2, ChannelID: c.ID, Enabled: true, Provider: "alipay", Kind: kind, AppID: c.Config["appid"], AccountMode: "direct", Path: path, MerchantIDs: IdentityMerchantIDs{1000}, Config: c.Config, Certificates: c.Certificates}}}
		// Use only the explicitly permitted identity credential fields.
		imp.IdentityProfiles[0].Config = map[string]string{"appsecret": c.Config["appsecret"], "sign_type": "1"}
		if _, err := BuildIdentityProfiles(imp, true); err != nil {
			t.Fatal("invalid test fixture", kind, err)
		}
		imp.IdentityProfiles[0].EnableURLLink = true
		if _, err := BuildIdentityProfiles(imp, true); err == nil {
			t.Fatal("Alipay profile installed WeChat URL Link", kind)
		}
	}
}

func TestIdentityProfiles64ExplicitOfflineIndependentAppsAndMode(t *testing.T) {
	imp := Import{Version: 1, Channels: []Channel{{ID: 7, Plugin: "wxpay", Config: map[string]string{"appid": "pay-app", "appmchid": "mch"}}}, IdentityProfiles: []IdentityProfileSpec{{ID: 11, Revision: 1, ChannelID: 7, Enabled: true, Provider: "wxpay", AppID: "separate-mini", Kind: "mini", AccountMode: "direct", Path: "pages/identity/index", MerchantIDs: []int64{1}, Config: map[string]string{"appsecret": "synthetic-secret"}}}}
	p, e := BuildIdentityProfiles(imp, false)
	if e != nil || len(p) != 0 {
		t.Fatal("not default off", e)
	}
	p, e = BuildIdentityProfiles(imp, true)
	if e != nil || p[11].Mini.ApplicationID() != "separate-mini" || !p[11].AllowsMerchant(1) || p[11].AllowsMerchant(2) {
		t.Fatal(p, e)
	}
	raw, _ := json.Marshal(p[11])
	if strings.Contains(string(raw), "synthetic-secret") || strings.Contains(string(raw), p[11].Fingerprint) {
		t.Fatal("private profile material serialized")
	}
	original := p[11].Fingerprint
	imp.IdentityProfiles[0].Config["appsecret"] = "rotated-secret"
	p, e = BuildIdentityProfiles(imp, true)
	if e != nil || p[11].Fingerprint == original {
		t.Fatal("credential rotation not bound", e)
	}
	imp.IdentityProfiles = append(imp.IdentityProfiles, imp.IdentityProfiles[0])
	if _, e = BuildIdentityProfiles(imp, true); e == nil {
		t.Fatal("duplicate profile overwritten")
	}
	imp.IdentityProfiles = imp.IdentityProfiles[:1]
	for _, mode := range []string{"", "partner", "ecommerce"} {
		imp.IdentityProfiles[0].AccountMode = mode
		if _, e = BuildIdentityProfiles(imp, true); e == nil {
			t.Fatal("unsupported role accepted", mode)
		}
	}
}

func TestIdentityProfiles64ImportIdentifiersAreCanonicalStrings(t *testing.T) {
	for _, raw := range []string{`{"id":"01","revision":"1","channel_id":"7","merchant_ids":["1"]}`, `{"id":"1","revision":"1","channel_id":"7","merchant_ids":[1]}`, `{"id":"1","revision":"1","channel_id":"7","merchant_ids":["01"]}`, `{"id":"1","revision":"1","channel_id":"7","id":"2","merchant_ids":["1"]}`, `{"id":"1","revision":"1","channel_id":"7","config":{"appsecret":"one","appsecret":"two"}}`, `{"id":"1","revision":"1","channel_id":"7","certificates":{"app_cert_path":null}}`} {
		var spec IdentityProfileSpec
		if json.Unmarshal([]byte(raw), &spec) == nil {
			t.Fatal("ambiguous profile import", raw)
		}
	}
	raw, e := json.Marshal(IdentityProfileSpec{ID: 11, Revision: 1, ChannelID: 7, MerchantIDs: []int64{1}})
	if e != nil || !strings.Contains(string(raw), `"merchant_ids":["1"]`) {
		t.Fatal("merchant IDs not strings", string(raw), e)
	}
}

func TestIdentityProfiles64AlipayCertificateApplicationIsIndependent(t *testing.T) {
	c := certificateChannel(t)
	for _, kind := range []string{"web", "mini"} {
		path := ""
		if kind == "mini" {
			path = "pages/identity/index"
		}
		spec := IdentityProfileSpec{ID: 11, Revision: 1, ChannelID: c.ID, Enabled: true, Provider: "alipay", Kind: kind, AppID: c.Config["appid"], AccountMode: "direct", Path: path, MerchantIDs: []int64{1}, Config: map[string]string{"appsecret": c.Config["appsecret"], "sign_type": "1"}, Certificates: c.Certificates}
		channel := Channel{ID: c.ID, Plugin: "alipay", Config: map[string]string{"appid": "different-payment-app"}}
		imp := Import{Version: 1, Channels: []Channel{channel}, IdentityProfiles: []IdentityProfileSpec{spec}}
		p, e := BuildIdentityProfiles(imp, true)
		if e != nil {
			t.Fatal(kind, e)
		}
		if kind == "web" && p[11].Web.ApplicationID() != spec.AppID || kind == "mini" && p[11].Mini.ApplicationID() != spec.AppID {
			t.Fatal("profile inherited payment AppID")
		}
		original := p[11].Fingerprint
		imp.Channels[0].Config["appmchid"] = "2088123412341234"
		changed, e := BuildIdentityProfiles(imp, true)
		if e != nil || changed[11].Fingerprint == original {
			t.Fatal("same AppID changed physical Alipay SellerID did not invalidate profile", e)
		}
		imp.IdentityProfiles[0].Certificates = map[string]string{"app_cert_path": c.Certificates["app_cert_path"], "root_cert_path": c.Certificates["root_cert_path"]}
		if _, e = BuildIdentityProfiles(imp, true); e == nil {
			t.Fatal("incomplete trust silently fell back to public key mode")
		}
	}
}

func TestIdentityProfiles64FingerprintBindsEntireTrustedChannel(t *testing.T) {
	imp := Import{Version: 1, Channels: []Channel{{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "payment-app", "appmchid": "mch", "appkey": strings.Repeat("k", 32)}}}, IdentityProfiles: []IdentityProfileSpec{{ID: 11, Revision: 1, ChannelID: 7, Enabled: true, Provider: "wxpay", Kind: "mini", AppID: "identity-app", AccountMode: "direct", Path: "pages/identity/index", MerchantIDs: []int64{1}, Config: map[string]string{"appsecret": "synthetic-secret"}}}}
	p, e := BuildIdentityProfiles(imp, true)
	if e != nil {
		t.Fatal(e)
	}
	original := p[11].Fingerprint
	imp.Channels[0].Config["appkey"] = strings.Repeat("j", 32)
	changed, e := BuildIdentityProfiles(imp, true)
	if e != nil || changed[11].Fingerprint == original {
		t.Fatal("trusted channel credential rotation did not invalidate original profile binding", e)
	}
	raw, _ := json.Marshal(changed[11])
	if strings.Contains(string(raw), strings.Repeat("j", 32)) {
		t.Fatal("channel credential exposed")
	}
}

func TestIdentityProfiles64UnsupportedAccountModesNeverBecomeDirectPayment(t *testing.T) {
	for key, value := range map[string]string{"account_mode": "partner", "sub_mchid": "sub", "sub_appid": "sub-app", "sp_mchid": "service", "sp_appid": "service-app", "ecommerce": "true", "app_auth_token": "synthetic-token"} {
		c := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "wxapp", "appmchid": "mch", "appkey": strings.Repeat("k", 32), key: value}}
		if _, e := Build(Import{Version: 1, Channels: []Channel{c}}, false); e == nil {
			t.Fatal("unsupported account dimension silently downgraded", key)
		}
	}
}
