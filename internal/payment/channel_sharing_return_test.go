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

// All money is synthetic. The existing fixture creates the original declared
// payment and authenticated sharing; this endpoint only handles its return.
type sharingReturnFixture struct {
	*sharingExecutionFixture
	Sharing        ChannelSharingOrderView
	Sends, Queries atomic.Int32
	Phase          atomic.Int32 // 0 pending, 1 success, 2 failure, 3 lost acceptance, 4 wrong amount
	mu             sync.Mutex
	requests       map[string]map[string]any
	SwitchAccount  func(string)
}

func newSharingReturnFixture(t *testing.T) *sharingReturnFixture {
	t.Helper()
	base := newSharingExecutionFixture(t)
	o := base.paid(t, "original-return-payment", false)
	base.Phase.Store(1)
	sharing, e := base.Flow.SubmitChannelSharing(context.Background(), 751, o.TradeNo, "original-return-sharing", channelSharingActor)
	if e != nil || !sharing.FundsReleased {
		t.Fatal(sharing, e)
	}
	f := &sharingReturnFixture{sharingExecutionFixture: base, Sharing: sharing, requests: map[string]map[string]any{}}
	merchant, _ := rsa.GenerateKey(rand.Reader, 2048)
	platform, _ := rsa.GenerateKey(rand.Reader, 2048)
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		var no string
		if r.Method == "POST" && r.URL.Path == "/v3/profitsharing/return-orders" {
			f.Sends.Add(1)
			if e := json.NewDecoder(r.Body).Decode(&request); e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
			no, _ = request["out_return_no"].(string)
			if len(no) != 32 || !strings.HasPrefix(no, "PR") || request["out_order_no"] != sharing.OutNo {
				t.Error("return changed durable original identity")
			}
			f.mu.Lock()
			f.requests[no] = request
			f.mu.Unlock()
			if f.Phase.Load() == 3 {
				w.WriteHeader(503)
				return
			}
		} else if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v3/profitsharing/return-orders/") {
			f.Queries.Add(1)
			no = strings.TrimPrefix(r.URL.Path, "/v3/profitsharing/return-orders/")
			f.mu.Lock()
			request = f.requests[no]
			f.mu.Unlock()
			if request == nil || r.URL.Query().Get("out_order_no") != sharing.OutNo {
				t.Error("return query lost original number")
				w.WriteHeader(400)
				return
			}
		} else {
			t.Error("unexpected return endpoint", r.URL.Path)
			w.WriteHeader(400)
			return
		}
		state := "PROCESSING"
		if f.Phase.Load() == 1 || f.Phase.Load() == 4 {
			state = "SUCCESS"
		}
		if f.Phase.Load() == 2 {
			state = "FAILED"
		}
		amount := request["amount"].(float64)
		if f.Phase.Load() == 4 {
			amount++
		}
		raw, _ := json.Marshal(map[string]any{"order_id": sharing.ProviderOrderNo, "out_order_no": sharing.OutNo, "out_return_no": no, "return_mchid": request["return_mchid"], "return_id": "WX_" + no, "amount": amount, "result": state})
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := "local-return-only"
		digest := sha256.Sum256([]byte(stamp + "\n" + nonce + "\n" + string(raw) + "\n"))
		signature, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		w.Header().Set("Wechatpay-Timestamp", stamp)
		w.Header().Set("Wechatpay-Nonce", nonce)
		w.Header().Set("Wechatpay-Serial", "PUB_KEY_ID_RETURN")
		w.Header().Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(signature))
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
	}))
	t.Cleanup(up.Close)
	p, e := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local-app", MerchantID: "original-mch", MerchantSerial: "LOCAL_RETURN_SERIAL", PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_RETURN": &platform.PublicKey}, EnableChannelSharing: true, Methods: []string{"native"}, HTTPClient: &http.Client{Transport: sharingReceiverTLSTransport{up}}})
	if e != nil {
		t.Fatal(e)
	}
	f.Flow.Providers[7] = p
	f.SwitchAccount = func(mch string) {
		p, e := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "local-app", MerchantID: mch, MerchantSerial: "LOCAL_RETURN_SERIAL", PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_RETURN": &platform.PublicKey}, EnableChannelSharing: true, Methods: []string{"native"}, HTTPClient: &http.Client{Transport: sharingReceiverTLSTransport{up}}})
		if e != nil {
			t.Fatal(e)
		}
		f.Flow.Providers[7] = p
	}
	return f
}

