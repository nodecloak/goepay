package channelconfig

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/platform/httpclient"
	"io"
	"strconv"
)

// ApplicationAuthorizationProfileSpec is an independently approved ISV
// application in the explicitly encrypted import. It does not name a payment
// channel or imply any payment product entitlement. Enabled defaults false.
type ApplicationAuthorizationProfileSpec struct {
	ID                int64                                 `json:"id,string"`
	Revision          int64                                 `json:"revision,string"`
	Enabled           bool                                  `json:"enabled"`
	IssuerAppID       string                                `json:"issuer_app_id"`
	ExpectedAuthAppID string                                `json:"expected_auth_app_id"`
	LaunchKind        string                                `json:"launch_kind"`
	AppTypes          []provider.AlipayAuthorizationAppType `json:"app_types,omitempty"`
	MerchantIDs       IdentityMerchantIDs                   `json:"merchant_ids"`
	Config            map[string]string                     `json:"config"`
	Certificates      map[string]string                     `json:"certificates,omitempty"`
}

// Runtime JSON is safe application/launch metadata only. The adapter and full
// allowance/fingerprint must remain on the server, separate from user identity.
type ApplicationAuthorizationProfile struct {
	ID                int64                                     `json:"id,string"`
	Revision          int64                                     `json:"revision,string"`
	IssuerAppID       string                                    `json:"issuer_app_id"`
	ExpectedAuthAppID string                                    `json:"expected_auth_app_id"`
	LaunchKind        string                                    `json:"launch_kind"`
	AppTypes          []provider.AlipayAuthorizationAppType     `json:"app_types,omitempty"`
	MerchantIDs       []int64                                   `json:"-"`
	Fingerprint       string                                    `json:"-"`
	Adapter           provider.ApplicationAuthorizationProvider `json:"-"`
}

func (p ApplicationAuthorizationProfile) AllowsMerchant(mid int64) bool {
	if mid <= 0 {
		return false
	}
	for _, id := range p.MerchantIDs {
		if id == mid {
			return true
		}
	}
	return false
}

