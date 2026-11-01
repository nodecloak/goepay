package server

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/payment"
	"golang.org/x/crypto/hkdf"
)

func TestWeixinApplicationReviewExternalImportsAndPublicBuildersCannotForgeManagedOrigin(t *testing.T) {
	origin := channelconfig.Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "wxroot", "appmchid": "synthetic-root", "appkey": strings.Repeat("k", 32)}}
	spec := channelconfig.IdentityProfileSpec{ID: 81, Revision: 1, ChannelID: 7, Enabled: true, Provider: "wxpay", Kind: "web", AppID: "wxlegacyreview", AccountMode: "direct", MerchantIDs: channelconfig.IdentityMerchantIDs{1}, Config: map[string]string{"appsecret": "synthetic-legacy-secret"}}
	legacy, e := json.Marshal(spec)
	if e != nil {
		t.Fatal(e)
	}
	const originalBytes = `{"id":"81","revision":"1","channel_id":"7","enabled":true,"provider":"wxpay","kind":"web","app_id":"wxlegacyreview","account_mode":"direct","path":"","merchant_ids":["1"],"config":{"appsecret":"synthetic-legacy-secret"}}`
	if string(legacy) != originalBytes {
		t.Fatal("legacy profile serialization changed when no managed marker is present")
	}
	spec.ManagedVaultFingerprint = "vault186:" + strings.Repeat("a", 64)
	for _, enabled := range []bool{false, true} {
		if _, e := channelconfig.BuildIdentityProfiles(channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{origin}, IdentityProfiles: []channelconfig.IdentityProfileSpec{spec}}, enabled); e == nil {
			t.Error("public identity builder accepted an invented managed origin", enabled)
		}
	}
	plain, e := json.Marshal(channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{origin}, IdentityProfiles: []channelconfig.IdentityProfileSpec{spec}})
	if e != nil {
		t.Fatal(e)
	}
	var decoded channelconfig.Import
	if json.Unmarshal(plain, &decoded) == nil {
		t.Error("external import JSON impersonated the managed vault")
	}
	const purpose = "goepay:imported-channels:v1"
	master := bytes.Repeat([]byte{2}, 32)
	key := make([]byte, 32)
	if _, e := io.ReadFull(hkdf.New(sha256.New, master, nil, []byte(purpose)), key); e != nil {
		t.Fatal(e)
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		t.Fatal(e)
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		t.Fatal(e)
	}
	nonce := bytes.Repeat([]byte{3}, aead.NonceSize())
	sealed := aead.Seal(nil, nonce, plain, []byte(purpose))
	envelope, e := json.Marshal(map[string]any{"version": 1, "aad": purpose, "nonce": base64.StdEncoding.EncodeToString(nonce), "ciphertext": base64.StdEncoding.EncodeToString(sealed[:len(sealed)-aead.Overhead()]), "tag": base64.StdEncoding.EncodeToString(sealed[len(sealed)-aead.Overhead():])})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := channelconfig.Decrypt(envelope, master); e == nil {
		t.Error("an authenticated encrypted external import forged managed provenance")
	}
}

func reviewWeixinFactory(t *testing.T, profiles []channelconfig.IdentityProfileSpec) *channelconfig.ParentChannelFactory {
	t.Helper()
	origin := channelconfig.Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1", "2"}, Config: map[string]string{"appid": "wxreviewroot", "appmchid": "synthetic-review-mch", "appkey": strings.Repeat("k", 32)}}
	f, e := channelconfig.NewParentChannelFactory(origin, channelconfig.ParentBuildOptions{IdentityProfiles: profiles})
	if e != nil {
		t.Fatal(e)
	}
	return f
}

func reviewWeixinHTTPData(t *testing.T, w *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	var out struct{ Data map[string]any }
	if w.Code != status || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data == nil {
		t.Fatalf("unexpected application HTTP result: got=%d want=%d", w.Code, status)
	}
	return out.Data
}

func reviewWeixinSourceFingerprint(t *testing.T, f channel168Fixture) string {
	t.Helper()
	data := reviewWeixinHTTPData(t, weixinApplicationHTTP(f, "GET", "/capabilities", ""), 200)
	sources, ok := data["sources"].([]any)
	if !ok {
		t.Fatal("source capabilities missing")
	}
	for _, value := range sources {
		source, ok := value.(map[string]any)
		if ok && source["id"] == "7" {
			fp, _ := source["fingerprint"].(string)
			return fp
		}
	}
	t.Fatal("registered source 7 not offered")
	return ""
}

func reviewWeixinHTTPBody(request, fingerprint, name string, mids []string) map[string]any {
	return map[string]any{"request_id": request, "kind": "official_account", "name": name, "app_id": "wxmanagedreview", "appsecret": "synthetic-managed-review-secret", "enabled": true, "source_channel_id": "7", "merchant_ids": mids, "path": "", "expected_source_fingerprint": fingerprint}
}

func reviewWeixinJSON(t *testing.T, body map[string]any) string {
	t.Helper()
	raw, e := json.Marshal(body)
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}

