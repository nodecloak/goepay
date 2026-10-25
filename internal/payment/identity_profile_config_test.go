package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"strings"
	"testing"
)

func TestIdentityProfile64SameAppAlipayPhysicalChannelChangeRejectsOriginal(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	token := strings.Repeat("a", 43)
	lookupTestSession(t, s, token, "profile-account64", 972)
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	pub, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	private, public := base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key)), base64.StdEncoding.EncodeToString(pub)
	build := func(seller, credential string) IdentityProfileBinding {
		imp := channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{{ID: 7, Plugin: "alipay", AppTypes: []string{"1"}, Config: map[string]string{"appid": "original-pay-app", "appmchid": seller, "appsecret": credential, "appkey": public}}}, IdentityProfiles: []channelconfig.IdentityProfileSpec{{ID: 11, Revision: 1, ChannelID: 7, Enabled: true, Provider: "alipay", Kind: "mini", AppID: "independent-app", AccountMode: "direct", Path: "pages/identity/index", MerchantIDs: []int64{972}, Config: map[string]string{"appsecret": private, "appkey": public}}}}
		if _, e := channelconfig.Build(imp, false); e != nil {
			t.Fatal("linked payment config must remain valid", e)
		}
		profiles, e := channelconfig.BuildIdentityProfiles(imp, true)
		if e != nil {
			t.Fatal(e)
		}
		p := profiles[11]
		return IdentityProfileBinding{MerchantID: 972, ChannelID: 7, ProfileID: p.ID, ProfileRevision: p.Revision, Provider: p.Provider, AppID: p.AppID, Kind: p.Kind, Path: p.Path, Fingerprint: p.Fingerprint}
	}
	original := build("2088123412341234", private)
	for _, change := range []struct{ name, seller, credential string }{{"seller", "2088987698769876", private}, {"credential", "2088123412341234", private + "\n"}} {
		t.Run(change.name, func(t *testing.T) {
			q, e := s.CreateProfileIdentityLookup(ctx, token, "merchant", original)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.ConsumeProfileIdentityLookup(ctx, q.ID, q.State, "", "account-code-"+change.name, original); e != nil {
				t.Fatal(e)
			}
			if e = s.CompleteProfileIdentityLookup(ctx, q.ID, provider.OAuthIdentity{Provider: original.Provider, AppID: original.AppID, SubjectID: "local-identity"}, original); e != nil {
				t.Fatal(e)
			}
			current := build(change.seller, change.credential)
			if _, e = s.ReadProfileIdentityLookup(ctx, q.ID, token, "merchant", current); e == nil {
				t.Fatal("original request survived changed physical channel")
			}
			if _, e = s.RevealProfileIdentityLookup(ctx, q.ID, token, "merchant", original); e == nil {
				t.Fatal("restoring old channel revived original result")
			}
		})
	}
}
