package channelconfig

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	provider "goepay/internal/adapters/payment"
	"strings"
	"testing"
)

func applicationAuthorizationSpec(t *testing.T) ApplicationAuthorizationProfileSpec {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return ApplicationAuthorizationProfileSpec{ID: 21, Revision: 1, Enabled: true, IssuerAppID: "2021000000000001", ExpectedAuthAppID: "2021000000000002", LaunchKind: "basic", MerchantIDs: IdentityMerchantIDs{1000}, Config: map[string]string{"appsecret": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), "appkey": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)})), "sign_type": "0"}}
}

// These profiles must work with no payment channel or product at all. A payment
// factory dependency, secret serialization or missing allowance check breaks it.
func TestApplicationAuthorizationProfilesStandaloneOfflineAndPrivate(t *testing.T) {
	spec := applicationAuthorizationSpec(t)
	imp := Import{Version: 1, ApplicationAuthorizationProfiles: []ApplicationAuthorizationProfileSpec{spec}}
	profiles, err := BuildApplicationAuthorizationProfiles(imp, true)
	if err != nil || len(profiles) != 1 {
		t.Fatal("standalone authorization profile rejected", err)
	}
	p := profiles[21]
	if p.ID != 21 || p.Revision != 1 || p.IssuerAppID != spec.IssuerAppID || p.ExpectedAuthAppID != spec.ExpectedAuthAppID || p.LaunchKind != "basic" || p.Adapter == nil || p.Adapter.ApplicationID() != spec.IssuerAppID || !p.AllowsMerchant(1000) || p.AllowsMerchant(0) || p.AllowsMerchant(1001) || len(p.Fingerprint) != 64 {
		t.Fatal("invalid application authorization profile")
	}
	if _, err := p.Adapter.ApplicationAuthorizationURL(strings.Repeat("a", 64), "https://pay.example.invalid/cb/"); err != nil {
		t.Fatal("offline launch construction failed", err)
	}
	raw, err := json.Marshal(p)
	if err != nil || strings.Contains(string(raw), "PRIVATE KEY") || strings.Contains(string(raw), p.Fingerprint) || strings.Contains(string(raw), "merchant_ids") || strings.Contains(string(raw), "Adapter") {
		t.Fatal("private runtime metadata serialized")
	}
	if _, ok := p.Adapter.(provider.Provider); ok {
		t.Fatal("profile exposes payment methods")
	}
	imp.ApplicationAuthorizationProfiles[0].MerchantIDs[0] = 1001
	if !p.AllowsMerchant(1000) || p.AllowsMerchant(1001) {
		t.Fatal("runtime allowlist aliases mutable import")
	}
	profiles, err = BuildApplicationAuthorizationProfiles(Import{Version: 99, ApplicationAuthorizationProfiles: []ApplicationAuthorizationProfileSpec{{ID: -1, Enabled: true}}}, false)
	if err != nil || len(profiles) != 0 {
		t.Fatal("default-off builder validated disabled import", err)
	}
	spec.Enabled = false
	spec.Config = nil
	profiles, err = BuildApplicationAuthorizationProfiles(Import{Version: 1, ApplicationAuthorizationProfiles: []ApplicationAuthorizationProfileSpec{spec}}, true)
	if err != nil || len(profiles) != 0 {
		t.Fatal("disabled profile constructed an adapter", err)
	}
}

func TestApplicationAuthorizationProfileFingerprintBindsEntireSpec(t *testing.T) {
	spec := applicationAuthorizationSpec(t)
	build := func(spec ApplicationAuthorizationProfileSpec) ApplicationAuthorizationProfile {
		profiles, err := BuildApplicationAuthorizationProfiles(Import{Version: 1, ApplicationAuthorizationProfiles: []ApplicationAuthorizationProfileSpec{spec}}, true)
		if err != nil {
			t.Fatal(err)
		}
		return profiles[spec.ID]
	}
	original := build(spec).Fingerprint
	for _, change := range []func(*ApplicationAuthorizationProfileSpec){
		func(p *ApplicationAuthorizationProfileSpec) { p.ID++ },
		func(p *ApplicationAuthorizationProfileSpec) { p.Revision++ },
		func(p *ApplicationAuthorizationProfileSpec) { p.IssuerAppID = "2021000000000003" },
		func(p *ApplicationAuthorizationProfileSpec) { p.ExpectedAuthAppID = "2021000000000004" },
		func(p *ApplicationAuthorizationProfileSpec) { p.MerchantIDs = IdentityMerchantIDs{1000, 1001} },
		func(p *ApplicationAuthorizationProfileSpec) {
			p.LaunchKind = "assigned"
			p.AppTypes = []provider.AlipayAuthorizationAppType{provider.AlipayAuthorizationWebApp}
		},
		func(p *ApplicationAuthorizationProfileSpec) {
			p.Config = map[string]string{"appsecret": spec.Config["appsecret"] + "\n", "appkey": spec.Config["appkey"], "sign_type": "0"}
		},
	} {
		changed := spec
		change(&changed)
		if build(changed).Fingerprint == original {
			t.Fatal("trusted profile dimension omitted from fingerprint")
		}
	}
	assigned := spec
	assigned.LaunchKind = "assigned"
	assigned.AppTypes = []provider.AlipayAuthorizationAppType{provider.AlipayAuthorizationWebApp}
	first := build(assigned)
	assigned.AppTypes = []provider.AlipayAuthorizationAppType{provider.AlipayAuthorizationTinyApp}
	if build(assigned).Fingerprint == first.Fingerprint {
		t.Fatal("assigned app types omitted from fingerprint")
	}
	assigned.AppTypes[0] = provider.AlipayAuthorizationBaseApp
	if first.AppTypes[0] != provider.AlipayAuthorizationWebApp {
		t.Fatal("runtime app types alias import")
	}
}

