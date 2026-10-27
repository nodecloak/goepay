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
	adapter "goepay/internal/adapters/payment"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type sharingExecutionFixture struct {
	Store          *Store
	Flow           ProviderFlow
	Phase          atomic.Int32 // 0 pending, 1 succeeded, 2 partial, 3 wrong amount, 4 lost receipt, 5 one success pending
	Remaining      atomic.Int64
	Sends, Queries atomic.Int32
	mu             sync.Mutex
	requests       map[string]struct {
		Trade     string
		Receivers []map[string]any
	}
}

func newSharingExecutionFixture(t *testing.T) *sharingExecutionFixture {
	t.Helper()
	ctx := context.Background()
	s := testStore(t)
	if e := s.CreateMerchant(ctx, Merchant{ID: 751, Name: "sharing-owner", APIKey: "local-only"}); e != nil {
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
	f := &sharingExecutionFixture{Store: s, requests: map[string]struct {
		Trade     string
		Receivers []map[string]any
	}{}}
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		input := map[string]json.RawMessage{}
		if r.Method == "POST" {
			if e := json.NewDecoder(r.Body).Decode(&input); e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
		}
		var out any
		switch {
		case strings.HasPrefix(r.URL.Path, "/v3/profitsharing/receivers/"):
			out = map[string]json.RawMessage{"type": input["type"], "account": input["account"], "relation_type": input["relation_type"]}
		case r.URL.Path == "/v3/pay/transactions/native":
			out = map[string]string{"code_url": "weixin://wxpay/bizpayurl?pr=local-financial-sharing"}
		case r.URL.Path == "/v3/profitsharing/orders" || strings.HasPrefix(r.URL.Path, "/v3/profitsharing/orders/"):
			var no, trade string
			var receivers []map[string]any
			if r.Method == "POST" {
				f.Sends.Add(1)
				json.Unmarshal(input["out_order_no"], &no)
				json.Unmarshal(input["transaction_id"], &trade)
				json.Unmarshal(input["receivers"], &receivers)
				var finish bool
				json.Unmarshal(input["unfreeze_unsplit"], &finish)
				if !finish || len(no) != 32 || !strings.HasPrefix(no, "PS") {
					t.Error("sharing request lost durable finish/operation identity")
				}
				f.mu.Lock()
				f.requests[no] = struct {
					Trade     string
					Receivers []map[string]any
				}{trade, receivers}
				f.mu.Unlock()
				if f.Phase.Load() == 4 {
					w.WriteHeader(503)
					return
				}
			} else {
				f.Queries.Add(1)
				no = strings.TrimPrefix(r.URL.Path, "/v3/profitsharing/orders/")
				f.mu.Lock()
				original, ok := f.requests[no]
				f.mu.Unlock()
				if !ok || r.URL.Query().Get("transaction_id") != original.Trade {
					t.Error("query changed original PS or payment")
					w.WriteHeader(400)
					return
				}
				trade, receivers = original.Trade, original.Receivers
			}
			state := "PROCESSING"
			if f.Phase.Load() != 0 && f.Phase.Load() != 4 && f.Phase.Load() != 5 {
				state = "FINISHED"
			}
			details := []map[string]any{}
			for i, receiver := range receivers {
				result := "PENDING"
				detail := ""
				amount := receiver["amount"]
				if state == "FINISHED" || f.Phase.Load() == 5 && i == 0 {
					result = "SUCCESS"
					detail = fmt.Sprintf("DETAIL_%d", i+1)
					if f.Phase.Load() == 2 && i == 1 {
						result = "CLOSED"
						detail = ""
					}
					if f.Phase.Load() == 3 {
						amount = receiver["amount"].(float64) + 1
					}
				}
				entry := map[string]any{"type": receiver["type"], "account": receiver["account"], "amount": amount, "result": result, "detail_id": detail}
				if result == "CLOSED" {
					entry["fail_reason"] = "bad account " + receiver["account"].(string)
				}
				details = append(details, entry)
			}
			out = map[string]any{"out_order_no": no, "transaction_id": trade, "order_id": "WX_" + no, "state": state, "receivers": details}
		case strings.HasPrefix(r.URL.Path, "/v3/profitsharing/transactions/") && strings.HasSuffix(r.URL.Path, "/amounts"):
			trade := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v3/profitsharing/transactions/"), "/amounts")
			out = map[string]any{"transaction_id": trade, "unsplit_amount": f.Remaining.Load()}
		default:
			t.Error("unexpected local sharing endpoint", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		raw, _ := json.Marshal(out)
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := "sharing-execution-local"
		hash := sha256.Sum256([]byte(stamp + "\n" + nonce + "\n" + string(raw) + "\n"))
		signature, e := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, hash[:])
		if e != nil {
			t.Error(e)
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Wechatpay-Timestamp", stamp)
		w.Header().Set("Wechatpay-Nonce", nonce)
		w.Header().Set("Wechatpay-Serial", "PUB_KEY_ID_EXECUTION")
		w.Header().Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(signature))
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
	}))
	t.Cleanup(up.Close)
	p, e := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local-app", MerchantID: "original-mch", MerchantSerial: "LOCAL_MERCHANT_SERIAL", PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_EXECUTION": &platform.PublicKey}, EnableChannelSharing: true, Methods: []string{"native"}, HTTPClient: &http.Client{Transport: sharingReceiverTLSTransport{up}}})
	if e != nil {
		t.Fatal(e)
	}
	f.Flow = ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://local.example.invalid"}
	return f
}
func (f *sharingExecutionFixture) paid(t *testing.T, no string, second bool) Order {
	t.Helper()
	ctx := context.Background()
	rule := sharingPaymentRule(t, f.Flow, 2000)
	if second {
		input := channelSharingReceiverInput
		input.Account = "second-receiver-5678"
		r, e := f.Flow.CreateChannelSharingReceiver(ctx, 751, 7, "second-sharing-receiver", input, channelSharingActor)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Flow.BindChannelSharingReceiver(ctx, 751, r.ID, "second-sharing-bind", channelSharingActor); e != nil {
			t.Fatal(e)
		}
		config := rule.ChannelSharingRuleInput
		config.Items = append(config.Items, ChannelSharingRuleItem{ReceiverID: r.ID, Mode: "rate", RateBPS: 3000})
		if _, e = f.Flow.SetChannelSharingRule(ctx, 751, 7, config, rule.ID, channelSharingActor); e != nil {
			t.Fatal(e)
		}
	}
	o, e := f.Store.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: no, Type: "wxpay", Amount: 100, Name: "local sharing item"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Flow.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e != nil {
		t.Fatal(e)
	}
	if e = f.Store.markPaid(ctx, o.TradeNo, "paid-"+no, true, PaidReceipt{RealMoney: 100, ChannelID: 7, ProviderTradeNo: "WX_" + o.TradeNo}); e != nil {
		t.Fatal(e)
	}
	return o
}
func TestChannelSharingExecutionRealTLSPendingThenPartialReleasesOnlyOriginalRemainder(t *testing.T) {
	f := newSharingExecutionFixture(t)
	o := f.paid(t, "sharing-partial", true)
	ctx := context.Background()
	first, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "original-sharing", channelSharingActor)
	if e != nil || first.State != "pending" || first.Completed || first.FundsReleased || f.Sends.Load() != 1 {
		t.Fatal(first, e, f.Sends.Load())
	}
	f.Phase.Store(2)
	final, e := f.Flow.QueryChannelSharing(ctx, 751, "original-sharing", channelSharingActor)
	if e != nil || final.State != "partial" || !final.Completed || !final.FundsReleased || final.ReleasedAmount != 80 || len(final.Items) != 2 {
		t.Fatal(final, e)
	}
	encoded, _ := json.Marshal(final)
	if strings.Contains(string(encoded), "second-receiver-5678") {
		t.Fatal("upstream human failure text disclosed unmasked receiver identity")
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 80 || b.Reserved != 0 {
		t.Fatal(b, e)
	}
	var paid, release int
	if e = f.Store.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM journals WHERE event_key LIKE 'channel-sharing:%:receiver:%:paid'),(SELECT count(*) FROM channel_sharing_completions)").Scan(&paid, &release); e != nil || paid != 1 || release != 1 {
		t.Fatal(paid, release, e)
	}
	for i := 0; i < 3; i++ {
		if _, e = f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "original-sharing", channelSharingActor); e != nil {
			t.Fatal(e)
		}
		if _, e = f.Flow.QueryChannelSharing(ctx, 751, "original-sharing", channelSharingActor); e != nil {
			t.Fatal(e)
		}
	}
	if f.Sends.Load() != 1 {
		t.Fatal("completed PS replay sent again", f.Sends.Load())
	}
	report, e := f.Store.CheckLedger(ctx)
	if e != nil || report.InvalidJournals != 0 || report.InvalidAccounts != 0 || report.InvalidChannelSharingFundings != 0 {
		t.Fatal(report, e)
	}
}

