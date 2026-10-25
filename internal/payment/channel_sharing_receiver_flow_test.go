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
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type sharingReceiverTLSTransport struct{ up *httptest.Server }

func (x sharingReceiverTLSTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "api.mch.weixin.qq.com" {
		return nil, adapter.ErrAuthentication
	}
	copy := r.Clone(r.Context())
	target, _ := url.Parse(x.up.URL)
	copy.URL.Scheme = target.Scheme
	copy.URL.Host = target.Host
	return x.up.Client().Transport.RoundTrip(copy)
}
func sharingReceiverFlowFixture(t *testing.T, observers ...func(*http.Request, map[string]json.RawMessage)) (*Store, ProviderFlow, *atomic.Int32, *atomic.Bool) {
	t.Helper()
	s := testStore(t)
	if e := s.CreateMerchant(context.Background(), Merchant{ID: 751, Name: "sharing-owner", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	merchant, _ := rsa.GenerateKey(rand.Reader, 2048)
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	calls, lost := &atomic.Int32{}, &atomic.Bool{}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if lost.Load() {
			w.WriteHeader(503)
			return
		}
		var input map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			t.Error("bad receiver request JSON")
			w.WriteHeader(400)
			return
		}
		for _, observer := range observers {
			observer(r, input)
		}
		var kind, account, relation string
		json.Unmarshal(input["type"], &kind)
		json.Unmarshal(input["account"], &account)
		json.Unmarshal(input["relation_type"], &relation)
		raw, _ := json.Marshal(map[string]string{"type": kind, "account": account, "relation_type": relation})
		if r.URL.Path == "/v3/pay/transactions/native" {
			raw = []byte(`{"code_url":"weixin://wxpay/bizpayurl?pr=local-pg-sharing"}`)
		}
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := "pg-sharing-nonce"
		hash := sha256.Sum256([]byte(stamp + "\n" + nonce + "\n" + string(raw) + "\n"))
		signature, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, hash[:])
		w.Header().Set("Wechatpay-Timestamp", stamp)
		w.Header().Set("Wechatpay-Nonce", nonce)
		w.Header().Set("Wechatpay-Serial", "PUB_KEY_ID_PG")
		w.Header().Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(signature))
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
	}))
	t.Cleanup(upstream.Close)
	p, e := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local-app", MerchantID: "original-mch", MerchantSerial: "MERCHANT_SERIAL", PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_PG": &platform.PublicKey}, EnableChannelSharing: true, HTTPClient: &http.Client{Transport: sharingReceiverTLSTransport{upstream}}})
	if e != nil {
		t.Fatal(e)
	}
	return s, ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true}, calls, lost
}

var channelSharingActor = TransferBatchActor{Role: "merchant", ID: 751, Name: "sharing-owner"}
var channelSharingReceiverInput = adapter.SharingReceiver{Type: "MERCHANT_ID", Account: "private-receiver-1234", Name: "Private Legal Name", RelationType: "SUPPLIER"}