func applicationAuthorizationAppID(app string) bool {
	if len(app) != 16 {
		return false
	}
	for _, r := range app {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func applicationAuthorizationAppTypes(kind string, types []provider.AlipayAuthorizationAppType) bool {
	if kind == "basic" {
		return len(types) == 0
	}
	if kind != "assigned" || len(types) < 1 || len(types) > 5 {
		return false
	}
	seen := map[provider.AlipayAuthorizationAppType]bool{}
	for _, kind := range types {
		switch kind {
		case provider.AlipayAuthorizationMobileApp, provider.AlipayAuthorizationWebApp, provider.AlipayAuthorizationPublicApp, provider.AlipayAuthorizationTinyApp, provider.AlipayAuthorizationBaseApp:
		default:
			return false
		}
		if seen[kind] {
			return false
		}
		seen[kind] = true
	}
	return true
}

// BuildApplicationAuthorizationProfiles performs no I/O and requires no payment
// channel/product or financial-write permission. A disabled global gate returns
// an empty capability set without inspecting disabled configuration.
func BuildApplicationAuthorizationProfiles(imp Import, enabled bool) (map[int64]ApplicationAuthorizationProfile, error) {
	out := map[int64]ApplicationAuthorizationProfile{}
	if !enabled {
		return out, nil
	}
	if imp.Version != 1 || len(imp.ApplicationAuthorizationProfiles) > 256 {
		return nil, ErrImport
	}
	seen := map[int64]bool{}
	for _, spec := range imp.ApplicationAuthorizationProfiles {
		if spec.ID <= 0 || seen[spec.ID] {
			return nil, ErrImport
		}
		seen[spec.ID] = true
		if !spec.Enabled {
			continue
		}
		if spec.Revision <= 0 || !applicationAuthorizationAppID(spec.IssuerAppID) || !applicationAuthorizationAppID(spec.ExpectedAuthAppID) || !applicationAuthorizationAppTypes(spec.LaunchKind, spec.AppTypes) || len(spec.MerchantIDs) < 1 || len(spec.MerchantIDs) > 1000 {
			return nil, ErrImport
		}
		allowance := map[int64]bool{}
		for _, id := range spec.MerchantIDs {
			if id <= 0 || allowance[id] {
				return nil, ErrImport
			}
			allowance[id] = true
		}
		for key := range spec.Config {
			if key != "appsecret" && key != "appkey" && key != "sign_type" {
				return nil, ErrImport
			}
		}
		for key := range spec.Certificates {
			if key != "app_cert_path" && key != "alipay_cert_path" && key != "root_cert_path" {
				return nil, ErrImport
			}
		}
		raw, err := json.Marshal(spec)
		if err != nil || len(raw) > 65536 {
			return nil, ErrImport
		}
		private, err := compat.ParsePrivateKey(spec.Config["appsecret"])
		if err != nil {
			return nil, ErrImport
		}
		var public *rsa.PublicKey
		var certificates *provider.AlipayCertificateConfig
		switch spec.Config["sign_type"] {
		case "", "0":
			if len(spec.Certificates) != 0 {
				return nil, ErrImport
			}
			public, err = compat.ParsePublicKey(spec.Config["appkey"])
			if err != nil {
				return nil, ErrImport
			}
		case "1":
			// Imported *_path values are already decrypted PEM content. Never
			// interpret them as filenames or discover files from these values.
			certificates = &provider.AlipayCertificateConfig{ApplicationPEM: spec.Certificates["app_cert_path"], PlatformPEM: spec.Certificates["alipay_cert_path"], RootPEM: spec.Certificates["root_cert_path"]}
		default:
			return nil, ErrImport
		}
		adapter, err := provider.NewAlipayApplicationAuthorization(provider.AlipayConfig{AppID: spec.IssuerAppID, PrivateKey: private, PublicKey: public, CertificateMode: certificates, HTTPClient: httpclient.New()})
		if err != nil {
			return nil, ErrImport
		}
		hash := sha256.Sum256(raw)
		out[spec.ID] = ApplicationAuthorizationProfile{ID: spec.ID, Revision: spec.Revision, IssuerAppID: spec.IssuerAppID, ExpectedAuthAppID: spec.ExpectedAuthAppID, LaunchKind: spec.LaunchKind, AppTypes: append([]provider.AlipayAuthorizationAppType(nil), spec.AppTypes...), MerchantIDs: append([]int64(nil), spec.MerchantIDs...), Fingerprint: hex.EncodeToString(hash[:]), Adapter: adapter}
	}
	return out, nil
}

// Decode canonical IDs, exact field names, and duplicate-free string maps
// before typed decoding. Nulls must not silently select defaults or overwrite a
// prior credential. This decoder does not change legacy profile semantics.
func (p *ApplicationAuthorizationProfileSpec) UnmarshalJSON(raw []byte) error {
	if len(raw) > 65536 {
		return ErrImport
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return ErrImport
	}
	seen := map[string]bool{}
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return ErrImport
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			return ErrImport
		}
		switch name {
		case "id", "revision":
			var text string
			if json.Unmarshal(value, &text) != nil {
				return ErrImport
			}
			id, err := strconv.ParseInt(text, 10, 64)
			if err != nil || id <= 0 || strconv.FormatInt(id, 10) != text {
				return ErrImport
			}
		case "config", "certificates":
			if !identityProfileStringMap(value) {
				return ErrImport
			}
		case "app_types":
			var values []json.RawMessage
			if json.Unmarshal(value, &values) != nil || len(values) > 5 {
				return ErrImport
			}
			for _, kind := range values {
				var text string
				if bytes.Equal(kind, []byte("null")) || json.Unmarshal(kind, &text) != nil {
					return ErrImport
				}
			}
		case "enabled", "issuer_app_id", "expected_auth_app_id", "launch_kind", "merchant_ids":
		default:
			return ErrImport
		}
	}
	if _, err = d.Token(); err != nil {
		return ErrImport
	}
	if _, err = d.Token(); err != io.EOF {
		return ErrImport
	}
	type plain ApplicationAuthorizationProfileSpec
	var decoded plain
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&decoded) != nil {
		return ErrImport
	}
	*p = ApplicationAuthorizationProfileSpec(decoded)
	return nil
}