func TestChannelSharingExecutionConcurrentClaimsConsumeOneGrantAndSendOnce(t *testing.T) {
	f := newSharingExecutionFixture(t)
	o := f.paid(t, "sharing-concurrent", false)
	var grants atomic.Int32
	ctx := WithFundsActionGuard(context.Background(), func(ctx context.Context, tx pgx.Tx, kind, key string) error {
		if kind != "channel-sharing-submit" || key != "751:concurrent-sharing" {
			return ErrInvalid
		}
		grants.Add(1)
		return nil
	})
	var wg sync.WaitGroup
	failures := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "concurrent-sharing", channelSharingActor)
			if e != nil {
				failures <- e
			}
		}()
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		t.Fatal(e)
	}
	if grants.Load() != 1 || f.Sends.Load() != 1 {
		t.Fatal("concurrent claims reused authorization or sent twice", grants.Load(), f.Sends.Load())
	}
}
func TestChannelSharingExecutionUnknownOnlyQueriesOriginalAndChangedNumberCannotResend(t *testing.T) {
	f := newSharingExecutionFixture(t)
	o := f.paid(t, "sharing-unknown", false)
	ctx := context.Background()
	f.Phase.Store(4)
	initial, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "unknown-sharing", channelSharingActor)
	var unknown *adapter.UnknownOutcome
	if !errors.As(e, &unknown) || initial.State != "unknown" || f.Sends.Load() != 1 {
		t.Fatal(initial, e, f.Sends.Load())
	}
	for i := 0; i < 3; i++ {
		if _, e = f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "unknown-sharing", channelSharingActor); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "another-unsafe-number", channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal("unknown operation accepted another number", e)
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 0 || b.Reserved != 100 || f.Sends.Load() != 1 {
		t.Fatal(b, e, f.Sends.Load())
	}
	f.Phase.Store(1)
	readOnly := f.Flow
	readOnly.AllowLive = false
	final, e := readOnly.QueryChannelSharing(ctx, 751, "unknown-sharing", channelSharingActor)
	if e != nil || !final.FundsReleased || f.Sends.Load() != 1 || f.Queries.Load() != 1 {
		t.Fatal(final, e, f.Sends.Load(), f.Queries.Load())
	}
}
func TestChannelSharingExecutionRejectsBadEvidenceAndNonzeroUnsplitKeepsOriginalReserve(t *testing.T) {
	f := newSharingExecutionFixture(t)
	o := f.paid(t, "sharing-evidence", false)
	ctx := context.Background()
	if _, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "evidence-sharing", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	f.Phase.Store(3)
	if _, e := f.Flow.QueryChannelSharing(ctx, 751, "evidence-sharing", channelSharingActor); e == nil {
		t.Fatal("signed changed recipient amount accepted")
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 0 || b.Reserved != 100 {
		t.Fatal(b, e)
	}
	f.Phase.Store(1)
	f.Remaining.Store(80)
	held, e := f.Flow.QueryChannelSharing(ctx, 751, "evidence-sharing", channelSharingActor)
	if !errors.Is(e, ErrState) || !held.Completed || held.FundsReleased {
		t.Fatal("nonzero upstream remainder released local funds", held, e)
	}
	b, e = f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 0 || b.Reserved != 80 {
		t.Fatal("confirmed receiver outflow was not recorded independently", b, e)
	}
	f.Remaining.Store(0)
	released, e := f.Flow.QueryChannelSharing(ctx, 751, "evidence-sharing", channelSharingActor)
	if e != nil || released.ReleasedAmount != 80 || !released.FundsReleased || f.Sends.Load() != 1 {
		t.Fatal(released, e, f.Sends.Load())
	}
}
func TestChannelSharingExecutionAuditFailureBeforeAndAfterIOCannotRearm(t *testing.T) {
	f := newSharingExecutionFixture(t)
	o := f.paid(t, "sharing-audit", false)
	ctx := context.Background()
	if _, e := f.Store.pool.Exec(ctx, `CREATE FUNCTION reject_ps_audit_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='channel-sharing-orders' THEN RAISE EXCEPTION 'injected PS audit failure'; END IF; RETURN NEW; END $$;CREATE TRIGGER reject_ps_audit_test BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_ps_audit_test()`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "audit-sharing", channelSharingActor); e == nil || f.Sends.Load() != 0 {
		t.Fatal("failed claim audit reached upstream", e, f.Sends.Load())
	}
	var count int
	if e := f.Store.pool.QueryRow(ctx, "SELECT count(*) FROM channel_sharing_orders").Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
	if _, e := f.Store.pool.Exec(ctx, "DROP TRIGGER reject_ps_audit_test ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Store.pool.Exec(ctx, `CREATE FUNCTION reject_ps_result_audit_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='channel-sharing-orders' AND NEW.operation='submit-result' THEN RAISE EXCEPTION 'injected PS result audit failure'; END IF; RETURN NEW; END $$;CREATE TRIGGER reject_ps_result_audit_test BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_ps_result_audit_test()`); e != nil {
		t.Fatal(e)
	}
	f.Phase.Store(1)
	if _, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "audit-sharing", channelSharingActor); e == nil || f.Sends.Load() != 1 {
		t.Fatal("failed result audit was acknowledged", e, f.Sends.Load())
	}
	current, e := f.Store.ChannelSharingOrder(ctx, 751, "audit-sharing", channelSharingActor)
	if e != nil || current.State != "claimed" || current.Completed || current.FundsReleased {
		t.Fatal(current, e)
	}
	if _, e = f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "audit-sharing", channelSharingActor); e != nil || f.Sends.Load() != 1 {
		t.Fatal("result audit rollback reset the send barrier", e, f.Sends.Load())
	}
	if _, e = f.Store.pool.Exec(ctx, "DROP TRIGGER reject_ps_result_audit_test ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Flow.QueryChannelSharing(ctx, 751, "audit-sharing", channelSharingActor); e != nil || f.Sends.Load() != 1 {
		t.Fatal(e, f.Sends.Load())
	}
}
func TestChannelSharingExecutionDisabledMerchantFailsBeforeExternalSend(t *testing.T) {
	f := newSharingExecutionFixture(t)
	o := f.paid(t, "sharing-disable", false)
	ctx := WithFundsActionGuard(context.Background(), func(ctx context.Context, tx pgx.Tx, kind, key string) error {
		_, e := tx.Exec(ctx, "UPDATE merchants SET enabled=false WHERE id=751")
		return e
	})
	if _, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "disabled-sharing", channelSharingActor); !errors.Is(e, ErrState) || f.Sends.Load() != 0 {
		t.Fatal("merchant disabled at authorization boundary still sent PS", e, f.Sends.Load())
	}
	failed, e := f.Store.ChannelSharingOrder(context.Background(), 751, "disabled-sharing", channelSharingActor)
	if e != nil || failed.State != "failed" {
		t.Fatal(failed, e)
	}
	reader := f.Flow
	reader.AllowLive = false
	result, e := reader.ReconcileChannelSharing(context.Background(), failed.ID)
	if e != nil || result.State != "failed" || result.Completed || f.Queries.Load() != 0 || f.Sends.Load() != 0 {
		t.Fatal("worker queried an authenticated unaccepted rejection", result, e, f.Queries.Load(), f.Sends.Load())
	}
}
func TestChannelSharingExecutionFeeSnapshotDistinguishesGrossUnsplitAndNetRelease(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	if _, e := f.Store.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}');INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"ps-fee","enabled":true,"rate_bps":"1000"}'); UPDATE merchants SET profile=jsonb_build_object('group_id',(SELECT id::text FROM backoffice_records WHERE resource='groups' AND body->>'name'='ps-fee')) WHERE id=751`); e != nil {
		t.Fatal(e)
	}
	o := f.paid(t, "sharing-fee", false)
	if o.MerchantAmount != 90 || o.PlatformFee != 10 {
		t.Fatal(o)
	}
	f.Phase.Store(1)
	final, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "fee-sharing", channelSharingActor)
	if e != nil || !final.FundsReleased || final.ReleasedAmount != 70 {
		t.Fatal(final, e)
	}
	var expected, release, fee int64
	if e = f.Store.pool.QueryRow(ctx, "SELECT (SELECT expected_unsplit FROM channel_sharing_completions),(SELECT release_amount FROM channel_sharing_completions),(SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees')").Scan(&expected, &release, &fee); e != nil || expected != 80 || release != 70 || fee != 10 {
		t.Fatal("upstream gross remainder was confused with local net income", expected, release, fee, e)
	}
}

func TestChannelSharingExecutionWorkerQueriesOriginalWithSystemAuditAndCannotBypassPublicOwner(t *testing.T) {
	f := newSharingExecutionFixture(t)
	o := f.paid(t, "sharing-worker", false)
	ctx := context.Background()
	f.Phase.Store(4)
	initial, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "worker-sharing", channelSharingActor)
	var unknown *adapter.UnknownOutcome
	if !errors.As(e, &unknown) || initial.ID <= 0 {
		t.Fatal(initial, e)
	}
	worker := f.Flow
	worker.AllowLive = false
	f.Phase.Store(1)
	final, e := worker.ReconcileChannelSharing(ctx, initial.ID)
	if e != nil || !final.FundsReleased || f.Sends.Load() != 1 || f.Queries.Load() != 1 {
		t.Fatal(final, e, f.Sends.Load(), f.Queries.Load())
	}
	var observations, audits int
	if e = f.Store.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM channel_sharing_observations WHERE sharing_id=$1 AND source='query' AND actor_role='system' AND actor_id=0 AND actor_name='channel-sharing-worker'),(SELECT count(*) FROM backoffice_audit WHERE resource='channel-sharing-orders' AND resource_id=$1 AND operation IN ('query-result','release-remainder') AND actor_role='system' AND actor_id=0 AND actor_name='channel-sharing-worker')`, initial.ID).Scan(&observations, &audits); e != nil || observations != 1 || audits != 2 {
		t.Fatal("worker was attributed to the original financial actor", observations, audits, e)
	}
	if _, e = worker.QueryChannelSharing(ctx, 751, "worker-sharing", TransferBatchActor{Role: "system", Name: "channel-sharing-worker"}); !errors.Is(e, ErrInvalid) {
		t.Fatal("public query accepted internal actor", e)
	}
	if _, e = worker.ReconcileChannelSharing(ctx, initial.ID); e != nil || f.Queries.Load() != 1 || f.Sends.Load() != 1 {
		t.Fatal("completed worker job queried or sent again", e, f.Queries.Load(), f.Sends.Load())
	}
	if _, e = worker.ReconcileChannelSharing(ctx, 0); !errors.Is(e, ErrInvalid) {
		t.Fatal("invalid worker identity", e)
	}
}

