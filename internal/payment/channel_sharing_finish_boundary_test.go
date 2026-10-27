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
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type forbiddenFinishTransport struct{ calls *atomic.Int32 }

func (r forbiddenFinishTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls.Add(1)
	return nil, errors.New("local forbidden account I/O")
}
func TestChannelSharingFinishOriginalAccountCannotBeReplacedBeforeOrAfterClaim(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "after"}[claimed], func(t *testing.T) {
			f := newSharingFinishFixture(t)
			ctx := context.Background()
			if claimed {
				if _, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "finish-account", channelSharingActor); e != nil {
					t.Fatal(e)
				}
			}
			key, _ := rsa.GenerateKey(rand.Reader, 2048)
			var forbidden atomic.Int32
			p, e := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local-app", MerchantID: "foreign-mch", MerchantSerial: "FOREIGN_LOCAL_SERIAL", PrivateKey: key, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_LOCAL": &key.PublicKey}, EnableChannelSharing: true, Methods: []string{"native"}, HTTPClient: &http.Client{Transport: forbiddenFinishTransport{&forbidden}}})
			if e != nil {
				t.Fatal(e)
			}
			f.Flow.Providers[7] = p
			if claimed {
				if _, e = f.Flow.QueryChannelSharingFinish(ctx, 751, "finish-account", channelSharingActor); !errors.Is(e, ErrConflict) {
					t.Fatal(e)
				}
			} else {
				if _, e = f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "finish-account", channelSharingActor); !errors.Is(e, ErrConflict) {
					t.Fatal(e)
				}
			}
			if forbidden.Load() != 0 || f.Sends.Load() != map[bool]int32{false: 0, true: 1}[claimed] {
				t.Fatal("account replacement reached wrong financial endpoint", forbidden.Load(), f.Sends.Load())
			}
			b, e := f.Store.Balance(ctx, 751)
			if e != nil || b.Available != 0 || b.Reserved != 100 {
				t.Fatal(b, e)
			}
		})
	}
}
func TestChannelSharingFinishTrustedDefiniteRejectionCanBeExplicitlyRetried(t *testing.T) {
	f := newSharingFinishFixture(t)
	ctx := context.Background()
	f.Phase.Store(8)
	first, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "finish-definite-rejection", channelSharingActor)
	if e == nil || first.State != "failed" || first.ID <= 0 {
		t.Fatal(first, e)
	}
	preview, e := f.Flow.PreviewChannelSharingFinish(ctx, 751, f.Order.TradeNo, channelSharingActor)
	if e != nil || preview.State != "eligible" || preview.PreviousFailedID != first.ID {
		t.Fatal(preview, e)
	}
	f.Phase.Store(0)
	next, e := f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "finish-explicit-new", channelSharingActor)
	if e != nil || next.ID == first.ID || next.OutNo == first.OutNo || next.PreviousFailedID != first.ID {
		t.Fatal(next, e)
	}
	f.Phase.Store(1)
	final, e := f.Flow.QueryChannelSharingFinish(ctx, 751, "finish-explicit-new", channelSharingActor)
	if e != nil || !final.FundsReleased {
		t.Fatal(final, e)
	}
}
func TestChannelSharingFinishReleaseEndsOriginalPaymentReceiverReference(t *testing.T) {
	f := newSharingFinishFixture(t)
	ctx := context.Background()
	rule, e := f.Store.ChannelSharingRule(ctx, 751, 7, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Store.DisableChannelSharingRule(ctx, 751, 7, rule.ID, channelSharingActor); e != nil {
		t.Fatal(e)
	}
	receiver := rule.Items[0].ReceiverID
	if _, e = f.Flow.UnbindChannelSharingReceiver(ctx, 751, receiver, "finish-receiver-before", channelSharingActor); !errors.Is(e, ErrState) || f.ReceiverDeletes.Load() != 0 {
		t.Fatal(e, f.ReceiverDeletes.Load())
	}
	if _, e = f.Flow.SubmitChannelSharingFinish(ctx, 751, f.Order.TradeNo, "finish-receiver", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	f.Phase.Store(1)
	if _, e = f.Flow.QueryChannelSharingFinish(ctx, 751, "finish-receiver", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	v, e := f.Flow.UnbindChannelSharingReceiver(ctx, 751, receiver, "finish-receiver-after", channelSharingActor)
	if e != nil || v.State != "unbound" || f.ReceiverDeletes.Load() != 1 {
		t.Fatal(v, e, f.ReceiverDeletes.Load())
	}
}
func TestChannelSharingFinishAlipayStandaloneIsClosedBeforeGrantAndProviderIO(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 751, Name: "local ali finish", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	merchant, _ := rsa.GenerateKey(rand.Reader, 2048)
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	var calls atomic.Int32
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if e := r.ParseForm(); e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		if r.Form.Get("method") != "alipay.trade.royalty.relation.bind" {
			t.Error("standalone Alipay PF must not be called", r.Form.Get("method"))
			w.WriteHeader(500)
			return
		}
		raw := []byte(`{"code":"10000","result_code":"SUCCESS"}`)
		hash := sha256.Sum256(raw)
		sig, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, hash[:])
		json.NewEncoder(w).Encode(map[string]any{"alipay_trade_royalty_relation_bind_response": json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(sig)})
	}))
	t.Cleanup(up.Close)
	p, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "local-ali-finish-app", SellerID: "2088000000000009", PrivateKey: merchant, PublicKey: &platform.PublicKey, EnableChannelSharing: true, GatewayURL: up.URL, HTTPClient: up.Client(), Methods: []string{"app"}})
	if e != nil {
		t.Fatal(e)
	}
	g := ProviderFlow{Store: s, AllowLive: true, Providers: map[int64]adapter.Provider{7: p}, CallbackBaseURL: "https://local.example.invalid"}
	receiver, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "ali-finish-receiver", adapter.SharingReceiver{Type: "userId", Account: "2088000000000001"}, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, receiver.ID, "ali-finish-bind", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if _, e = g.SetChannelSharingRule(ctx, 751, 7, ChannelSharingRuleInput{Enabled: true, Methods: []string{"app"}, Items: []ChannelSharingRuleItem{{ReceiverID: receiver.ID, Mode: "rate", RateBPS: 2000}}}, 0, channelSharingActor); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "ali-finish-unsupported", Name: "local standalone finish", Type: "alipay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.Submit(ctx, o.TradeNo, 7, "app", "127.0.0.1", ""); e != nil {
		t.Fatal(e)
	}
	if e = s.markPaid(ctx, o.TradeNo, "ali-finish-original-receipt", true, PaidReceipt{RealMoney: 100, ChannelID: 7, ProviderTradeNo: "ALI_" + o.TradeNo}); e != nil {
		t.Fatal(e)
	}
	guarded := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error {
		t.Error("unsupported standalone PF consumed authorization")
		return ErrInvalid
	})
	if _, e = g.PreviewChannelSharingFinish(ctx, 751, o.TradeNo, channelSharingActor); !errors.Is(e, adapter.ErrUnsupported) {
		t.Fatal(e)
	}
	if _, e = g.SubmitChannelSharingFinish(guarded, 751, o.TradeNo, "ali-finish-closed", channelSharingActor); !errors.Is(e, adapter.ErrUnsupported) {
		t.Fatal(e)
	}
	if calls.Load() != 1 {
		t.Fatal("Alipay unsupported path performed extra I/O", calls.Load())
	}
	b, e := s.Balance(ctx, 751)
	if e != nil || b.Available != 0 || b.Reserved != 100 {
		t.Fatal(b, e)
	}
}
