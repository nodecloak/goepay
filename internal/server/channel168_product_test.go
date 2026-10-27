package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
)

type channel168NoNetwork struct{ calls atomic.Int64 }

func (x *channel168NoNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	x.calls.Add(1)
	return nil, errors.New("channel168 forbids all upstream I/O")
}

func TestChannel168WechatPaymentProductCannotRebindSameAccountAndRuntimeID(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	noNetwork := &channel168NoNetwork{}
	client := &http.Client{Transport: noNetwork}
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	v2, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "wx168app", MerchantID: "mch168", APIKey: "0123456789abcdef0123456789abcdef", Methods: []string{"native"}, HTTPClient: client})
	if e != nil {
		t.Fatal(e)
	}
	v3, e := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "wx168app", MerchantID: "mch168", MerchantSerial: "SERIAL168", PrivateKey: key, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUBLIC168": &key.PublicKey}, Methods: []string{"native"}, HTTPClient: client})
	if e != nil {
		t.Fatal(e)
	}
	f.s.ProviderFlow.Providers[7] = v2
	f.configure(t, 7, "99.50")
	p := compat.Params{"type": "wxpay", "method": "native", "name": "same account product168", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": "product168", "channel_id": "7"}
	if _, _, e = f.s.providerRoutedRequest(ctx, 1, p); e != nil {
		t.Fatal(e)
	}
	o, e := f.s.Store.CreateOrder(ctx, create168(p, 100))
	if e != nil {
		t.Fatal(e)
	}
	f.s.ProviderFlow.Providers[7] = v3
	if w := f.admin("GET", "/7", "", f.csrf); w.Code != 409 {
		t.Errorf("V2 policy silently rebound to V3 payment product: %d", w.Code)
	}
	if _, e = f.s.ProviderFlow.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.1", ""); !errors.Is(e, payment.ErrConflict) {
		t.Errorf("V2 quote submitted using V3 product: %v", e)
	}
	if noNetwork.calls.Load() != 0 {
		t.Errorf("product drift passed send ownership: %d", noNetwork.calls.Load())
	}
}
