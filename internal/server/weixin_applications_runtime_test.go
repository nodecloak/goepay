package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
	"goepay/internal/payment"
)

// This trusted test wrapper keeps the fixture's private TLS transport while
// cloning the actual production application catalog. No default transport or
// real provider address is replaced globally.
func (f *binding180TLSFactory) WithManagedWeixinProfiles(profiles []channelconfig.IdentityProfileSpec) (adapter.ParentAccountFactory, error) {
	core, e := f.ParentChannelFactory.WithManagedIdentityProfiles(profiles)
	if e != nil {
		return nil, e
	}
	copy := *f.parent180TLSFactory
	copy.ParentChannelFactory = core
	return &binding180TLSFactory{parent180TLSFactory: &copy, miniClient: f.miniClient}, nil
}
func (f *binding180TLSFactory) WithoutWeixinProfileIDs(ids []int64) (adapter.ParentAccountFactory, error) {
	core, e := f.ParentChannelFactory.WithoutIdentityProfileIDs(ids)
	if e != nil {
		return nil, e
	}
	copy := *f.parent180TLSFactory
	copy.ParentChannelFactory = core
	return &binding180TLSFactory{parent180TLSFactory: &copy, miniClient: f.miniClient}, nil
}

func weixinApplicationData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out struct{ Data map[string]any }
	if w.Code < 200 || w.Code >= 300 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data == nil {
		t.Fatalf("application HTTP=%d", w.Code)
	}
	return out.Data
}

