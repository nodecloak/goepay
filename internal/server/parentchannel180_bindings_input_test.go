package server

import (
	"encoding/json"
	"goepay/internal/channelconfig"
	"goepay/internal/payment"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestParentChannel180OriginalWeixinAssociationInput(t *testing.T) {
	for _, raw := range []string{`{"appwxmp":"7","appwxa":"9007199254740993"}`, `{"appwxmp":"0","appwxa":"0"}`} {
		in, err := parentAccountChange(json.RawMessage(raw))
		if err != nil {
			t.Fatal("original appwxmp/appwxa selector rejected", err)
		}
		value := reflect.ValueOf(in)
		for _, name := range []string{"AppWXMP", "AppWXA"} {
			if !value.FieldByName(name).IsValid() {
				t.Fatal("association was discarded", name)
			}
		}
	}
	for _, raw := range []string{`{"appwxmp":7}`, `{"appwxmp":"07"}`, `{"appwxa":"-1"}`, `{"appwxa":"9223372036854775808"}`, `{"appwxmp":"0","Appwxmp":"7"}`, `{"appwxmp":"7","appwxmp":"8"}`, `{"appwxmp":null}`} {
		if _, err := parentAccountChange(json.RawMessage(raw)); err == nil {
			t.Fatal("ambiguous original association accepted", raw)
		}
	}
}

func TestParentChannel180WeixinBindingCatalogIsTrustedCurrentAndReadOnly(t *testing.T) {
	setup := parent180RuntimeReady(t)
	f := setup.f
	old := f.s.ProviderFlow.ParentFactories[8].(*parent180TLSFactory)
	spec := channelconfig.IdentityProfileSpec{ID: 9007199254740993, Revision: 1, ChannelID: 8, Enabled: true, Provider: "wxpay", Kind: "web", AppID: "synthetic-catalog-web", AccountMode: "direct", MerchantIDs: channelconfig.IdentityMerchantIDs{1}, Config: map[string]string{"appsecret": "synthetic-never-return-secret"}}
	factory, err := channelconfig.NewParentChannelFactory(old.Bootstrap(), channelconfig.ParentBuildOptions{IdentityProfiles: []channelconfig.IdentityProfileSpec{spec}})
	if err != nil {
		t.Fatal(err)
	}
	old.ParentChannelFactory = factory
	for _, id := range []string{"8", setup.id} {
		w := parent180HTTP(f, "GET", "/"+id+"/weixin-options", "")
		if w.Code != 200 {
			session, _ := f.s.Sessions.Get(f.token, "admin")
			channel, _ := strconv.ParseInt(id, 10, 64)
			_, domainError := f.s.Store.ParentChannelBindingCatalog(t.Context(), payment.ParentChannelActor{User: session.User, Token: f.token, CSRF: f.csrf}, channel, f.s.ProviderFlow.ParentFactories)
			t.Fatal("original selector options unavailable", w.Code, w.Body.String(), domainError)
		}
		var out struct {
			Data struct {
				Items       []struct{ ID, AppID string }
				Fingerprint string
				ChannelID   string `json:"channel_id"`
				SourceID    string `json:"source_channel_id"`
			}
		}
		if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.ChannelID != id || out.Data.SourceID != "8" || len(out.Data.Items) != 1 || out.Data.Items[0].ID != "9007199254740993" || out.Data.Items[0].AppID != spec.AppID || len(out.Data.Fingerprint) != 64 {
			t.Fatal("untrusted or rounded selector DTO", w.Body.String())
		}
		if strings.Contains(w.Body.String(), spec.Config["appsecret"]) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("secret or cacheable selector response")
		}
	}
	if setup.creates.Load() != 0 {
		t.Fatal("reading application options sent a payment")
	}
}
