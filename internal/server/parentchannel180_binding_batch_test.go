package server

import (
	"encoding/json"
	"goepay/internal/channelconfig"
	"strings"
	"testing"
)

func TestParentChannel180OriginalWeixinBatchBindsExactCASSelectionAtomically(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	old := f.s.ProviderFlow.ParentFactories[8].(*parent180TLSFactory)
	spec := channelconfig.IdentityProfileSpec{ID: 81, Revision: 1, ChannelID: 8, Enabled: true, Provider: "wxpay", Kind: "web", AppID: "synthetic-batch-web", AccountMode: "direct", MerchantIDs: channelconfig.IdentityMerchantIDs{1}, Config: map[string]string{"appsecret": "synthetic-batch-web-secret"}}
	factory, err := channelconfig.NewParentChannelFactory(old.Bootstrap(), channelconfig.ParentBuildOptions{IdentityProfiles: []channelconfig.IdentityProfileSpec{spec}})
	if err != nil {
		t.Fatal(err)
	}
	old.ParentChannelFactory = factory
	templates := parent180HTTP(f, "GET", "/templates", "")
	var choices struct {
		Data struct{ Items []map[string]any }
	}
	json.Unmarshal(templates.Body.Bytes(), &choices)
	copyBody := map[string]any{"source_channel_id": "8", "expected_source_fingerprint": choices.Data.Items[0]["fingerprint"], "request_id": "a234567890abcdef1234567890abcdef", "copy_from_channel_id": setup.id, "expected_copy_fingerprint": setup.state["fingerprint"], "name": "关联副本", "rate": "99.50", "mode": 0, "account": map[string]any{"app_types": []string{"1", "2"}}}
	copyRaw, _ := json.Marshal(copyBody)
	copyResult := parent180HTTP(f, "POST", "", string(copyRaw))
	if copyResult.Code != 200 {
		t.Fatal(copyResult.Code, copyResult.Body.String())
	}
	var copied struct{ Data map[string]any }
	json.Unmarshal(copyResult.Body.Bytes(), &copied)
	selectItem := func(row map[string]any, fp string) map[string]any {
		return map[string]any{"id": row["id"], "expected_fingerprint": fp, "expected_bindings_fingerprint": factory.ParentWeixinCatalog().Fingerprint}
	}
	staleBody, _ := json.Marshal(map[string]any{"type": 0, "weixin": "81", "items": []any{selectItem(setup.state, setup.state["fingerprint"].(string)), selectItem(copied.Data, strings.Repeat("f", 64))}})
	failed := parent180HTTP(f, "POST", "/bind-weixin", string(staleBody))
	if failed.Code != 409 {
		t.Fatal("batch selector unavailable or stale selection not rejected", failed.Code, failed.Body.String())
	}
	current := parent180HTTP(f, "GET", "/"+setup.id, "")
	var unchanged struct{ Data map[string]any }
	json.Unmarshal(current.Body.Bytes(), &unchanged)
	if unchanged.Data["fingerprint"] != setup.state["fingerprint"] {
		t.Fatal("first binding committed before stale second item")
	}
	body, _ := json.Marshal(map[string]any{"type": 0, "weixin": "81", "items": []any{selectItem(setup.state, setup.state["fingerprint"].(string)), selectItem(copied.Data, copied.Data["fingerprint"].(string))}})
	result := parent180HTTP(f, "POST", "/bind-weixin", string(body))
	if result.Code != 200 {
		t.Fatal("original batch association unavailable", result.Code, result.Body.String())
	}
	var out struct {
		Data struct {
			Items []struct {
				Account struct {
					AppWXMP string `json:"appwxmp"`
				}
			}
			Total int
		}
	}
	if json.Unmarshal(result.Body.Bytes(), &out) != nil || out.Data.Total != 2 || len(out.Data.Items) != 2 || out.Data.Items[0].Account.AppWXMP != "81" || out.Data.Items[1].Account.AppWXMP != "81" {
		t.Fatal("batch did not bind exact selection", result.Body.String())
	}
	if setup.creates.Load() != 0 {
		t.Fatal("binding configuration sent real payment")
	}
}