func TestChannelSharingReceiverRealTLSClaimsOnceBeforeIOAndReplayNeverResends(t *testing.T) {
	s, g, calls, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	view, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "original-receiver", channelSharingReceiverInput, channelSharingActor)
	if e != nil || calls.Load() != 0 {
		t.Fatal(view, e)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := g.BindChannelSharingReceiver(ctx, 751, view.ID, "bind-original", channelSharingActor)
			var unknown *adapter.UnknownOutcome
			if e != nil && !errors.As(e, &unknown) {
				failures <- e
			}
		}()
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		t.Fatal(e)
	}
	latest, e := s.ChannelSharingReceiver(ctx, 751, view.ID, channelSharingActor)
	if e != nil || latest.State != "bound" || calls.Load() != 1 {
		t.Fatal(latest, e, calls.Load())
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, view.ID, "bind-original", channelSharingActor); e != nil || calls.Load() != 1 {
		t.Fatal("confirmed bind replay resent", e, calls.Load())
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 751, view.ID, "unbind-original", channelSharingActor); e != nil || calls.Load() != 2 {
		t.Fatal(e, calls.Load())
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 751, view.ID, "unbind-original", channelSharingActor); e != nil || calls.Load() != 2 {
		t.Fatal("confirmed unbind replay resent", e, calls.Load())
	}
	var operations, orders, journals int
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM channel_sharing_receiver_operations),(SELECT count(*) FROM orders),(SELECT count(*) FROM journals)").Scan(&operations, &orders, &journals); e != nil || operations != 2 || orders != 0 || journals != 0 {
		t.Fatal(operations, orders, journals, e)
	}
}
func TestChannelSharingReceiverUnknownCannotBeRearmedWithAnotherRequestNumber(t *testing.T) {
	s, g, calls, lost := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	view, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "unknown-receiver", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	lost.Store(true)
	if _, e = g.BindChannelSharingReceiver(ctx, 751, view.ID, "bind-unknown", channelSharingActor); e == nil {
		t.Fatal("missing upstream receipt confirmed bind")
	}
	lost.Store(false)
	for _, no := range []string{"bind-unknown", "new-unsafe-request"} {
		if _, e = g.BindChannelSharingReceiver(ctx, 751, view.ID, no, channelSharingActor); e == nil {
			t.Fatal("unknown receiver became a new send", no)
		}
	}
	current, e := s.ChannelSharingReceiver(ctx, 751, view.ID, channelSharingActor)
	if e != nil || current.State != "unknown" || calls.Load() != 1 {
		t.Fatal(current, e, calls.Load())
	}
}

type sharingReceiverErrorProvider struct {
	*adapter.WechatV3
	failure error
	before  func()
}

type forbidSharingReceiverTransport struct{ calls atomic.Int32 }

func (x *forbidSharingReceiverTransport) RoundTrip(*http.Request) (*http.Response, error) {
	x.calls.Add(1)
	return nil, errors.New("network forbidden in changed-account regression")
}
func TestChannelSharingReceiverSameAppChangedMerchantFailsBeforeClaimAndKeyRotationKeepsDraft(t *testing.T) {
	s, g, calls, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	original := g.Providers[7]
	view, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "scope-receiver", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	blocked := &forbidSharingReceiverTransport{}
	makeProvider := func(mch string) adapter.Provider {
		t.Helper()
		p, e := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local-app", MerchantID: mch, MerchantSerial: "ROTATED_SERIAL", PrivateKey: key, APIv3Key: []byte("abcdef0123456789abcdef0123456789"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_ROTATED": &key.PublicKey}, EnableChannelSharing: true, HTTPClient: &http.Client{Transport: blocked}})
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	g.Providers[7] = makeProvider("original-mch")
	same, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "scope-receiver", channelSharingReceiverInput, channelSharingActor)
	if e != nil || same.ID != view.ID {
		t.Fatal("key rotation rebound configuration identity", same, e)
	}
	g.Providers[7] = makeProvider("changed-mch")
	if _, e = g.BindChannelSharingReceiver(ctx, 751, view.ID, "scope-bind", channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal("changed merchant inherited old receiver", e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM channel_sharing_receiver_operations").Scan(&count); e != nil || count != 0 || calls.Load() != 0 || blocked.calls.Load() != 0 {
		t.Fatal("account mismatch reached claim or network", count, e, calls.Load(), blocked.calls.Load())
	}
	g.Providers[7] = original
	bound, e := g.BindChannelSharingReceiver(ctx, 751, view.ID, "scope-bind", channelSharingActor)
	if e != nil || bound.State != "bound" || calls.Load() != 1 {
		t.Fatal(bound, e, calls.Load())
	}
	if _, e = s.pool.Exec(ctx, "UPDATE channel_sharing_receiver_operations SET state='unsent',claimed_at=NULL"); e == nil {
		t.Fatal("completed receiver send barrier was reset")
	}
}

