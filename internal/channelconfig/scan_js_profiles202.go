package channelconfig

import adapter "goepay/internal/adapters/payment"

// Immutable per-import snapshot. Profile is safe runtime metadata; Signer owns
// the secret and credential cache and deliberately has no JSON projection.
type ScanJSProfile202 struct {
	Profile IdentityProfile              `json:"-"`
	Signer  adapter.WechatJSSDKSigner202 `json:"-"`
}

// This builds independently of the OAuth/identity feature flag. Building a
// signer grants no scan authority: the separate signed operator setting must
// select the current profile revision and complete encrypted-source fingerprint.
func BuildScanJSProfiles202(imp Import) (map[int64]ScanJSProfile202, error) {
	filtered := imp
	filtered.IdentityProfiles = nil
	for _, spec := range imp.IdentityProfiles {
		if spec.Enabled && spec.Provider == "wxpay" && spec.Kind == "web" {
			filtered.IdentityProfiles = append(filtered.IdentityProfiles, spec)
		}
	}
	if len(filtered.IdentityProfiles) == 0 {
		return map[int64]ScanJSProfile202{}, nil
	}
	profiles, err := BuildIdentityProfiles(filtered, true)
	if err != nil {
		return nil, err
	}
	out := map[int64]ScanJSProfile202{}
	for _, spec := range filtered.IdentityProfiles {
		p, ok := profiles[spec.ID]
		if !ok || p.Provider != "wxpay" || p.Kind != "web" {
			continue
		}
		signer, err := adapter.NewWechatJSSDK202(adapter.WechatJSSDKConfig202{AppID: p.AppID, AppSecret: spec.Config["appsecret"]})
		if err != nil {
			return nil, ErrImport
		}
		out[p.ID] = ScanJSProfile202{Profile: p, Signer: signer}
	}
	return out, nil
}