func TestChannelSharingReturnUnknownCannotChangeNumberAndWrongAmountNeverCredits(t *testing.T) {
	f := newSharingReturnFixture(t)
	ctx := context.Background()
	receiver := f.Sharing.Items[0].ReceiverID
	f.Phase.Store(3)
	first, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "unknown-return", 12, channelSharingActor)
	var unknown *adapter.UnknownOutcome
	if !errors.As(e, &unknown) || first.State != "unknown" || f.Sends.Load() != 1 {
		t.Fatal(first, e)
	}
	for i := 0; i < 3; i++ {
		replay, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "unknown-return", 12, channelSharingActor)
		if e != nil || replay.ID != first.ID {
			t.Fatal(replay, e)
		}
	}
	if _, e = f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "bypass-unknown", 1, channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal("unknown original permits a fresh return number", e)
	}
	if _, e = f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "unknown-return", 11, channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal("replay changed original amount", e)
	}
	if f.Sends.Load() != 1 || f.Queries.Load() != 0 {
		t.Fatal("unknown replay caused provider I/O")
	}
	f.Phase.Store(4)
	if _, e = f.Flow.QueryChannelSharingReturn(ctx, 751, "unknown-return", channelSharingActor); e == nil {
		t.Fatal("signed mismatched amount adopted")
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 80 {
		t.Fatal(b, e)
	}
	f.Phase.Store(1)
	if _, e = f.Flow.QueryChannelSharingReturn(ctx, 751, "unknown-return", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if f.Sends.Load() != 1 || f.Queries.Load() != 2 {
		t.Fatal(f.Sends.Load(), f.Queries.Load())
	}
}

func TestChannelSharingReturnConcurrentReplayConsumesOneGrantAndOriginalCapacity(t *testing.T) {
	f := newSharingReturnFixture(t)
	receiver := f.Sharing.Items[0].ReceiverID
	var grants atomic.Int32
	ctx := WithFundsActionGuard(context.Background(), func(ctx context.Context, tx pgx.Tx, kind, key string) error {
		if kind != "channel-sharing-return" || key != "751:parallel-return" {
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
			_, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "parallel-return", 12, channelSharingActor)
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
		t.Fatal("concurrent replay consumed repeated grant/send", grants.Load(), f.Sends.Load())
	}
	f.Phase.Store(1)
	ctx = context.Background()
	if _, e := f.Flow.QueryChannelSharingReturn(ctx, 751, "parallel-return", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "over-original", 9, channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal("another wallet enlarged original return capacity", e)
	}
	if _, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "remaining-original", 8, channelSharingActor); e != nil {
		t.Fatal(e)
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 100 || f.Sends.Load() != 2 {
		t.Fatal(b, e, f.Sends.Load())
	}
	if _, e = f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "original-exhausted", 1, channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal("exhausted original allowed a return", e)
	}
}

func TestChannelSharingReturnAccountBoundaryAndPublicOwnerStayStrict(t *testing.T) {
	f := newSharingReturnFixture(t)
	ctx := context.Background()
	receiver := f.Sharing.Items[0].ReceiverID
	f.SwitchAccount("another-mch")
	if _, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "wrong-current-account", 12, channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if f.Sends.Load() != 0 {
		t.Fatal("changed merchant account sent original money")
	}
	f.SwitchAccount("original-mch")
	first, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "account-bound-return", 12, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	f.SwitchAccount("another-mch")
	f.Phase.Store(1)
	if _, e = f.Flow.QueryChannelSharingReturn(ctx, 751, "account-bound-return", channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if f.Queries.Load() != 0 {
		t.Fatal("account mismatch queried a different original merchant")
	}
	system := TransferBatchActor{Role: "system", Name: "channel-sharing-return-worker"}
	if _, e = f.Flow.QueryChannelSharingReturn(ctx, 751, "account-bound-return", system); !errors.Is(e, ErrInvalid) {
		t.Fatal("public system actor bypassed owner", e)
	}
	if _, e = f.Store.ListChannelSharingReturns(ctx, 752, 0, 10, channelSharingActor); !errors.Is(e, ErrInvalid) {
		t.Fatal("cross-merchant list bypass", e)
	}
	if _, e = f.Flow.ReconcileChannelSharingReturn(ctx, first.ID); !errors.Is(e, ErrConflict) {
		t.Fatal("worker account mismatch", e)
	}
	f.SwitchAccount("original-mch")
	if _, e = f.Flow.ReconcileChannelSharingReturn(ctx, first.ID); e != nil {
		t.Fatal(e)
	}
	page, e := f.Store.ListChannelSharingReturns(ctx, 751, 0, 10, channelSharingActor)
	if e != nil || len(page.Items) != 1 || page.Items[0].ID != first.ID {
		t.Fatal(page, e)
	}
}

func TestChannelSharingReturnAuditFailureBeforeAndAfterIOCannotResend(t *testing.T) {
	f := newSharingReturnFixture(t)
	ctx := context.Background()
	receiver := f.Sharing.Items[0].ReceiverID
	_, e := f.Store.pool.Exec(ctx, `CREATE FUNCTION reject_pr_claim_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='channel-sharing-returns' THEN RAISE EXCEPTION 'injected PR audit failure'; END IF;RETURN NEW;END $$;CREATE TRIGGER reject_pr_claim_test BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_pr_claim_test()`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "audit-claim-return", 12, channelSharingActor); e == nil {
		t.Fatal("claim audit failure ignored")
	}
	if f.Sends.Load() != 0 {
		t.Fatal("unaudited claim sent")
	}
	if _, e = f.Store.pool.Exec(ctx, "DROP TRIGGER reject_pr_claim_test ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	_, e = f.Store.pool.Exec(ctx, `CREATE FUNCTION reject_pr_result_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='channel-sharing-returns' AND NEW.operation='submit-result' THEN RAISE EXCEPTION 'injected PR result audit failure'; END IF;RETURN NEW;END $$;CREATE TRIGGER reject_pr_result_test BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_pr_result_test()`)
	if e != nil {
		t.Fatal(e)
	}
	f.Phase.Store(1)
	first, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "audit-result-return", 12, channelSharingActor)
	if e == nil || f.Sends.Load() != 1 {
		t.Fatal(first, e)
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 80 {
		t.Fatal("failed result audit left credit behind", b, e)
	}
	if _, e = f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "audit-result-return", 12, channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "audit-result-bypass", 1, channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if f.Sends.Load() != 1 {
		t.Fatal("audit failure resent original return")
	}
	if _, e = f.Store.pool.Exec(ctx, "DROP TRIGGER reject_pr_result_test ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Flow.QueryChannelSharingReturn(ctx, 751, "audit-result-return", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	b, e = f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 92 {
		t.Fatal(b, e)
	}
}

func TestChannelSharingReturnFailedIsNotCashAndSuccessRestoresOnlyOriginalSource(t *testing.T) {
	f := newSharingReturnFixture(t)
	ctx := context.Background()
	receiver := f.Sharing.Items[0].ReceiverID
	var trade string
	if e := f.Store.pool.QueryRow(ctx, "SELECT trade_no FROM orders WHERE id=$1", f.Sharing.OrderID).Scan(&trade); e != nil {
		t.Fatal(e)
	}
	f.Phase.Store(2)
	failed, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "failed-original-return", 12, channelSharingActor)
	if e != nil || failed.State != "failed" {
		t.Fatal(failed, e)
	}
	amount, e := f.Store.OrderIncomeFreezable(ctx, 751, trade)
	if e != nil || amount != 80 {
		t.Fatal("failed return restored original source", amount, e)
	}
	f.Phase.Store(0)
	pending, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "credible-original-return", 12, channelSharingActor)
	if e != nil || pending.State != "pending" {
		t.Fatal(pending, e)
	}
	amount, e = f.Store.OrderIncomeFreezable(ctx, 751, trade)
	if e != nil || amount != 80 {
		t.Fatal("pending return restored original source", amount, e)
	}
	f.Phase.Store(1)
	if _, e = f.Flow.QueryChannelSharingReturn(ctx, 751, "credible-original-return", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	amount, e = f.Store.OrderIncomeFreezable(ctx, 751, trade)
	if e != nil || amount != 92 {
		t.Fatal("credible PR did not restore exact original retained income", amount, e)
	}
	if _, e = f.Store.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settle_type","value":"1"}')`); e != nil {
		t.Fatal(e)
	}
	funds, e := f.Store.FundsAvailability(ctx, 751)
	if e != nil || funds.TodayGross != 92 || funds.Available != 0 || funds.Blocked != 92 {
		t.Fatal("PR bypassed original T1 maturity", funds, e)
	}
}
func TestChannelSharingReturnPendingThenReadOnlyWorkerCreditsOriginalMerchantOnce(t *testing.T) {
	f := newSharingReturnFixture(t)
	ctx := context.Background()
	receiver := f.Sharing.Items[0].ReceiverID
	first, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "original-return", 12, channelSharingActor)
	if e != nil || first.State != "pending" || f.Sends.Load() != 1 {
		t.Fatal(first, e, f.Sends.Load())
	}
	b, e := f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 80 {
		t.Fatal(b, e)
	}
	f.Phase.Store(1)
	reader := f.Flow
	reader.AllowLive = false
	final, e := reader.ReconcileChannelSharingReturn(ctx, first.ID)
	if e != nil || final.State != "succeeded" || final.Amount != 12 || final.ReceiverID != receiver {
		t.Fatal(final, e)
	}
	for i := 0; i < 3; i++ {
		if _, e = reader.QueryChannelSharingReturn(ctx, 751, "original-return", channelSharingActor); e != nil {
			t.Fatal(e)
		}
	}
	b, e = f.Store.Balance(ctx, 751)
	if e != nil || b.Available != 92 || f.Sends.Load() != 1 || f.Queries.Load() != 1 {
		t.Fatal(b, e, f.Sends.Load(), f.Queries.Load())
	}
	var journals, system int
	e = f.Store.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM journals WHERE event_key=$1),(SELECT count(*) FROM backoffice_audit WHERE resource='channel-sharing-returns' AND operation='query-result' AND actor_role='system' AND actor_id=0)", fmt.Sprintf("channel-sharing-return:%d:paid", first.ID)).Scan(&journals, &system)
	if e != nil || journals != 1 || system != 1 {
		t.Fatal(journals, system, e)
	}
	report, e := f.Store.CheckLedger(ctx)
	if e != nil || report.InvalidChannelSharingReturns != 0 || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
		t.Fatal(report, e)
	}
}