func TestApplicationAuthorizationProfilesRejectInvalidEnabledSpecs(t *testing.T) {
	spec := applicationAuthorizationSpec(t)
	for name, change := range map[string]func(*ApplicationAuthorizationProfileSpec){
		"id":               func(p *ApplicationAuthorizationProfileSpec) { p.ID = 0 },
		"revision":         func(p *ApplicationAuthorizationProfileSpec) { p.Revision = 0 },
		"issuer":           func(p *ApplicationAuthorizationProfileSpec) { p.IssuerAppID = "app" },
		"missing_expected": func(p *ApplicationAuthorizationProfileSpec) { p.ExpectedAuthAppID = "" },
		"wrong_expected":   func(p *ApplicationAuthorizationProfileSpec) { p.ExpectedAuthAppID = "20210000000000a2" },
		"kind":             func(p *ApplicationAuthorizationProfileSpec) { p.LaunchKind = "oauth" },
		"basic_types": func(p *ApplicationAuthorizationProfileSpec) {
			p.AppTypes = []provider.AlipayAuthorizationAppType{provider.AlipayAuthorizationWebApp}
		},
		"assigned_empty": func(p *ApplicationAuthorizationProfileSpec) { p.LaunchKind = "assigned" },
		"assigned_unknown": func(p *ApplicationAuthorizationProfileSpec) {
			p.LaunchKind = "assigned"
			p.AppTypes = []provider.AlipayAuthorizationAppType{"unknown"}
		},
		"assigned_duplicate": func(p *ApplicationAuthorizationProfileSpec) {
			p.LaunchKind = "assigned"
			p.AppTypes = []provider.AlipayAuthorizationAppType{provider.AlipayAuthorizationWebApp, provider.AlipayAuthorizationWebApp}
		},
		"assigned_limit": func(p *ApplicationAuthorizationProfileSpec) {
			p.LaunchKind = "assigned"
			p.AppTypes = make([]provider.AlipayAuthorizationAppType, 6)
		},
		"no_allowance":       func(p *ApplicationAuthorizationProfileSpec) { p.MerchantIDs = nil },
		"zero_merchant":      func(p *ApplicationAuthorizationProfileSpec) { p.MerchantIDs = IdentityMerchantIDs{0} },
		"duplicate_merchant": func(p *ApplicationAuthorizationProfileSpec) { p.MerchantIDs = IdentityMerchantIDs{1000, 1000} },
		"merchant_limit":     func(p *ApplicationAuthorizationProfileSpec) { p.MerchantIDs = make(IdentityMerchantIDs, 1001) },
		"unknown_config": func(p *ApplicationAuthorizationProfileSpec) {
			p.Config = map[string]string{"appsecret": spec.Config["appsecret"], "appkey": spec.Config["appkey"], "gateway_url": "https://evil.example.invalid"}
		},
		"missing_private": func(p *ApplicationAuthorizationProfileSpec) {
			p.Config = map[string]string{"appkey": spec.Config["appkey"]}
		},
		"missing_public": func(p *ApplicationAuthorizationProfileSpec) {
			p.Config = map[string]string{"appsecret": spec.Config["appsecret"]}
		},
		"bad_sign_type": func(p *ApplicationAuthorizationProfileSpec) {
			p.Config = map[string]string{"appsecret": spec.Config["appsecret"], "appkey": spec.Config["appkey"], "sign_type": "RSA"}
		},
		"unknown_certificate": func(p *ApplicationAuthorizationProfileSpec) {
			p.Certificates = map[string]string{"file_path": "must-not-read"}
		},
		"certificate_in_public_mode": func(p *ApplicationAuthorizationProfileSpec) {
			p.Certificates = map[string]string{"app_cert_path": "must-not-read"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := spec
			change(&changed)
			if _, err := BuildApplicationAuthorizationProfiles(Import{Version: 1, ApplicationAuthorizationProfiles: []ApplicationAuthorizationProfileSpec{changed}}, true); err == nil {
				t.Fatal("invalid authorization profile accepted")
			}
		})
	}
	for _, imp := range []Import{{Version: 2}, {Version: 1, ApplicationAuthorizationProfiles: []ApplicationAuthorizationProfileSpec{spec, spec}}, {Version: 1, ApplicationAuthorizationProfiles: make([]ApplicationAuthorizationProfileSpec, 257)}} {
		if _, err := BuildApplicationAuthorizationProfiles(imp, true); err == nil {
			t.Fatal("invalid or duplicate profile set accepted")
		}
	}
}

