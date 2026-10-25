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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type routedNotice77 struct {
	calls           atomic.Int32
	mode, scope, to string
	fail            bool
}

func (r *routedNotice77) ChannelNoticeMode() string  { return r.mode }
func (r *routedNotice77) ChannelNoticeScope() string { return r.scope }
func (r *routedNotice77) ValidateChannelNoticeRecipient(to string) error {
	if to != r.to {
		return ErrInvalid
	}
	return nil
}
func (r *routedNotice77) SendChannelStopped(ctx context.Context, to string, channel, event int64, code string) error {
	r.calls.Add(1)
	if e := r.ValidateChannelNoticeRecipient(to); e != nil {
		return e
	}
	if channel != 7 || event <= 0 || code != "40004:PAYER_BALANCE_NOT_ENOUGH" {
		return ErrInvalid
	}
	if r.fail {
		return errors.New("synthetic uncertain external acceptance77")
	}
	return nil
}

func noticeSource77(t *testing.T) (*Store, int64, string, int64) {
	t.Helper()
	return noticeSourceStore77(t, testStore(t))
}

func noticeSourceStore77(t *testing.T, store *Store) (*Store, int64, string, int64) {
	t.Helper()
	s, g, _, _, _, id := autoReadyStore73(t, store)
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	merchant, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := []byte(`{"code":"40004","sub_code":"PAYER_BALANCE_NOT_ENOUGH"}`)
		d := sha256.Sum256(raw)
		sig, e := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, d[:])
		if e != nil {
			t.Error(e)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"alipay_fund_trans_toaccount_transfer_response": json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(sig)})
	}))
	t.Cleanup(up.Close)
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "batch-test-app", SellerID: "2088000000000073", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client()})
	if e != nil {
		t.Fatal(e)
	}
	g.Providers[7] = ali
	g.ChannelAutoStopEnabled = true
	if _, e = g.ExecuteSettlementAuto(context.Background(), id, nil); e == nil {
		t.Fatal("signed fixture did not reject")
	}
	status, e := s.ChannelWriteStatus(context.Background(), 7)
	if e != nil || !status.Blocked {
		t.Fatal(status, e)
	}
	q := jobsQueue76(s)
	job, e := q.Claim(context.Background(), []string{"admin.channel-stopped"}, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	return s, job.ID, job.Token, status.EventID
}

func TestChannelNotice77RouteBoundOnceBeforeAnyIOAndNeverFallsBackAfterUnknown(t *testing.T) {
	s, job, lease, event := noticeSource77(t)
	ctx := context.Background()
	r := &routedNotice77{mode: "telegram", scope: strings.Repeat("a", 64), to: "-12345", fail: true}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.DeliverChannelStopNotice(ctx, job, lease, r.to, r); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if r.calls.Load() != 1 {
		t.Fatal("route failed to acquire exactly one permanent send claim", r.calls.Load())
	}
	v, e := s.readChannelNoticeClaim(ctx, s.pool, event)
	if e != nil || v.State != "unknown" {
		t.Fatal(v.State, e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `SET LOCAL bytea_output='escape'`); e != nil {
		t.Fatal(e)
	}
	_, readErr := s.readChannelNoticeClaim(ctx, tx, event)
	_ = tx.Rollback(ctx)
	if readErr != nil {
		t.Fatal("valid route proof depends on PostgreSQL bytea output encoding", readErr)
	}
	var version int
	var mode string
	var scope, route []byte
	if e = s.pool.QueryRow(ctx, `SELECT route_version,route_mode,route_scope_hash,route_hash FROM channel_stop_notice_claims WHERE event_id=$1`, event).Scan(&version, &mode, &scope, &route); e != nil || version != 1 || mode != "telegram" || len(scope) != 32 || len(route) != 32 {
		t.Fatal("route proof not persisted", version, mode, e)
	}
	for _, replacement := range []*routedNotice77{
		{mode: "qqbot", scope: r.scope, to: r.to},
		{mode: r.mode, scope: strings.Repeat("b", 64), to: r.to},
		{mode: r.mode, scope: r.scope, to: "-54321"},
	} {
		if e = s.DeliverChannelStopNotice(ctx, job, lease, replacement.to, replacement); !errors.Is(e, ErrConflict) || replacement.calls.Load() != 0 {
			t.Fatal("unknown claim reopened with replacement route", e)
		}
	}
	old := &uncertainNoticeSender76{}
	if e = s.DeliverChannelStopNotice(ctx, job, lease, "local-admin@example.test", old); !errors.Is(e, ErrConflict) || old.calls.Load() != 0 {
		t.Fatal("route downgraded to legacy SMTP", e)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidChannelStops != 0 {
		t.Fatal(report, e)
	}
	if _, e = s.pool.Exec(ctx, `BEGIN; SET LOCAL session_replication_role=replica; UPDATE channel_stop_notice_claims SET route_version=0,route_mode='',route_scope_hash=NULL,route_hash=NULL;COMMIT`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChannelWriteStatus(ctx, 7); !errors.Is(e, ErrConflict) {
		t.Fatal("route proof deletion bypassed HMAC", e)
	}
	report, e = s.CheckLedger(ctx)
	if e != nil || report.InvalidChannelStops == 0 {
		t.Fatal("checker missed route downgrade", report, e)
	}
}