func TestChannelSharingReturnForgedObservationCannotBeAdoptedOrRestoreOriginalIncome(t *testing.T) {
	f := newSharingReturnFixture(t)
	ctx := context.Background()
	receiver := f.Sharing.Items[0].ReceiverID
	first, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "untrusted-retained-return", 12, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := f.Store.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var clearing, available, journal, observation int64
	if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=0 AND kind='clearing' AND scope_id=0").Scan(&clearing); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=751 AND kind='available' AND scope_id=0").Scan(&available); e != nil {
		t.Fatal(e)
	}
	key := fmt.Sprintf("channel-sharing-return:%d:paid", first.ID)
	if e = postByAccountIDs(ctx, tx, key, "forensic synthetic forged return", clearing, available, 12); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(ctx, "SELECT id FROM journals WHERE event_key=$1", key).Scan(&journal); e != nil {
		t.Fatal(e)
	}
	// SQL can verify shape, not possession of the keyed provider evidence. The
	// public query must refuse this complete-looking but unauthenticated fact.
	if e = tx.QueryRow(ctx, "INSERT INTO channel_sharing_return_observations(return_id,fingerprint,source,provider_return_no,state,amount,actor_role,actor_id,actor_name) VALUES($1,$2,'query',$3,'succeeded',12,'merchant',751,'sharing-owner') RETURNING id", first.ID, make([]byte, 32), first.ProviderReturnNo).Scan(&observation); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_returns SET state='succeeded',last_observation_id=$2,paid_journal_id=$3 WHERE id=$1", first.ID, observation, journal); e != nil {
		t.Fatal(e)
	}
	if e = sharingConfigurationAudit(ctx, tx, channelSharingActor, "channel-sharing-returns", first.ID, "query-result", "succeeded"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Flow.QueryChannelSharingReturn(ctx, 751, "untrusted-retained-return", channelSharingActor); !errors.Is(e, ErrConflict) {
		t.Fatal("forged retained proof was relabeled as provider success", e)
	}
	if _, e = f.Flow.ReconcileChannelSharingReturn(ctx, first.ID); !errors.Is(e, ErrConflict) {
		t.Fatal("worker adopted forged retained proof", e)
	}
	if f.Queries.Load() != 0 || f.Sends.Load() != 1 {
		t.Fatal("proof failure still reached upstream")
	}
	report, e := f.Store.CheckLedger(ctx)
	if e != nil || report.InvalidChannelSharingReturns != 1 {
		t.Fatal("keyed proof failed to invalidate structural ledger report", report, e)
	}
	var trade string
	if e = f.Store.pool.QueryRow(ctx, "SELECT trade_no FROM orders WHERE id=$1", f.Sharing.OrderID).Scan(&trade); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Store.OrderIncomeFreezable(ctx, 751, trade); !errors.Is(e, ErrConflict) {
		t.Fatal("fake PR restored source budget", e)
	}
	if _, e = f.Store.ReserveRefund(ctx, 751, "original-return-payment", "forged-source-refund", 1); !errors.Is(e, ErrConflict) {
		t.Fatal("fake retained evidence enabled ordinary refund", e)
	}
}

