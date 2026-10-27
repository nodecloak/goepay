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
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/platform/jobs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
)

type uncertainNoticeSender76 struct{ calls atomic.Int32 }

func (s *uncertainNoticeSender76) SendChannelStopped(_ context.Context, to string, channel, event int64, code string) error {
	s.calls.Add(1)
	if to != "local-admin@example.test" || channel != 7 || event <= 0 || code != "40004:PAYER_BALANCE_NOT_ENOUGH" {
		return ErrInvalid
	}
	return errors.New("synthetic uncertain SMTP DATA response")
}

func TestChannelStop76AuthenticatedAutoPayerFailurePersistsOnceAndDoesNotReleaseUnknownFunds(t *testing.T) {
	s, g, _, plan, authority, id := autoReady73(t)
	ctx := context.Background()
	merchant, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	var writes, queries atomic.Int32
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseForm(); e != nil {
			t.Error(e)
			return
		}
		params := map[string]string{}
		for k, v := range r.PostForm {
			if len(v) != 1 {
				t.Error("duplicate gateway field")
			}
			params[k] = v[0]
		}
		sig, e := adapter.SignAlipayRSA2(params, merchant)
		if e != nil || sig != params["sign"] {
			t.Error("unsigned original automatic request")
		}
		var biz map[string]any
		if json.Unmarshal([]byte(params["biz_content"]), &biz) != nil {
			t.Error("invalid synthetic gateway body")
		}
		p, e := s.SettlementPayout(ctx, id)
		if e != nil || p.SendState != "claimed" {
			t.Error("provider called before original claim", e)
		}
		if biz["out_biz_no"] != fmt.Sprintf("TF%d", p.TransferID) {
			t.Error("wrong original transfer number", biz["out_biz_no"])
		}
		node := map[string]any{}
		switch params["method"] {
		case "alipay.fund.trans.toaccount.transfer":
			writes.Add(1)
			node = map[string]any{"code": "40004", "sub_code": "PAYER_BALANCE_NOT_ENOUGH"}
		case "alipay.fund.trans.common.query":
			queries.Add(1)
			node = map[string]any{"code": "10000", "out_biz_no": biz["out_biz_no"], "trans_amount": "10.00", "order_id": "local-provider76", "status": "SUCCESS", "pay_date": "2026-10-24 12:00:00"}
		default:
			t.Error("unexpected gateway method", params["method"])
		}
		raw, _ := json.Marshal(node)
		d := sha256.Sum256(raw)
		signature, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, d[:])
		json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(params["method"], ".", "_") + "_response": json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(signature)})
	}))
	defer up.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "batch-test-app", SellerID: "2088000000000073", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client()})
	if e != nil {
		t.Fatal(e)
	}
	g.Providers[7] = ali
	g.ChannelAutoStopEnabled = true
	result, sendErr := g.ExecuteSettlementAuto(ctx, id, nil)
	if sendErr == nil || result.State != "unknown" || writes.Load() != 1 {
		t.Fatal("unverified finality or wrong submit count", result.State, writes.Load(), sendErr)
	}
	status, e := s.ChannelWriteStatus(ctx, 7)
	if e != nil || !status.Blocked || status.Code != "40004:PAYER_BALANCE_NOT_ENOUGH" || status.EventID <= 0 {
		t.Fatal("trusted automatic error failed to stop original channel", status, e)
	}
	statusTx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		t.Fatal(e)
	}
	withinTx, e := s.ChannelWriteStatusTx(ctx, statusTx, 7)
	if e != nil || withinTx.ChannelID != status.ChannelID || withinTx.EventID != status.EventID || withinTx.Revision != status.Revision || withinTx.Blocked != status.Blocked || withinTx.Code != status.Code {
		statusTx.Rollback(ctx)
		t.Fatal("routing transaction changed authenticated stop", withinTx, e)
	}
	if e = statusTx.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	var sources, jobs int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM channel_stop_events),(SELECT count(*) FROM jobs WHERE kind='admin.channel-stopped')`).Scan(&sources, &jobs); e != nil || sources != 1 || jobs != 1 {
		t.Fatal("pause/notice not exactly once", sources, jobs, e)
	}
	if e = s.recordAutomaticChannelFailure(ctx, id, sendErr); e != nil {
		t.Fatal(e)
	}
	in := authority.SettlementAutoInput
	in.ExpectedAuthorizationID = authority.ID
	newGrant := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { return nil })
	if _, e = g.AuthorizeSettlementAuto(newGrant, plan.ID, plan.Revision, in, settlementPlanAdmin); !errors.Is(e, ErrChannelStopped) {
		t.Fatal("stopped channel obtained replacement automatic authority", e)
	}
	balance, e := s.Balance(ctx, 7395)
	if e != nil || balance.Available != 1000 || balance.Reserved != 1000 {
		t.Fatal("classification released uncertain original gross", balance, e)
	}
	if _, e = g.Transfer(ctx, 7395, 7, "blocked-after-error76", adapter.TransferRequest{Type: "alipay", Account: "local-recipient@example.test", Name: "Local Recipient", Amount: 100, Remark: "local only"}); !errors.Is(e, ErrChannelStopped) || writes.Load() != 1 {
		t.Fatal("stopped channel created another provider write", e, writes.Load())
	}
	order, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 7395, OutTradeNo: "blocked-payment76", Type: "alipay", Name: "Local blocked payment", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	g.CallbackBaseURL = "https://local-pay.example.test"
	if _, e = g.Submit(ctx, order.TradeNo, 7, "wap", "192.0.2.76", ""); !errors.Is(e, ErrChannelStopped) {
		t.Fatal("stopped channel allowed new collection", e)
	}
	if _, e = g.SubmitPreauthorization(ctx, order.TradeNo, 7, "192.0.2.76", ""); !errors.Is(e, ErrChannelStopped) {
		t.Fatal("stopped channel allowed new freeze", e)
	}
	if _, e = g.SubmitScan(ctx, order.TradeNo, 7, "192.0.2.76", "28763443825664394"); !errors.Is(e, ErrChannelStopped) {
		t.Fatal("stopped channel allowed new barcode collection", e)
	}
	var attempts int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM provider_attempts WHERE order_id=$1`, order.ID).Scan(&attempts); e != nil || attempts != 0 {
		t.Fatal("blocked collection allocated an attempt", attempts, e)
	}
	freeze, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 7395, OutTradeNo: "stopped-existing-freeze76", Type: "alipay", Name: "Local original authorization", Amount: 125})
	if e != nil {
		t.Fatal(e)
	}
	legacy := &preauthFlowProvider{}
	account, e := adapter.TransferAccountScope(legacy)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.BeginProviderAttemptWithAccount(ctx, freeze.TradeNo, 7, "preauth", "", "", account); e != nil {
		t.Fatal(e)
	}
	frozenFlow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: legacy}, AllowLive: true, CallbackBaseURL: "https://local-pay.example.test"}
	if e = frozenFlow.HandlePreauthorizationNotification(ctx, 7, freeze.TradeNo, nil, []byte("valid-authorization")); e != nil {
		t.Fatal("verified freeze callback lost during stop", e)
	}
	pausedAuth, e := s.Preauthorization(ctx, freeze.TradeNo)
	if e != nil || pausedAuth.CaptureState != "none" || legacy.captures != 0 {
		t.Fatal("paused callback created new capture", pausedAuth, e, legacy.captures)
	}
	if _, e = frozenFlow.CapturePreauthorization(ctx, 7395, freeze.TradeNo); !errors.Is(e, ErrChannelStopped) || legacy.captures != 0 {
		t.Fatal("paused manual capture created first write", e, legacy.captures)
	}
	if e = s.RevokeSettlementAuto(ctx, authority.ID, settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	g.AllowLive = false
	result, e = g.QuerySettlementPayout(ctx, id, settlementAdmin)
	if e != nil || result.State != "succeeded" || queries.Load() != 1 || writes.Load() != 1 {
		t.Fatal("pause blocked original read-only recovery", result.State, e, queries.Load(), writes.Load())
	}
	status, e = s.ChannelWriteStatus(ctx, 7)
	if e != nil || !status.Blocked {
		t.Fatal("later success silently reenabled original channel", status, e)
	}
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM channel_stop_events),(SELECT count(*) FROM jobs WHERE kind='admin.channel-stopped')`).Scan(&sources, &jobs); e != nil || sources != 1 || jobs != 1 {
		t.Fatal("replay duplicated pause or notice", sources, jobs, e)
	}
	q := jobsQueue76(s)
	job, e := q.Claim(ctx, []string{"admin.channel-stopped"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	sender := &uncertainNoticeSender76{}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.DeliverChannelStopNotice(ctx, job.ID, job.Token, "local-admin@example.test", sender); e != nil {
				t.Error("valid notice dispatch rejected", e)
			}
		}()
	}
	wg.Wait()
	if sender.calls.Load() != 1 || writes.Load() != 1 {
		t.Fatal("notice retried or initiated another payment", sender.calls.Load(), writes.Load())
	}
	status, e = s.ChannelWriteStatus(ctx, 7)
	if e != nil || status.NoticeState != "unknown" {
		t.Fatal("uncertain SMTP claimed as delivered or missing receipt", status, e)
	}
	if e = s.DeliverChannelStopNotice(ctx, job.ID, job.Token, "other-admin@example.test", sender); !errors.Is(e, ErrConflict) {
		t.Fatal("claimed recipient silently replaced", e)
	}
	// Restoration is a separate, current, non-delegated super session and an
	// independent confirmation of this exact stop, never a retry of the TF.
	token := strings.Repeat("r", 43)
	if _, e = s.pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class)VALUES('channel-resume76','admin',7601,'Local resume','unused','super')`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `INSERT INTO console_sessions(token_hash,username,role,subject_id,csrf,expires_at,auth_version)VALUES($1,'channel-resume76','admin',7601,'local',clock_timestamp()+interval '1 hour',1)`, lookupSessionHash(token)); e != nil {
		t.Fatal(e)
	}
	actor := SettlementPlanActor{Role: "admin", Class: "super", ID: 7601, Name: "channel-resume76"}
	if _, e = s.ResumeChannel(ctx, 7, status.EventID, status.Revision, token, actor); !errors.Is(e, ErrInvalid) {
		t.Fatal("resume accepted missing independent confirmation", e)
	}
	uses := 0
	guarded := WithFundsActionGuard(ctx, func(_ context.Context, _ pgx.Tx, kind, key string) error {
		if kind != "channel-resume" || key != fmt.Sprintf("7:%d:%d", status.EventID, status.Revision) {
			return ErrInvalid
		}
		uses++
		return nil
	})
	bad := actor
	bad.Class = "operator"
	if _, e = s.ResumeChannel(guarded, 7, status.EventID, status.Revision, token, bad); !errors.Is(e, ErrInvalid) || uses != 0 {
		t.Fatal("operator resumed", e, uses)
	}
	if _, e = s.ResumeChannel(guarded, 7, status.EventID, status.Revision, strings.Repeat("z", 43), actor); !errors.Is(e, ErrInvalid) || uses != 0 {
		t.Fatal("caller actor bypassed original session", e, uses)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE console_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE token_hash=$1`, lookupSessionHash(token)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ResumeChannel(guarded, 7, status.EventID, status.Revision, token, actor); !errors.Is(e, ErrInvalid) || uses != 0 {
		t.Fatal("expired original session restored channel", e, uses)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE console_sessions SET expires_at=clock_timestamp()+interval '1 hour' WHERE token_hash=$1`, lookupSessionHash(token)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE SEQUENCE channel76_resume_audit_seen; CREATE FUNCTION channel76_resume_delay()RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE deadline timestamptz;BEGIN IF NEW.resource='channel-stops' AND NEW.operation='resume' THEN PERFORM nextval('channel76_resume_audit_seen');SELECT expires_at INTO deadline FROM console_sessions WHERE username='channel-resume76';PERFORM pg_sleep(GREATEST(0,extract(epoch FROM(deadline-clock_timestamp())))+0.05);END IF;RETURN NEW;END $$; CREATE TRIGGER channel76_resume_delay BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION channel76_resume_delay()`); e != nil {
		t.Fatal(e)
	}
	expiring := WithFundsActionGuard(ctx, func(c context.Context, tx pgx.Tx, kind, key string) error {
		_, e := tx.Exec(c, `UPDATE console_sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE token_hash=$1`, lookupSessionHash(token))
		return e
	})
	if _, e = s.ResumeChannel(expiring, 7, status.EventID, status.Revision, token, actor); !errors.Is(e, ErrInvalid) {
		t.Fatal("session expired at final audit wait still committed a resume", e)
	}
	var reached bool
	if e = s.pool.QueryRow(ctx, `SELECT is_called FROM channel76_resume_audit_seen`).Scan(&reached); e != nil || !reached {
		t.Fatal("expiry fixture never reached actual resume audit", reached, e)
	}
	if _, e = s.pool.Exec(ctx, `DROP TRIGGER channel76_resume_delay ON backoffice_audit;DROP FUNCTION channel76_resume_delay()`); e != nil {
		t.Fatal(e)
	}
	still, e := s.ChannelWriteStatus(ctx, 7)
	if e != nil || !still.Blocked || still.EventID != status.EventID || still.Revision != 1 {
		t.Fatal("expired resume left committed history", still, e)
	}
	if _, e = s.ResumeChannel(guarded, 7, status.EventID+1, status.Revision, token, actor); !errors.Is(e, ErrConflict) || uses != 0 {
		t.Fatal("stale stop resumed", e, uses)
	}
	restored, e := s.ResumeChannel(guarded, 7, status.EventID, status.Revision, token, actor)
	if e != nil || restored.Blocked || restored.Revision != 2 || uses != 1 {
		t.Fatal("explicit resume failed", restored, e, uses)
	}
	if e = s.recordAutomaticChannelFailure(ctx, id, sendErr); e != nil {
		t.Fatal(e)
	}
	restored, e = s.ChannelWriteStatus(ctx, 7)
	if e != nil || restored.Blocked || restored.Revision != 2 || writes.Load() != 1 {
		t.Fatal("old failure restopped or resent", restored, e, writes.Load())
	}
}

func jobsQueue76(s *Store) jobs.Queue { return jobs.Queue{Pool: s.pool} }