func TestWeixinApplicationReviewCurrentSourceIntersectionDoesNotExpandOrPoisonOtherMerchants(t *testing.T) {
	f := channel168Local(t)
	root := reviewWeixinFactory(t, nil)
	f.s.ProviderFlow.ParentFactories = map[int64]adapter.ParentAccountFactory{7: root}
	f.s.MerchantChannels = map[int64][]int64{1: {7}, 2: {7}}
	f.s.IdentityProfileEnabled = true
	if e := f.s.Store.CreateMerchant(t.Context(), payment.Merchant{ID: 2, Name: "Synthetic source review", APIKey: "synthetic-scope"}); e != nil {
		t.Fatal(e)
	}
	fp := reviewWeixinSourceFingerprint(t, f)
	body := reviewWeixinJSON(t, reviewWeixinHTTPBody(strings.Repeat("a", 64), fp, "Scoped managed application", []string{"1", "2"}))
	view := reviewWeixinHTTPData(t, weixinApplicationHTTP(f, "POST", "", body), 201)
	id, e := strconv.ParseInt(view["id"].(string), 10, 64)
	if e != nil {
		t.Fatal(e)
	}
	resolved, e := f.s.managedWeixinRuntime(t.Context())
	if e != nil {
		t.Fatal("new managed profile did not consume the declared source scope", e)
	}
	_, binding1, ready1 := resolved.identityProfileBindingFromCatalog(t.Context(), 1, id)
	_, _, ready2 := resolved.identityProfileBindingFromCatalog(t.Context(), 2, id)
	if !ready1 || !ready2 {
		t.Fatal("configured source did not authorize both declared merchants")
	}
	f.s.MerchantChannels[2] = nil
	remaining, e := f.s.managedWeixinRuntime(t.Context())
	if e != nil {
		t.Fatal("revoking merchant 2 poisoned merchant 1 or retained merchant 2", e)
	}
	_, after1, still1 := remaining.identityProfileBindingFromCatalog(t.Context(), 1, id)
	_, _, still2 := remaining.identityProfileBindingFromCatalog(t.Context(), 2, id)
	if !still1 || still2 || after1.Fingerprint != binding1.Fingerprint {
		t.Error("effective source revocation expanded authority or rewrote the unchanged stored profile identity")
	}
	if len(root.ParentWeixinCatalog().Items) != 0 || len(f.s.IdentityProfiles) != 0 {
		t.Error("request-owned scope resolution mutated shared profile registries")
	}
	delete(f.s.MerchantChannels, 1)
	none, e := f.s.managedWeixinRuntime(t.Context())
	if e != nil {
		t.Fatal("empty effective scope broke unrelated base configuration", e)
	}
	if _, exists := none.IdentityProfiles[id]; exists {
		t.Error("a profile with no authorized merchants remained executable")
	}
}