func TestChannelSharingExecutionPreservesEarlierSuccessfulReceiverEvidenceWhileOthersPending(t *testing.T) {
	f := newSharingExecutionFixture(t)
	ctx := context.Background()
	o := f.paid(t, "sharing-observed-progress", true)
	f.Phase.Store(5)
	progress, e := f.Flow.SubmitChannelSharing(ctx, 751, o.TradeNo, "progress-sharing", channelSharingActor)
	if e != nil || progress.State != "pending" || progress.FundsReleased || progress.Items[0].State != "succeeded" || progress.Items[1].State != "pending" {
		t.Fatal(progress, e)
	}
	var firstObservation int64
	if e = f.Store.pool.QueryRow(ctx, "SELECT last_observation_id FROM channel_sharing_order_items WHERE sharing_id=$1 AND receiver_id=$2", progress.ID, progress.Items[0].ReceiverID).Scan(&firstObservation); e != nil {
		t.Fatal(e)
	}
	f.Phase.Store(2)
	final, e := f.Flow.QueryChannelSharing(ctx, 751, "progress-sharing", channelSharingActor)
	if e != nil || !final.FundsReleased || final.State != "partial" || final.ReleasedAmount != 80 {
		t.Fatal(final, e)
	}
	var keptObservation, lastObservation int64
	if e = f.Store.pool.QueryRow(ctx, "SELECT i.last_observation_id,s.last_observation_id FROM channel_sharing_order_items i JOIN channel_sharing_orders s ON s.id=i.sharing_id WHERE s.id=$1 AND i.receiver_id=$2", progress.ID, progress.Items[0].ReceiverID).Scan(&keptObservation, &lastObservation); e != nil || keptObservation != firstObservation || keptObservation == lastObservation {
		t.Fatal("terminal recipient evidence was overwritten", keptObservation, firstObservation, lastObservation, e)
	}
	if _, e = f.Store.ChannelSharingOrder(ctx, 751, "progress-sharing", channelSharingActor); e != nil {
		t.Fatal("valid earlier keyed result was rejected", e)
	}
}