func TestChannelSharingReturnDatabaseGuardsOriginalIdentityUnknownBarrierAndProjection(t *testing.T) {
	f := newSharingReturnFixture(t)
	ctx := context.Background()
	receiver := f.Sharing.Items[0].ReceiverID
	first, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "database-original-return", 12, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Store.pool.Exec(ctx, "UPDATE channel_sharing_returns SET amount=11 WHERE id=$1", first.ID); e == nil {
		t.Fatal("original PR amount changed")
	}
	tx, e := f.Store.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var second int64
	e = tx.QueryRow(ctx, `INSERT INTO channel_sharing_returns(sharing_id,order_id,merchant_id,receiver_id,external_no,out_no,amount,request_hash,actor_role,actor_id,actor_name,actor_class) SELECT sharing_id,order_id,merchant_id,receiver_id,'raw-new-number','PR012345678901234567890123456789',1,request_hash,actor_role,actor_id,actor_name,actor_class FROM channel_sharing_returns WHERE id=$1 RETURNING id`, first.ID).Scan(&second)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_returns SET state='claimed',claimed_at=clock_timestamp() WHERE id=$1", second); e != nil {
		t.Fatal(e)
	}
	if e = sharingConfigurationAudit(ctx, tx, channelSharingActor, "channel-sharing-returns", second, "submit", "claimed"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e == nil {
		t.Fatal("database allowed a new return through unresolved original barrier")
	}
	f.Phase.Store(1)
	if _, e = f.Flow.QueryChannelSharingReturn(ctx, 751, "database-original-return", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Store.pool.Exec(ctx, "UPDATE channel_sharing_return_observations SET amount=1"); e == nil {
		t.Fatal("retained provider observation mutated")
	}
	if _, e = f.Store.pool.Exec(ctx, `CREATE OR REPLACE VIEW channel_sharing_return_invariants AS SELECT id AS return_id,true AS valid FROM channel_sharing_returns WHERE false`); e != nil {
		t.Fatal(e)
	}
	report, e := f.Store.CheckLedger(ctx)
	if e != nil || report.InvalidChannelSharingReturns != 1 {
		t.Fatal("missing invariant projection hid original PR funding", report, e)
	}
}

func TestChannelSharingReturnForgedFailureCannotReopenUnknownRecipientCapacity(t *testing.T) {
	f := newSharingReturnFixture(t)
	ctx := context.Background()
	receiver := f.Sharing.Items[0].ReceiverID
	f.Phase.Store(3)
	first, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "unknown-before-fake-failure", 12, channelSharingActor)
	var unknown *adapter.UnknownOutcome
	if !errors.As(e, &unknown) || first.State != "unknown" {
		t.Fatal(first, e)
	}
	tx, e := f.Store.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "UPDATE channel_sharing_returns SET state='failed',failure_proof=$2 WHERE id=$1", first.ID, make([]byte, 32)); e != nil {
		t.Fatal(e)
	}
	if e = sharingConfigurationAudit(ctx, tx, channelSharingActor, "channel-sharing-returns", first.ID, "submit-result", "failed"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	_, e = f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "new-after-fake-failure", 12, channelSharingActor)
	if !errors.Is(e, ErrConflict) || f.Sends.Load() != 1 {
		t.Fatal("forged unobserved failure reopened unknown original capacity", e, f.Sends.Load())
	}
}