func TestWeixinApplicationReviewFutureImportCollisionCanBeArchivedAndReceiptsReplayBeforeCAS(t *testing.T) {
	f := channel168Local(t)
	root := reviewWeixinFactory(t, nil)
	f.s.ProviderFlow.ParentFactories = map[int64]adapter.ParentAccountFactory{7: root}
	f.s.MerchantChannels = map[int64][]int64{1: {7}}
	f.s.IdentityProfileEnabled = true
	fp := reviewWeixinSourceFingerprint(t, f)
	create := reviewWeixinHTTPBody(strings.Repeat("b", 64), fp, "Original managed application", []string{"1"})
	first := reviewWeixinHTTPData(t, weixinApplicationHTTP(f, "POST", "", reviewWeixinJSON(t, create)), 201)
	id, e := strconv.ParseInt(first["id"].(string), 10, 64)
	if e != nil {
		t.Fatal(e)
	}
	patch := reviewWeixinHTTPBody(strings.Repeat("c", 64), fp, "Renamed managed application", []string{"1"})
	patch["appsecret"] = ""
	patch["expected_revision"], patch["expected_fingerprint"] = "1", first["fingerprint"]
	path := "/" + first["id"].(string)
	current := reviewWeixinHTTPData(t, weixinApplicationHTTP(f, "PATCH", path, reviewWeixinJSON(t, patch)), 200)
	replayed := reviewWeixinHTTPData(t, weixinApplicationHTTP(f, "PATCH", path, reviewWeixinJSON(t, patch)), 200)
	if replayed["revision"] != "2" || replayed["fingerprint"] != current["fingerprint"] {
		t.Error("accepted PATCH replay was re-applied or evaluated against newer CAS")
	}
	imported := channelconfig.IdentityProfileSpec{ID: id, Revision: 17, ChannelID: 7, Enabled: true, Provider: "wxpay", Kind: "web", AppID: "wxfutureimport", AccountMode: "direct", MerchantIDs: channelconfig.IdentityMerchantIDs{1}, Config: map[string]string{"appsecret": "synthetic-future-import-secret"}}
	future := reviewWeixinFactory(t, []channelconfig.IdentityProfileSpec{imported})
	f.s.ProviderFlow.ParentFactories[7] = future
	f.s.WeixinImportedProfileIDs = []int64{id}
	f.s.IdentityProfiles, e = channelconfig.BuildIdentityProfiles(channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{future.Bootstrap()}, IdentityProfiles: []channelconfig.IdentityProfileSpec{imported}}, true)
	if e != nil {
		t.Fatal(e)
	}
	if conflict, e := f.s.managedWeixinRuntime(t.Context()); e == nil {
		if _, _, ready := conflict.identityProfileBindingFromCatalog(t.Context(), 1, id); ready {
			t.Error("a conflicted managed ID silently selected the newly imported identity")
		}
	}
	archive := map[string]any{"request_id": strings.Repeat("d", 64), "expected_revision": "2", "expected_fingerprint": current["fingerprint"]}
	archived := reviewWeixinHTTPData(t, weixinApplicationHTTP(f, "POST", path+"/archive", reviewWeixinJSON(t, archive)), 200)
	if archived["archived"] != true || archived["revision"] != "3" {
		t.Error("conflicting managed entry could not be safely archived")
	}
	again := reviewWeixinHTTPData(t, weixinApplicationHTTP(f, "POST", path+"/archive", reviewWeixinJSON(t, archive)), 200)
	if again["fingerprint"] != archived["fingerprint"] || again["revision"] != "3" {
		t.Error("accepted archive request did not replay its immutable result")
	}
	archive["expected_revision"] = "1"
	if w := weixinApplicationHTTP(f, "POST", path+"/archive", reviewWeixinJSON(t, archive)); w.Code != 409 {
		t.Error("same request ID was accepted for different archive facts", w.Code)
	}
	after, e := f.s.managedWeixinRuntime(t.Context())
	if e != nil || after.IdentityProfiles[id].AppID != imported.AppID || f.s.IdentityProfiles[id].AppID != imported.AppID {
		t.Fatal("archived collision stranded or replaced the trusted imported profile", e)
	}
	var heads, versions, receipts int
	if e = f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM runtime_weixin_app_heads),(SELECT count(*) FROM runtime_weixin_app_versions),(SELECT count(*) FROM runtime_weixin_app_requests)`).Scan(&heads, &versions, &receipts); e != nil || heads != 1 || versions != 3 || receipts != 3 {
		t.Errorf("replays/conflicts retained extra versions: %d/%d/%d err=%v", heads, versions, receipts, e)
	}
}

func TestWeixinApplicationReviewActualFactoryCapacityAndInvalidSecretRejectBeforeCommit(t *testing.T) {
	f := channel168Local(t)
	profiles := make([]channelconfig.IdentityProfileSpec, 255)
	for i := range profiles {
		profiles[i] = channelconfig.IdentityProfileSpec{ID: int64(i + 1), Revision: 1, ChannelID: 7, Provider: "wxpay", Kind: "web", AppID: fmt.Sprint("wxdisabled", i), AccountMode: "direct", MerchantIDs: channelconfig.IdentityMerchantIDs{}, Config: map[string]string{}}
	}
	root := reviewWeixinFactory(t, profiles)
	f.s.ProviderFlow.ParentFactories = map[int64]adapter.ParentAccountFactory{7: root}
	f.s.MerchantChannels = map[int64][]int64{1: {7}}
	fp := reviewWeixinSourceFingerprint(t, f)
	invalid := reviewWeixinHTTPBody(strings.Repeat("e", 64), fp, "Invalid SDK credential", []string{"1"})
	invalid["appsecret"] = "@synthetic-invalid-sdk-credential"
	if w := weixinApplicationHTTP(f, "POST", "", reviewWeixinJSON(t, invalid)); w.Code != 422 && w.Code != 400 {
		t.Fatal("SDK-invalid credential was accepted into the executable vault", w.Code)
	}
	first := reviewWeixinHTTPBody(strings.Repeat("f", 64), fp, "Last factory profile slot", []string{"1"})
	view := reviewWeixinHTTPData(t, weixinApplicationHTTP(f, "POST", "", reviewWeixinJSON(t, first)), 201)
	if view["id"] != "256" {
		t.Error("managed allocator did not reserve every disabled import ID", view["id"])
	}
	second := reviewWeixinHTTPBody(strings.Repeat("0", 64), fp, "Over factory capacity", []string{"1"})
	if w := weixinApplicationHTTP(f, "POST", "", reviewWeixinJSON(t, second)); w.Code != 422 && w.Code != 409 {
		t.Fatal("over-capacity profile poisoned the current executable catalog", w.Code)
	}
	var heads, versions, receipts int
	if e := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM runtime_weixin_app_heads),(SELECT count(*) FROM runtime_weixin_app_versions),(SELECT count(*) FROM runtime_weixin_app_requests)`).Scan(&heads, &versions, &receipts); e != nil || heads != 1 || versions != 1 || receipts != 1 {
		t.Errorf("refused credential/capacity mutation retained private records: %d/%d/%d %v", heads, versions, receipts, e)
	}
	if _, e := f.s.managedWeixinRuntime(t.Context()); e != nil {
		t.Fatal("accepted boundary entry is not usable by the actual factory", e)
	}
}
