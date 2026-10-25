package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

func sharingChangedPaymentAccount(t *testing.T) (adapter.Provider, *forbidSharingReceiverTransport) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	blocked := &forbidSharingReceiverTransport{}
	p, e := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local-app", MerchantID: "another-mch", MerchantSerial: "LOCAL_MERCHANT_SERIAL", PrivateKey: key, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_EXECUTION": &key.PublicKey}, EnableChannelSharing: true, Methods: []string{"native"}, HTTPClient: &http.Client{Transport: blocked}})
	if e != nil {
		t.Fatal(e)
	}
	return p, blocked
}

func TestChannelSharingExecutionChangedAccountBeforeClaimAndIOAndReadOnlyQuery(t *testing.T) {
	for _, boundary := range []string{"before-claim", "before-send", "before-query"} {
		t.Run(boundary, func(t *testing.T) {
			f := newSharingExecutionFixture(t)
			ctx := context.Background()
			o := f.paid(t, "sharing-identity-"+boundary, false)
			original := f.Flow.Providers[7]
			changed, blocked := sharingChangedPaymentAccount(t)
			switch boundary {
			case "before-claim":
				f.Flow.Providers[7] = changed
				if _, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "identity-sharing", channelSharingActor); !errors.Is(e, ErrConflict) {
					t.Fatal(e)
				}
				var count int
				if e := f.Store.pool.QueryRow(ctx, "SELECT count(*) FROM channel_sharing_orders").Scan(&count); e != nil || count != 0 {
					t.Fatal("changed account created a PS intent", count, e)
				}
			case "before-send":
				ctx = WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { f.Flow.Providers[7] = changed; return nil })
				if _, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "identity-sharing", channelSharingActor); !errors.Is(e, ErrConflict) {
					t.Fatal(e)
				}
				r, e := f.Store.ChannelSharingOrder(ctx, 751, "identity-sharing", channelSharingActor)
				if e != nil || r.State != "failed" {
					t.Fatal("unsent rejection lacked its authenticated failure proof", r, e)
				}
			case "before-query":
				if _, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "identity-sharing", channelSharingActor); e != nil {
					t.Fatal(e)
				}
				f.Flow.Providers[7] = changed
				f.Flow.AllowLive = false
				if _, e := f.Flow.QueryChannelSharing(ctx, 751, "identity-sharing", channelSharingActor); !errors.Is(e, ErrConflict) {
					t.Fatal(e)
				}
				if f.Queries.Load() != 0 || f.Sends.Load() != 1 {
					t.Fatal("changed account queried original upstream intent", f.Queries.Load(), f.Sends.Load())
				}
				f.Flow.Providers[7] = original
				f.Phase.Store(1)
				if final, e := f.Flow.QueryChannelSharing(ctx, 751, "identity-sharing", channelSharingActor); e != nil || !final.FundsReleased {
					t.Fatal("restored original account could not reconcile", final, e)
				}
			}
			if blocked.calls.Load() != 0 {
				t.Fatal("changed account reached network", blocked.calls.Load())
			}
			if boundary != "before-query" && f.Sends.Load() != 0 {
				t.Fatal("changed account sent financial request", f.Sends.Load())
			}
		})
	}
}
