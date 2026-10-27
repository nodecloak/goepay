package server

import (
	"encoding/json"
	"goepay/internal/channelconfig"
	"strconv"
	"testing"
)

func TestParentChannel180ProfilesFollowOriginalPaymentAccountAfterRotation(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	oldFactory := f.s.ProviderFlow.ParentFactories[8].(*parent180TLSFactory)
	constructor, e := channelconfig.NewParentChannelFactory(oldFactory.Bootstrap(), channelconfig.ParentBuildOptions{BuildOptions: channelconfig.BuildOptions{AllowLive: true}, OAuthEnabled: true})
	if e != nil {
		t.Fatal(e)
	}
	oldFactory.ParentChannelFactory = constructor
	change, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "account": map[string]any{"app_types": []string{"1", "2"}, "config": map[string]any{"enable_web_oauth": map[string]string{"operation": "replace", "value": "true"}, "oauth_appsecret": map[string]string{"operation": "replace", "value": "synthetic-profile-secret-A"}}}})
	w := parent180HTTP(f, "PATCH", "/"+setup.id, string(change))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var state struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &state)
	_, order := parent180Prepared(t, setup, "parent180-profile-original")
	id, _ := strconv.ParseInt(setup.id, 10, 64)
	prepared, e := f.s.withParentRuntime(t.Context(), 1, order.OutTradeNo)
	if e != nil {
		t.Fatal(e)
	}
	if prepared.OAuthProviders[id] == nil {
		t.Fatal("managed account profile absent from request-local real runtime")
	}
	originalApp := prepared.OAuthProviders[id].ApplicationID()
	change, _ = json.Marshal(map[string]any{"expected_fingerprint": state.Data["fingerprint"], "account": map[string]any{"config": map[string]any{"appid": map[string]string{"operation": "replace", "value": "synthetic-profile-app-B"}, "oauth_appsecret": map[string]string{"operation": "replace", "value": "synthetic-profile-secret-B"}}}})
	w = parent180HTTP(f, "PATCH", "/"+setup.id, string(change))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	current, e := f.s.withParentRuntime(t.Context(), 1, "")
	if e != nil {
		t.Fatal(e)
	}
	if current.OAuthProviders[id] == nil || current.OAuthProviders[id].ApplicationID() != "synthetic-profile-app-B" {
		t.Fatal("new profile ignored current account")
	}
	prior, e := f.s.withParentRuntime(t.Context(), 1, order.OutTradeNo)
	if e != nil {
		t.Fatal(e)
	}
	if prior.OAuthProviders[id] == nil || prior.OAuthProviders[id].ApplicationID() != originalApp {
		t.Fatal("old authorization switched account")
	}
}