func TestApplicationAuthorizationProfileJSONStrictCanonicalIdentifiers(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `{"id":1}`, `{"id":"01"}`, `{"id":"-1"}`, `{"id":"0"}`, `{"revision":1}`, `{"revision":"01"}`, `{"unknown":"value"}`, `{"id":"1","id":"2"}`,
		`{"enabled":null}`, `{"config":null}`, `{"config":{"appsecret":null}}`, `{"config":{"appsecret":"one","appsecret":"two"}}`, `{"config":{"appsecret":2}}`,
		`{"certificates":{"app_cert_path":"one","app_cert_path":"two"}}`, `{"certificates":{"root_cert_path":null}}`, `{"merchant_ids":[1]}`, `{"merchant_ids":["01"]}`, `{"merchant_ids":null}`, `{"app_types":null}`, `{"app_types":[null]}`, `{"id":"1"} {}`,
	} {
		var p ApplicationAuthorizationProfileSpec
		if json.Unmarshal([]byte(raw), &p) == nil {
			t.Fatal("ambiguous application authorization profile accepted", raw)
		}
	}
	var p ApplicationAuthorizationProfileSpec
	if json.Unmarshal([]byte(`{"id":"21","revision":"1","merchant_ids":["1000"]}`), &p) != nil || p.ID != 21 || p.Revision != 1 || p.Enabled || len(p.MerchantIDs) != 1 {
		t.Fatal("canonical disabled profile rejected")
	}
	raw, err := json.Marshal(p)
	if err != nil || !strings.Contains(string(raw), `"id":"21"`) || !strings.Contains(string(raw), `"merchant_ids":["1000"]`) {
		t.Fatal("profile identifiers not encoded as strings")
	}
	if json.Unmarshal([]byte(`{"config":{"appsecret":"`+strings.Repeat("a", 65536)+`"}}`), &p) == nil {
		t.Fatal("oversize profile accepted")
	}
}

func TestApplicationAuthorizationProfilesCertificateContentAndRotation(t *testing.T) {
	c := certificateChannel(t)
	spec := ApplicationAuthorizationProfileSpec{ID: 21, Revision: 1, Enabled: true, IssuerAppID: "2021000000000001", ExpectedAuthAppID: "2021000000000002", LaunchKind: "assigned", AppTypes: []provider.AlipayAuthorizationAppType{provider.AlipayAuthorizationWebApp}, MerchantIDs: IdentityMerchantIDs{1000}, Config: map[string]string{"appsecret": c.Config["appsecret"], "sign_type": "1"}, Certificates: c.Certificates}
	imp := Import{Version: 1, ApplicationAuthorizationProfiles: []ApplicationAuthorizationProfileSpec{spec}}
	p, err := BuildApplicationAuthorizationProfiles(imp, true)
	if err != nil || p[21].Adapter == nil {
		t.Fatal("complete certificate profile rejected", err)
	}
	before := p[21].Fingerprint
	rotated := spec
	rotated.Certificates = map[string]string{}
	for key, value := range spec.Certificates {
		rotated.Certificates[key] = value
	}
	rotated.Certificates["alipay_cert_path"] += "\n"
	imp.ApplicationAuthorizationProfiles[0] = rotated
	p, err = BuildApplicationAuthorizationProfiles(imp, true)
	if err != nil || p[21].Fingerprint == before {
		t.Fatal("certificate content missing from fingerprint", err)
	}
	for _, key := range []string{"app_cert_path", "alipay_cert_path", "root_cert_path"} {
		changed := rotated
		changed.Certificates = map[string]string{}
		for name, value := range rotated.Certificates {
			if name != key {
				changed.Certificates[name] = value
			}
		}
		imp.ApplicationAuthorizationProfiles[0] = changed
		if _, err := BuildApplicationAuthorizationProfiles(imp, true); err == nil {
			t.Fatal("incomplete certificate profile accepted", key)
		}
	}
}