func (p *sharingReceiverErrorProvider) BindChannelSharingReceiver(ctx context.Context, no string, r adapter.SharingReceiver) error {
	if p.before != nil {
		p.before()
	}
	if p.failure != nil {
		return p.failure
	}
	return p.WechatV3.BindChannelSharingReceiver(ctx, no, r)
}
func TestChannelSharingReceiverSystemErrorRemainsUnknownAndAuditFailureNeverRearms(t *testing.T) {
	t.Run("system-error", func(t *testing.T) {
		s, g, _, _ := sharingReceiverFlowFixture(t)
		ctx := context.Background()
		wrapped := &sharingReceiverErrorProvider{WechatV3: g.Providers[7].(*adapter.WechatV3), failure: &adapter.ProviderError{Code: "20000:ACQ.SYSTEM_ERROR"}}
		g.Providers[7] = wrapped
		view, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "system-error-receiver", channelSharingReceiverInput, channelSharingActor)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = g.BindChannelSharingReceiver(ctx, 751, view.ID, "system-error-bind", channelSharingActor); e == nil {
			t.Fatal("system error accepted")
		}
		current, e := s.ChannelSharingReceiver(ctx, 751, view.ID, channelSharingActor)
		if e != nil || current.State != "unknown" {
			t.Fatal("system error must not establish definite failure", current, e)
		}
	})
	t.Run("audit-before-io", func(t *testing.T) {
		s, g, calls, _ := sharingReceiverFlowFixture(t)
		ctx := context.Background()
		view, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "audit-receiver", channelSharingReceiverInput, channelSharingActor)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.pool.Exec(ctx, `CREATE FUNCTION reject_sharing_audit_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='channel-sharing-receiver-operations' THEN RAISE EXCEPTION 'injected receiver audit failure'; END IF; RETURN NEW; END $$;CREATE TRIGGER reject_sharing_audit_test BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_sharing_audit_test()`); e != nil {
			t.Fatal(e)
		}
		if _, e = g.BindChannelSharingReceiver(ctx, 751, view.ID, "audit-bind", channelSharingActor); e == nil || calls.Load() != 0 {
			t.Fatal("audit failure sent a receiver mutation", e, calls.Load())
		}
		var count int
		s.pool.QueryRow(ctx, "SELECT count(*) FROM channel_sharing_receiver_operations").Scan(&count)
		if count != 0 {
			t.Fatal("failed claim left an operation")
		}
		if _, e = s.pool.Exec(ctx, "DROP TRIGGER reject_sharing_audit_test ON backoffice_audit"); e != nil {
			t.Fatal(e)
		}
		if _, e = g.BindChannelSharingReceiver(ctx, 751, view.ID, "audit-bind", channelSharingActor); e != nil || calls.Load() != 1 {
			t.Fatal(e, calls.Load())
		}
	})
	t.Run("audit-after-io", func(t *testing.T) {
		s, g, calls, _ := sharingReceiverFlowFixture(t)
		ctx := context.Background()
		original := g.Providers[7].(*adapter.WechatV3)
		g.Providers[7] = &sharingReceiverErrorProvider{WechatV3: original, before: func() {
			_, e := s.pool.Exec(ctx, `CREATE FUNCTION reject_sharing_result_audit_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='channel-sharing-receiver-operations' THEN RAISE EXCEPTION 'injected result audit failure'; END IF; RETURN NEW; END $$;CREATE TRIGGER reject_sharing_result_audit_test BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_sharing_result_audit_test()`)
			if e != nil {
				t.Error(e)
			}
		}}
		view, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "result-audit-receiver", channelSharingReceiverInput, channelSharingActor)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = g.BindChannelSharingReceiver(ctx, 751, view.ID, "result-audit-bind", channelSharingActor); e == nil || calls.Load() != 1 {
			t.Fatal(e, calls.Load())
		}
		current, e := s.ChannelSharingReceiver(ctx, 751, view.ID, channelSharingActor)
		if e != nil || current.State != "binding" || current.Operation == nil || current.Operation.State != "claimed" {
			t.Fatal("failed result audit committed success", current, e)
		}
		g.Providers[7] = original
		if _, e = g.BindChannelSharingReceiver(ctx, 751, view.ID, "result-audit-bind", channelSharingActor); e == nil || calls.Load() != 1 {
			t.Fatal("lost result audit rearmed send", e, calls.Load())
		}
	})
}
