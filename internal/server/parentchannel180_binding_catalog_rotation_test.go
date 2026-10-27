package server

import (
	"encoding/json"
	"goepay/internal/channelconfig"
	"testing"
)

func TestParentChannel180WeixinRegistryRotationRequiresFreshExplicitBindingButKeepsOriginalSDK(t *testing.T) {
	setup, local, web, mini, state := binding180Ready(t)
	f := setup.f
	if out := binding180Payment(t, f, setup.id, "binding180-registry-original", web.AppID, false); out["code"] != float64(1) {
		t.Fatal(out)
	}
	order, err := f.s.Store.Order(t.Context(), 1, "binding180-registry-original")
	if err != nil {
		t.Fatal(err)
	}
	web.AppID = "synthetic-web-B"
	web.Revision = 2
	web.Config = map[string]string{"appsecret": "synthetic-web-secret-B"}
	next, err := channelconfig.NewParentChannelFactory(local.Bootstrap(), channelconfig.ParentBuildOptions{BuildOptions: channelconfig.BuildOptions{AllowLive: true}, IdentityProfiles: []channelconfig.IdentityProfileSpec{web, mini}, OAuthEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	local.ParentChannelFactory = next
	if out := binding180Payment(t, f, setup.id, "binding180-stale-registry-fresh-intent", "synthetic-web-A", false); out["code"] == float64(1) || setup.creates.Load() != 1 {
		t.Fatal("fresh order used superseded profile before explicit rebind", out, setup.creates.Load())
	}
	if _, err = f.s.ProviderFlow.Query(t.Context(), 1, order.TradeNo); err != nil {
		t.Fatal("registry rotation stranded original SDK", err)
	}
	patch, _ := json.Marshal(map[string]any{"expected_fingerprint": state["fingerprint"], "account": map[string]any{"appwxmp": "81", "expected_bindings_fingerprint": next.ParentWeixinCatalog().Fingerprint}})
	updated := parent180HTTP(f, "PATCH", "/"+setup.id, string(patch))
	if updated.Code != 200 {
		t.Fatal(updated.Code, updated.Body.String())
	}
	if out := binding180Payment(t, f, setup.id, "binding180-explicit-rebind-current", web.AppID, false); out["code"] != float64(1) || setup.creates.Load() != 2 || setup.bad.Load() != 0 {
		t.Fatal("explicit current binding did not restore new payments", out, setup.creates.Load(), setup.bad.Load())
	}
}