func TestChannelSharingReturnSealedPreIOFailureMayReleaseUnusedRecipientCapacity(t *testing.T) {
	f := newSharingReturnFixture(t)
	receiver := f.Sharing.Items[0].ReceiverID
	ctx := WithFundsActionGuard(context.Background(), func(ctx context.Context, tx pgx.Tx, kind, key string) error {
		_, e := tx.Exec(ctx, "UPDATE merchants SET enabled=false WHERE id=751")
		return e
	})
	_, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "pre-io-unused-return", 12, channelSharingActor)
	if !errors.Is(e, ErrState) || f.Sends.Load() != 0 {
		t.Fatal("disabled original sent return", e, f.Sends.Load())
	}
	ctx = context.Background()
	failed, e := f.Store.ChannelSharingReturn(ctx, 751, "pre-io-unused-return", channelSharingActor)
	if e != nil || failed.State != "failed" {
		t.Fatal("valid sealed failure could not be read", failed, e)
	}
	if _, e = f.Store.pool.Exec(ctx, "UPDATE merchants SET enabled=true WHERE id=751"); e != nil {
		t.Fatal(e)
	}
	f.Phase.Store(1)
	recovered, e := f.Flow.SubmitChannelSharingReturn(ctx, 751, f.Sharing.ExternalNo, receiver, "new-after-unused-return", 12, channelSharingActor)
	if e != nil || recovered.State != "succeeded" || f.Sends.Load() != 1 {
		t.Fatal(recovered, e, f.Sends.Load())
	}
	report, e := f.Store.CheckLedger(ctx)
	if e != nil || report.InvalidChannelSharingReturns != 0 {
		t.Fatal(report, e)
	}
}
