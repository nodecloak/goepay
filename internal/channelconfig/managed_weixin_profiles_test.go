package channelconfig

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"golang.org/x/crypto/hkdf"
)

func TestManagedWeixinFactoryOverlayPreservesRootOptionsAndNeverShadowsAnyImport(t *testing.T) {
	origin := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1", "2"}, Config: map[string]string{"appid": "base-app", "appmchid": "base-merchant", "appkey": strings.Repeat("k", 32)}}
	reserved := IdentityProfileSpec{ID: 88, ChannelID: 99, Provider: "alipay", Kind: "web", AppID: "disabled-import", Enabled: false}
	f, e := NewParentChannelFactory(origin, ParentBuildOptions{IdentityProfiles: []IdentityProfileSpec{reserved}, OAuthEnabled: false, WebOAuthAllowed: func() bool { return false }})
	if e != nil {
		t.Fatal(e)
	}
	api, ok := any(f).(interface {
		WithManagedIdentityProfiles([]IdentityProfileSpec) (*ParentChannelFactory, error)
		ReservedIdentityProfileIDs() []int64
	})
	if !ok {
		t.Fatal("no request-owned managed application factory overlay")
	}
	spec := IdentityProfileSpec{ManagedVaultFingerprint: adapter.ManagedWeixinVaultOrigin + strings.Repeat("a", 64), ID: 89, Revision: 1, ChannelID: 7, Provider: "wxpay", Kind: "web", AppID: "wxmanaged", AccountMode: "direct", Enabled: true, MerchantIDs: IdentityMerchantIDs{1}, Config: map[string]string{"appsecret": "synthetic-managed-secret"}}
	next, e := api.WithManagedIdentityProfiles([]IdentityProfileSpec{spec})
	if e != nil {
		t.Fatal(e)
	}
	if len(f.ParentWeixinCatalog().Items) != 0 || len(next.ParentWeixinCatalog().Items) != 1 || next.options.OAuthEnabled || next.options.WebOAuthAllowed == nil || next.options.WebOAuthAllowed() {
		t.Fatal("overlay mutated root catalog or expanded OAuth gates")
	}
	spec.Config["appsecret"] = "caller-mutated"
	spec.MerchantIDs[0] = 2
	origin.Config["appid"] = "caller-mutated-root"
	if next.profileCatalog[89].Config["appsecret"] != "synthetic-managed-secret" || next.profileCatalog[89].MerchantIDs[0] != 1 || next.origin.Config["appid"] != "base-app" {
		t.Fatal("overlay retains mutable input maps/slices")
	}
	collision := next.profileCatalog[89]
	collision.ID = 88
	if _, e = api.WithManagedIdentityProfiles([]IdentityProfileSpec{collision}); e == nil {
		t.Fatal("disabled other-source Alipay import was shadowed")
	}
	_, raw, e := next.BootstrapSnapshot(7)
	if e != nil {
		t.Fatal(e)
	}
	id := int64(89)
	execution, sealed, e := next.ChangeSnapshot(7, raw, adapter.ParentChannelAccountChange{AppWXMP: &id, ExpectedBindings: next.ParentWeixinCatalog().Fingerprint})
	if e != nil {
		t.Fatal(e)
	}
	if execution.Web != nil {
		t.Fatal("application binding bypassed global OAuth-off")
	}
	old, e := f.Restore(7, sealed)
	if e != nil || old.Web != nil || old.Mini != nil {
		t.Fatal("original sealed profile cannot restore without the current vault catalog", e)
	}
	startup, e := PrepareParentStartup(Import{Version: 1, Channels: []Channel{f.Bootstrap()}, IdentityProfiles: []IdentityProfileSpec{reserved}}, []adapter.ParentStartupAccount{{ChannelID: 7, SourceID: 7, PayType: "wxpay", Snapshot: sealed}})
	if e != nil {
		t.Fatal("authenticated managed sealed snapshot cannot restart from public import origins", e)
	}
	workers, e := startup.BuildFactories(ParentBuildOptions{})
	if e != nil {
		t.Fatal(e)
	}
	restored, e := workers[7].Restore(7, sealed)
	if e != nil || restored.Web != nil || restored.Mini != nil {
		t.Fatal("worker startup opened OAuth or could not restore managed history", e)
	}
}

func TestManagedWeixinOriginCannotBeForgedByPublicImportLoadOrBuild(t *testing.T) {
	imp := miniLinkIdentityImport()
	baseline, e := json.Marshal(imp)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(baseline), "managed_vault_fingerprint") {
		t.Fatal("legacy import bytes changed without managed origin")
	}
	imp.IdentityProfiles[0].ManagedVaultFingerprint = adapter.ManagedWeixinVaultOrigin + strings.Repeat("a", 64)
	for _, enabled := range []bool{false, true} {
		if _, e = BuildIdentityProfiles(imp, enabled); e == nil {
			t.Fatal("public Build accepted forged managed origin", enabled)
		}
	}
	raw, e := json.Marshal(imp)
	if e != nil {
		t.Fatal(e)
	}
	var decoded Import
	if json.Unmarshal(raw, &decoded) == nil {
		t.Fatal("public Import JSON accepted managed provenance")
	}
	key := make([]byte, 32)
	derived := make([]byte, 32)
	io.ReadFull(hkdf.New(sha256.New, key, nil, []byte(importContext)), derived)
	block, _ := aes.NewCipher(derived)
	g, _ := cipher.NewGCM(block)
	nonce := make([]byte, g.NonceSize())
	sealed := g.Seal(nil, nonce, raw, []byte(importContext))
	envelope, _ := json.Marshal(map[string]any{"version": 1, "aad": importContext, "nonce": base64.StdEncoding.EncodeToString(nonce), "ciphertext": base64.StdEncoding.EncodeToString(sealed[:len(sealed)-16]), "tag": base64.StdEncoding.EncodeToString(sealed[len(sealed)-16:])})
	if _, e = Decrypt(envelope, key); e == nil {
		t.Fatal("public encrypted decrypt accepted managed provenance")
	}
	path := filepath.Join(t.TempDir(), "synthetic-import.json")
	if e = os.WriteFile(path, envelope, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Load(path, key); e == nil {
		t.Fatal("public encrypted Load accepted managed provenance")
	}
}