func TestWeixinApplicationManagedMiniActualTLSFirstSendAndOriginalRecoveryAfterCollisionArchiveCorruption(t *testing.T) {
	setup, local, web, importedMini, state := binding180Ready(t)
	f := setup.f
	ctx := t.Context()
	counter := attachWeixinVaultCounter(t, f)
	var exchanges, bad atomic.Int64
	oauth := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/sns/jscode2session" || q.Get("appid") != "synthetic-managed-mini" || q.Get("secret") != "synthetic-managed-mini-secret" || q.Get("js_code") != "managed-one-use-code" {
			bad.Add(1)
			w.WriteHeader(403)
			return
		}
		exchanges.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"appid": "synthetic-managed-mini", "openid": "synthetic-managed-openid", "session_key": "synthetic-private-session"})
	}))
	defer oauth.Close()
	local.miniClient = &http.Client{Transport: binding180OAuthTransport{oauth}}
	if e := f.s.Store.CreateMerchant(ctx, payment.Merchant{ID: 2, Name: "Synthetic managed scope", APIKey: "synthetic-managed-mid2"}); e != nil {
		t.Fatal(e)
	}
	f.s.MerchantChannels[2] = []int64{8}
	caps := weixinApplicationData(t, weixinApplicationHTTP(f, "GET", "/capabilities", ""))
	sourceFP := ""
	for _, entry := range caps["sources"].([]any) {
		row := entry.(map[string]any)
		if row["id"] == "8" {
			sourceFP, _ = row["fingerprint"].(string)
		}
	}
	if sourceFP == "" {
		t.Fatal("trusted Weixin source missing")
	}
	input := map[string]any{"request_id": strings.Repeat("1", 64), "kind": "mini_program", "name": "Managed mini", "app_id": "synthetic-managed-mini", "appsecret": "synthetic-managed-mini-secret", "enabled": true, "source_channel_id": "8", "merchant_ids": []string{"1", "2"}, "path": "pages/managed", "expected_source_fingerprint": sourceFP}
	raw, _ := json.Marshal(input)
	created := weixinApplicationData(t, weixinApplicationHTTP(f, "POST", "", string(raw)))
	appID := created["id"].(string)
	if created["execution_ready"] != true {
		t.Fatal("explicitly scoped application did not become bindable")
	}
	view, e := f.s.managedWeixinRuntime(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := view.ProviderFlow.ParentFactories[8].(*binding180TLSFactory); !ok {
		t.Fatal("managed overlay lost the trusted local TLS wrapper")
	}
	catalogResponse := parent180HTTP(f, "GET", "/"+setup.id+"/weixin-options", "")
	var list struct{ Data map[string]any }
	if catalogResponse.Code != 200 || json.Unmarshal(catalogResponse.Body.Bytes(), &list) != nil {
		t.Fatal("actual parent selector cannot consume managed app", catalogResponse.Code)
	}
	found := false
	for _, item := range list.Data["items"].([]any) {
		if item.(map[string]any)["id"] == appID {
			found = true
		}
	}
	if !found {
		t.Fatal("saved app absent from actual parent selector")
	}
	patch, _ := json.Marshal(map[string]any{"expected_fingerprint": state["fingerprint"], "account": map[string]any{"appwxa": appID, "expected_bindings_fingerprint": list.Data["fingerprint"]}})
	if out := parent180HTTP(f, "PATCH", "/"+setup.id, string(patch)); out.Code != 200 {
		t.Fatal("actual parent cannot bind managed application", out.Code)
	}
	counter.count.Store(0)
	if native := managedWeixinRequest(t, f, "1", "test-secret", setup.id, "managed-native-valid", "native", "", false); native["code"] != float64(1) || counter.count.Load() != 1 {
		t.Fatal("managed-bound native did not validate precisely its current application dependency", native["msg"], counter.count.Load())
	}
	for _, no := range []string{"managed-first-A", "managed-first-B"} {
		if no == "managed-first-B" {
			f.s.MerchantChannels[2] = nil
		}
		if out := binding180Payment(t, f, setup.id, no, "synthetic-managed-mini", true); out["code"] != float64(1) {
			t.Fatal("sealed new order failed its first real local TLS send", no, out["msg"])
		}
	}
	before := setup.creates.Load()
	if result := managedWeixinRequest(t, f, "2", "synthetic-managed-mid2", setup.id, "managed-mid2-revoked", "jsapi", "synthetic-managed-mini", true); result["code"] == float64(1) || setup.creates.Load() != before {
		t.Fatal("revoked merchant borrowed another merchant's source or sent a payment")
	}
	launch := managedWeixinRequest(t, f, "1", "test-secret", setup.id, "managed-awaiting-first-send", "app", "", false)
	if launch["code"] != float64(1) {
		t.Fatal("synthetic original app intent not created", launch["msg"])
	}
	awaiting, e := f.s.Store.Order(ctx, 1, "managed-awaiting-first-send")
	if e != nil {
		t.Fatal(e)
	}
	var attempts int
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM provider_attempts WHERE order_id=$1`, awaiting.ID).Scan(&attempts); e != nil || attempts != 0 {
		t.Fatal("app launch must remain before its first provider send", attempts, e)
	}
	original, e := f.s.Store.Order(ctx, 1, "managed-first-A")
	if e != nil {
		t.Fatal(e)
	}
	resolved, e := f.s.withParentRuntime(ctx, 1, "managed-first-A")
	if e != nil {
		t.Fatal(e)
	}
	channel, _ := strconv.ParseInt(setup.id, 10, 64)
	mini := resolved.MiniOAuthProviders[channel]
	if mini == nil {
		t.Fatal("managed secret did not enter the real mini adapter")
	}
	if identity, e := mini.ExchangeMiniCode(ctx, "managed-one-use-code"); e != nil || identity.SubjectID != "synthetic-managed-openid" || exchanges.Load() != 1 || bad.Load() != 0 {
		t.Fatal("real local OAuth did not consume saved APPSECRET", e, exchanges.Load(), bad.Load())
	}
	f.s.IdentityProfileEnabled = true
	f.s.IdentityProfileCallbackBaseURL = "https://pay.example.invalid"
	_, binding, ready := f.s.identityProfileBinding(ctx, 1, mustWeixinAppID(t, appID))
	if !ready {
		t.Fatal("managed identity capability not available")
	}
	pending, e := f.s.Store.CreateProfileIdentityLookup(ctx, f.token, "admin", binding)
	if e != nil {
		t.Fatal(e)
	}
	freshCaps := weixinApplicationData(t, weixinApplicationHTTP(f, "GET", "/capabilities", ""))
	for _, entry := range freshCaps["sources"].([]any) {
		row := entry.(map[string]any)
		if row["id"] == "8" {
			sourceFP = row["fingerprint"].(string)
		}
	}
	input["request_id"] = strings.Repeat("3", 64)
	input["expected_revision"] = created["revision"]
	input["expected_fingerprint"] = created["fingerprint"]
	input["expected_source_fingerprint"] = sourceFP
	input["merchant_ids"] = []string{"1"}
	input["appsecret"] = "synthetic-managed-rotated"
	raw, _ = json.Marshal(input)
	rotated := weixinApplicationData(t, weixinApplicationHTTP(f, "PATCH", "/"+appID, string(raw)))
	if out := weixinApplicationHTTP(f, "PATCH", "/"+appID, string(raw)); out.Code != 200 {
		t.Fatal("same PATCH receipt hit its stale CAS", out.Code)
	}
	_, afterBinding, ready := f.s.identityProfileBinding(ctx, 1, mustWeixinAppID(t, appID))
	if !ready || afterBinding.Fingerprint == binding.Fingerprint {
		t.Fatal("explicit vault rotation did not change current identity source")
	}
	if _, e = f.s.Store.ReadProfileIdentityLookup(ctx, pending.ID, f.token, "admin", afterBinding); e == nil {
		t.Fatal("pending identity request used a rotated application's old credential")
	}
	if result := binding180Payment(t, f, setup.id, "managed-explicit-rotation-stale", "synthetic-managed-mini", true); result["code"] == float64(1) || setup.creates.Load() != before {
		t.Fatal("explicit vault scope/secret rotation did not make old parent binding stale")
	}
	mirror := channelconfig.IdentityProfileSpec{ID: mustWeixinAppID(t, appID), Revision: 1, ChannelID: 8, Enabled: true, Provider: "wxpay", Kind: "mini", AppID: "synthetic-managed-mini", AccountMode: "direct", Path: "pages/managed", MerchantIDs: channelconfig.IdentityMerchantIDs{1, 2}, Config: map[string]string{"appsecret": "synthetic-managed-mini-secret"}}
	imported, e := channelconfig.NewParentChannelFactory(local.Bootstrap(), channelconfig.ParentBuildOptions{BuildOptions: channelconfig.BuildOptions{AllowLive: true}, OAuthEnabled: true, IdentityProfiles: []channelconfig.IdentityProfileSpec{web, importedMini, mirror}})
	if e != nil {
		t.Fatal(e)
	}
	local.ParentChannelFactory = imported
	f.s.WeixinImportedProfileIDs = append(f.s.WeixinImportedProfileIDs, mustWeixinAppID(t, appID))
	assertWeixinIDQuarantined(t, f.s, mustWeixinAppID(t, appID))
	collisionDirectory := weixinApplicationData(t, weixinApplicationHTTP(f, "GET", "", ""))
	warned := false
	for _, raw := range collisionDirectory["configuration_errors"].([]any) {
		if raw.(map[string]any)["id"] == appID {
			warned = true
		}
	}
	for _, raw := range collisionDirectory["items"].([]any) {
		row := raw.(map[string]any)
		if row["origin"] == "managed" && row["id"] == appID && row["execution_ready"] != false {
			t.Fatal("conflicting managed application was advertised as execution-ready")
		}
	}
	if !warned {
		t.Fatal("future namespace conflict was not explained in the application directory")
	}
	archive, _ := json.Marshal(map[string]any{"request_id": strings.Repeat("2", 64), "expected_revision": rotated["revision"], "expected_fingerprint": rotated["fingerprint"]})
	archived := weixinApplicationHTTP(f, "POST", "/"+appID+"/archive", string(archive))
	if archived.Code != 200 {
		t.Fatal("colliding managed application cannot be archived safely", archived.Code)
	}
	if duplicate := weixinApplicationHTTP(f, "POST", "/"+appID+"/archive", string(archive)); duplicate.Code != 200 {
		t.Fatal("confirmed archive replay was repriced or hit stale CAS", duplicate.Code)
	}
	if _, e = f.s.managedWeixinRuntime(ctx); e != nil {
		t.Fatal("archived managed collision still blocks imported catalog", e)
	}
	if native := managedWeixinRequest(t, f, "1", "test-secret", setup.id, "managed-native-archived", "native", "", false); native["code"] == float64(1) || setup.creates.Load() != before {
		t.Fatal("archived managed application was bypassed through native's empty variant", native["msg"], setup.creates.Load(), before)
	}
	if _, e = f.s.ProviderFlow.Submit(ctx, awaiting.TradeNo, channel, "jsapi", "192.0.2.1", "synthetic-managed-openid"); !errors.Is(e, payment.ErrConflict) || setup.creates.Load() != before {
		t.Fatal("archived managed intent borrowed an imported same-ID/same-AppID credential", e, setup.creates.Load())
	}
	if _, e = f.p.Exec(ctx, `UPDATE runtime_weixin_app_heads SET proof_hash=decode(repeat('00',32),'hex') WHERE id=$1`, mustWeixinAppID(t, appID)); e != nil {
		t.Fatal(e)
	}
	assertWeixinIDQuarantined(t, f.s, mustWeixinAppID(t, appID))
	if _, e = f.s.withParentRuntime(ctx, 1, original.OutTradeNo); e != nil {
		t.Fatal("current vault corruption blocked historical execution restoration", e)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, original.TradeNo); e != nil || setup.queries.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("original query did not retain its sealed SDK", e, setup.queries.Load(), setup.bad.Load())
	}
	second, e := f.s.Store.Order(ctx, 1, "managed-first-B")
	if e != nil {
		t.Fatal(e)
	}
	callback := parent180SignedXML(map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "synthetic-managed-mini", "mch_id": "synthetic180-fresh-mch", "nonce_str": "managed-callback", "out_trade_no": second.TradeNo, "transaction_id": "WX180_" + second.TradeNo, "total_fee": "10000", "fee_type": "CNY", "openid": "synthetic-openid-synthetic-managed-mini"}, "abcdef0123456789abcdef0123456789")
	req := httptest.NewRequest("POST", "/callbacks/providers/"+setup.id+"/"+second.TradeNo, bytes.NewReader(callback))
	req.Header.Set("Content-Type", "application/xml")
	result := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(result, req)
	if result.Code != 200 {
		t.Fatal("current vault corruption/archival blocked original authenticated callback", result.Code)
	}
	paid, e := f.s.Store.Order(ctx, 1, second.OutTradeNo)
	if e != nil || paid.Status != "paid" {
		t.Fatal("authenticated original callback did not credit its original order", paid.Status, e)
	}
	var journalCount int
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM journals WHERE event_key=$1`, "payment:"+second.TradeNo).Scan(&journalCount); e != nil || journalCount != 1 {
		t.Fatal("callback did not post exactly one original payment journal", e, journalCount)
	}
	replay := httptest.NewRequest("POST", "/callbacks/providers/"+setup.id+"/"+second.TradeNo, bytes.NewReader(callback))
	replay.Header.Set("Content-Type", "application/xml")
	replayed := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(replayed, replay)
	var afterJournals int
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM journals WHERE event_key=$1`, "payment:"+second.TradeNo).Scan(&afterJournals); e != nil || replayed.Code != 200 || afterJournals != journalCount {
		t.Fatal("callback replay duplicated accounting", e, replayed.Code, journalCount, afterJournals)
	}
	workerCore, e := channelconfig.NewParentChannelFactory(local.Bootstrap(), channelconfig.ParentBuildOptions{})
	if e != nil {
		t.Fatal(e)
	}
	workerLocal := *local.parent180TLSFactory
	workerLocal.ParentChannelFactory = workerCore
	worker := *f.s.ProviderFlow
	worker.AllowLive = false
	worker.ParentFactories = map[int64]adapter.ParentAccountFactory{8: &binding180TLSFactory{parent180TLSFactory: &workerLocal}}
	if _, e = worker.Query(ctx, 1, original.TradeNo); e != nil || setup.creates.Load() != before {
		t.Fatal("default-off worker could not query the original managed sealed source", e, setup.creates.Load())
	}
}

func attachWeixinVaultCounter(t *testing.T, f channel168Fixture) *weixinCatalogQueryCounter {
	t.Helper()
	counter := &weixinCatalogQueryCounter{}
	cfg := f.p.Config()
	cfg.ConnConfig.Tracer = counter
	pool, e := pgxpool.NewWithConfig(t.Context(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	store, e := payment.NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	f.s.Pool = pool
	f.s.Store = store
	f.s.ProviderFlow.Store = store
	return counter
}

func assertWeixinIDQuarantined(t *testing.T, s *Server, id int64) {
	t.Helper()
	view, e := s.managedWeixinRuntime(t.Context())
	if e != nil {
		t.Fatal("isolated bad app broke the whole runtime", e)
	}
	for _, f := range view.ProviderFlow.ParentFactories {
		if c, ok := f.(interface {
			ParentWeixinCatalog() adapter.ParentWeixinCatalog
		}); ok {
			for _, item := range c.ParentWeixinCatalog().Items {
				if item.ID == id {
					t.Fatal("conflicting/corrupt managed ID remained selectable")
				}
			}
		}
	}
}

func managedWeixinRequest(t *testing.T, f channel168Fixture, pid, key, channel, no, method, app string, openid bool) map[string]any {
	t.Helper()
	p := compat.Params{"pid": pid, "out_trade_no": no, "type": "wxpay", "name": "Synthetic managed request", "money": "100.00", "channel_id": channel, "method": method, "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
	if app != "" {
		p["sub_appid"] = app
	}
	if openid {
		p["sub_openid"] = "synthetic-managed-openid"
		p["is_applet"] = "1"
	}
	p["sign"] = compat.SignMD5(p, key)
	values := url.Values{}
	for k, v := range p {
		values.Set(k, v)
	}
	r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var out map[string]any
	if json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal("synthetic managed response not JSON", w.Code)
	}
	return out
}

func mustWeixinAppID(t *testing.T, value string) int64 {
	t.Helper()
	id, e := strconv.ParseInt(value, 10, 64)
	if e != nil || id <= 0 {
		t.Fatal("invalid synthetic app ID")
	}
	return id
}
