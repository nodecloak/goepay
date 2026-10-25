package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestChannelSharingRelationReferencesAlipayClaimsBothAuthenticatedAndDeclaredIdentity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 751, Name: "sharing-owner", APIKey: "local-ali-reference"}); e != nil {
		t.Fatal(e)
	}
	merchant, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if e := r.ParseForm(); e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		method := r.Form.Get("method")
		if method != "alipay.trade.royalty.relation.bind" && method != "alipay.trade.royalty.relation.unbind" {
			t.Error("unexpected local method", method)
			w.WriteHeader(500)
			return
		}
		node := []byte(`{"code":"10000","result_code":"SUCCESS"}`)
		hash := sha256.Sum256(node)
		sig, e := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, hash[:])
		if e != nil {
			t.Error(e)
			w.WriteHeader(500)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(method, ".", "_") + "_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(sig)})
	}))
	defer up.Close()
	build := func(app, seller string) *adapter.Alipay {
		t.Helper()
		p, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: app, SellerID: seller, PrivateKey: merchant, PublicKey: &platform.PublicKey, EnableChannelSharing: true, GatewayURL: up.URL, HTTPClient: up.Client(), Methods: []string{"app"}})
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	p := build("local-ali-app", "2088000000000009")
	g := ProviderFlow{Store: s, AllowLive: true, Providers: map[int64]adapter.Provider{7: p, 8: build("local-ali-app", "2088000000000010"), 9: build("another-ali-app", "2088000000000009"), 10: p}}
	input := adapter.SharingReceiver{Type: "userId", Account: "2088000000000001"}
	ids := map[int64]int64{}
	for channel, no := range map[int64]string{7: "ali-reference-original", 8: "ali-reference-changed-seller", 9: "ali-reference-another-app", 10: "ali-reference-same-config"} {
		r, e := g.CreateChannelSharingReceiver(ctx, 751, channel, no, input, channelSharingActor)
		if e != nil {
			t.Fatal("a draft claimed an upstream identity", channel, e)
		}
		ids[channel] = r.ID
	}
	if _, e := g.BindChannelSharingReceiver(ctx, 751, ids[7], "ali-reference-original-bind", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	for _, channel := range []int64{8, 9} {
		if _, e := g.BindChannelSharingReceiver(ctx, 751, ids[channel], "ali-reference-forbidden-bind", channelSharingActor); !errors.Is(e, ErrConflict) || calls.Load() != 1 {
			t.Fatal("unverified identity alias escaped the original registry", channel, e, calls.Load())
		}
	}
	r, e := g.BindChannelSharingReceiver(ctx, 751, ids[10], "ali-reference-local-bind", channelSharingActor)
	if e != nil || r.Operation == nil || r.Operation.Source != "reference" || calls.Load() != 1 {
		t.Fatal("exact Alipay configuration did not reuse the original remote proof", r, e, calls.Load())
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 751, ids[10], "ali-reference-local-release", channelSharingActor); e != nil || calls.Load() != 1 {
		t.Fatal(e, calls.Load())
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 751, ids[7], "ali-reference-last-release", channelSharingActor); e != nil || calls.Load() != 2 {
		t.Fatal(e, calls.Load())
	}
}
