package reconciliation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
)

type queryProvider struct {
	creates, refunds, queries, refundQueries int
	transfers, transferQueries               int
	state                                    string
	unknown                                  bool
}

func (p *queryProvider) TransferTypes() []string { return []string{"wxpay"} }
func (p *queryProvider) Transfer(context.Context, adapter.TransferRequest) (adapter.TransferResult, error) {
	p.transfers++
	return adapter.TransferResult{}, &adapter.UnknownOutcome{Operation: "transfer", Cause: errors.New("synthetic timeout")}
}
func (p *queryProvider) QueryTransfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.transferQueries++
	return adapter.TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: "synthetic-tf", State: p.state, Amount: r.Amount}, nil
}

func (p *queryProvider) ID() string { return "wxpay" }
func (p *queryProvider) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{Methods: []string{"native"}, Query: true, Refund: true, RefundQuery: true}
}
func (p *queryProvider) Create(context.Context, adapter.CreateRequest) (adapter.CreateResult, error) {
	p.creates++
	if p.unknown {
		return adapter.CreateResult{}, &adapter.UnknownOutcome{Operation: "create", Cause: errors.New("test timeout")}
	}
	return adapter.CreateResult{MaterialType: "qrcode", Material: "weixin://synthetic"}, nil
}
func (p *queryProvider) Query(_ context.Context, r adapter.QueryRequest) (adapter.QueryResult, error) {
	p.queries++
	return adapter.QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: "synthetic-paid", State: p.state, Amount: 100, Currency: "CNY"}, nil
}
func (p *queryProvider) Close(context.Context, adapter.QueryRequest) error {
	return adapter.ErrUnsupported
}
func (p *queryProvider) Refund(context.Context, adapter.RefundRequest) (adapter.RefundResult, error) {
	p.refunds++
	return adapter.RefundResult{}, &adapter.UnknownOutcome{Operation: "refund", Cause: errors.New("test timeout")}
}
func (p *queryProvider) QueryRefund(_ context.Context, r adapter.RefundRequest) (adapter.RefundResult, error) {
	p.refundQueries++
	return adapter.RefundResult{RefundNo: r.RefundNo, State: p.state, Amount: r.Amount}, nil
}

func fixture(t *testing.T) (*Handler, *queryProvider, payment.Order) {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	p, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	nonce := make([]byte, 8)
	rand.Read(nonce)
	schema := "reconcile_" + hex.EncodeToString(nonce)
	if _, e = p.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { pool.Close(); p.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); p.Close() })
	paths, e := filepath.Glob("../../db/migrations/*.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range paths {
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(raw)); e != nil {
			t.Fatal(e)
		}
	}
	store, e := payment.NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	if e = store.CreateMerchant(ctx, payment.Merchant{ID: 1, Name: "synthetic", APIKey: "synthetic-secret"}); e != nil {
		t.Fatal(e)
	}
	order, e := store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "synthetic", Type: "wxpay", Name: "test", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	provider := &queryProvider{state: "SUCCESS", unknown: true}
	flow := &payment.ProviderFlow{Store: store, Providers: map[int64]adapter.Provider{7: provider}, AllowLive: true, CallbackBaseURL: "https://synthetic.invalid"}
	if _, e = flow.Submit(ctx, order.TradeNo, 7, "native", "192.0.2.1", ""); e == nil {
		t.Fatal("expected unknown create")
	}
	flow.AllowLive = false
	return &Handler{Pool: pool, Flow: flow}, provider, order
}
func job(kind string, payload any) jobs.Job {
	raw, _ := json.Marshal(payload)
	return jobs.Job{Kind: kind, Payload: raw}
}
func TestPaymentReconciliationOnlyQueriesAndIsIdempotent(t *testing.T) {
	h, p, o := fixture(t)
	ctx := context.Background()
	j := job("payment.reconcile", map[string]any{"trade_no": o.TradeNo})
	var count int
	if e := h.Pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE kind='payment.reconcile' AND payload->>'trade_no'=$1", o.TradeNo).Scan(&count); e != nil || count != 1 {
		t.Fatalf("unknown create not durably queued: %d %v", count, e)
	}
	for i := 0; i < 2; i++ {
		if e := h.Handle(ctx, j); e != nil {
			t.Fatal(e)
		}
	}
	b, e := h.Flow.Store.Balance(ctx, 1)
	if e != nil || b.Available != 100 || b.Reserved != 0 {
		t.Fatal(b, e)
	}
	if p.creates != 1 || p.refunds != 0 || p.queries != 1 {
		t.Fatalf("writes repeated or terminal queried: %+v", p)
	}
	if e := h.Handle(ctx, job("transfer.reconcile", map[string]int{"transfer_id": 1})); e == nil {
		t.Fatal("unsupported task silently succeeded")
	}
}
func TestRefundReconciliationPreservesUnknownAndRequiresOperation(t *testing.T) {
	h, p, o := fixture(t)
	ctx := context.Background()
	if e := h.Handle(ctx, job("payment.reconcile", map[string]string{"trade_no": o.TradeNo})); e != nil {
		t.Fatal(e)
	}
	h.Flow.AllowLive = true
	r, e := h.Flow.Refund(ctx, 1, o.TradeNo, "merchant-refund", 60)
	if e == nil {
		t.Fatal("expected timeout")
	}
	h.Flow.AllowLive = false
	p.state = "PROCESSING"
	j := job("refund.reconcile", map[string]int64{"refund_id": r.ID})
	if e = h.Handle(ctx, j); !errors.Is(e, ErrPending) {
		t.Fatalf("pending task acknowledged: %v", e)
	}
	b, _ := h.Flow.Store.Balance(ctx, 1)
	if b.Available != 40 || b.Reserved != 60 {
		t.Fatal("pending released reservation", b)
	}
	p.state = "SUCCESS"
	for i := 0; i < 2; i++ {
		if e = h.Handle(ctx, j); e != nil {
			t.Fatal(e)
		}
	}
	b, _ = h.Flow.Store.Balance(ctx, 1)
	if b.Available != 40 || b.Reserved != 0 || p.refunds != 1 || p.refundQueries != 2 {
		t.Fatal(b, p)
	}
	orphan, e := h.Flow.Store.ReserveRefund(ctx, 1, o.OutTradeNo, "orphan", 10)
	if e != nil {
		t.Fatal(e)
	}
	before := p.refundQueries
	if e = h.Handle(ctx, job("refund.reconcile", map[string]int64{"refund_id": orphan.ID})); e == nil {
		t.Fatal("missing operation acknowledged")
	}
	if p.refundQueries != before {
		t.Fatal("queried without immutable provider operation")
	}
	b, _ = h.Flow.Store.Balance(ctx, 1)
	if b.Reserved != 10 {
		t.Fatal(b)
	}
}
func TestReconciliationRejectsMalformedJobs(t *testing.T) {
	h, _, _ := fixture(t)
	for _, raw := range []string{`{}`, `{"trade_no":"x","extra":1}`, `{"trade_no":"x"} garbage`, `{"trade_no":"x","trade_no":"y"}`} {
		if e := h.Handle(context.Background(), jobs.Job{Kind: "payment.reconcile", Payload: json.RawMessage(raw)}); e == nil {
			t.Fatal(fmt.Sprint("accepted malformed payload ", raw))
		}
	}
	for _, raw := range []string{`{}`, `{"sharing_id":0}`, `{"sharing_id":-1}`, `{"sharing_id":"1"}`, `{"sharing_id":1,"channel_id":7}`, `{"sharing_id":1,"sharing_id":2}`, `{"sharing_id":1} trailing`} {
		if e := h.Handle(context.Background(), jobs.Job{Kind: "channel-sharing.reconcile", Payload: json.RawMessage(raw)}); !errors.Is(e, ErrJob) {
			t.Fatal("accepted malformed sharing task", raw, e)
		}
	}
	if e := h.Handle(context.Background(), job("channel-sharing.reconcile", map[string]int64{"sharing_id": 999999999})); !errors.Is(e, payment.ErrNotFound) {
		t.Fatal("missing sharing operation was acknowledged", e)
	}
	for _, raw := range []string{`{}`, `{"return_id":0}`, `{"return_id":1,"channel_id":7}`, `{"return_id":1,"return_id":2}`} {
		if e := h.Handle(context.Background(), jobs.Job{Kind: "channel-sharing-return.reconcile", Payload: json.RawMessage(raw)}); !errors.Is(e, ErrJob) {
			t.Fatal("accepted malformed return task", raw, e)
		}
	}
	if e := h.Handle(context.Background(), job("channel-sharing-return.reconcile", map[string]int64{"return_id": 999999999})); !errors.Is(e, payment.ErrNotFound) {
		t.Fatal("missing sharing return was acknowledged", e)
	}
	for _, raw := range []string{`{}`, `{"finish_id":0}`, `{"finish_id":1,"channel_id":7}`, `{"finish_id":1,"finish_id":2}`} {
		if e := h.Handle(context.Background(), jobs.Job{Kind: "channel-sharing-finish.reconcile", Payload: json.RawMessage(raw)}); !errors.Is(e, ErrJob) {
			t.Fatal("accepted malformed finish task", raw, e)
		}
	}
	if e := h.Handle(context.Background(), job("channel-sharing-finish.reconcile", map[string]int64{"finish_id": 999999999})); !errors.Is(e, payment.ErrNotFound) {
		t.Fatal("missing sharing finish was acknowledged", e)
	}
}

func TestReconciliationClaimFiltersConfiguredChannels(t *testing.T) {
	h, _, o := fixture(t)
	ctx := context.Background()
	if _, e := h.Pool.Exec(ctx, "UPDATE jobs SET run_at=now(); INSERT INTO jobs(event_key,kind,payload) VALUES('unconfigured','payment.reconcile','{\"trade_no\":\"missing\"}')"); e != nil {
		t.Fatal(e)
	}
	q := jobs.Queue{Pool: h.Pool}
	j, e := q.ClaimProviderChannels(ctx, []string{"payment.notify", "payment.reconcile", "refund.reconcile"}, time.Minute, []int64{8})
	if !jobs.IsEmpty(e) {
		t.Fatalf("claimed unconfigured channel: %+v %v", j, e)
	}
	j, e = q.ClaimProviderChannels(ctx, []string{"payment.notify", "payment.reconcile", "refund.reconcile"}, time.Minute, []int64{7})
	if e != nil || string(j.Payload) != fmt.Sprintf("{\"trade_no\": \"%s\"}", o.TradeNo) {
		t.Fatal(j, e)
	}
}

func TestPaymentReconciliationVerifiesLatePaidClosedOrder(t *testing.T) {
	h, p, o := fixture(t)
	ctx := context.Background()
	if _, e := h.Pool.Exec(ctx, "UPDATE orders SET status='closed' WHERE trade_no=$1", o.TradeNo); e != nil {
		t.Fatal(e)
	}
	if e := h.Handle(ctx, job("payment.reconcile", map[string]string{"trade_no": o.TradeNo})); e != nil {
		t.Fatal(e)
	}
	b, e := h.Flow.Store.Balance(ctx, 1)
	if e != nil || b.Available != 100 || p.queries != 1 {
		t.Fatalf("closed late payment was not verified: %+v %v queries%d", b, e, p.queries)
	}
}

func TestTransferReconciliationOnlyQueriesAndPreservesReservation(t *testing.T) {
	h, p, o := fixture(t)
	ctx := context.Background()
	if e := h.Handle(ctx, job("payment.reconcile", map[string]string{"trade_no": o.TradeNo})); e != nil {
		t.Fatal(e)
	}
	if _, e := h.Pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true}' WHERE id=1; INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	h.Flow.AllowLive = true
	tr, e := h.Flow.Transfer(ctx, 1, 7, "business-tf", adapter.TransferRequest{Type: "wxpay", Account: "synthetic-private-openid", Amount: 60, Remark: "synthetic"})
	if e == nil {
		t.Fatal("timeout accepted")
	}
	h.Flow.AllowLive = false
	p.state = "PROCESSING"
	j := job("transfer.reconcile", map[string]int64{"transfer_id": tr.ID})
	if e = h.Handle(ctx, j); !errors.Is(e, ErrPending) {
		t.Fatalf("pending transfer acknowledged %v", e)
	}
	b, _ := h.Flow.Store.Balance(ctx, 1)
	if b.Available != 40 || b.Reserved != 60 {
		t.Fatal(b)
	}
	q := jobs.Queue{Pool: h.Pool}
	if _, e := h.Pool.Exec(ctx, "UPDATE jobs SET run_at=now() WHERE kind='transfer.reconcile'"); e != nil {
		t.Fatal(e)
	}
	claimed, e := q.ClaimProviderChannels(ctx, []string{"transfer.reconcile"}, time.Minute, []int64{8})
	if !jobs.IsEmpty(e) {
		t.Fatal("wrong channel transfer claimed", claimed, e)
	}
	claimed, e = q.ClaimProviderChannels(ctx, []string{"transfer.reconcile"}, time.Minute, []int64{7})
	if e != nil || claimed.Kind != "transfer.reconcile" {
		t.Fatal(claimed, e)
	}
	p.state = "SUCCESS"
	for i := 0; i < 2; i++ {
		if e = h.Handle(ctx, j); e != nil {
			t.Fatal(e)
		}
	}
	b, _ = h.Flow.Store.Balance(ctx, 1)
	if b.Available != 40 || b.Reserved != 0 || p.transfers != 1 || p.transferQueries != 2 {
		t.Fatal(b, p)
	}
	orphan, e := h.Flow.Store.ReserveTransfer(ctx, 1, "legacy-without-provider", 10)
	if e != nil {
		t.Fatal(e)
	}
	before := p.transferQueries
	if e = h.Handle(ctx, job("transfer.reconcile", map[string]int64{"transfer_id": orphan.ID})); e == nil {
		t.Fatal("missing operation became successful")
	}
	if p.transferQueries != before {
		t.Fatal("query without durable channel")
	}
}

type recurringBankProvider struct {
	queryProvider
	bankState string
	failQuery bool
}

func (p *recurringBankProvider) TransferTypes() []string { return []string{"bank"} }
func (p *recurringBankProvider) Transfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.transfers++
	return adapter.TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: "synthetic-bank", State: "PROCESSING", Amount: r.Amount, BankFeeAmount: 2, BankFeeKnown: true}, nil
}
func (p *recurringBankProvider) QueryTransfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.transferQueries++
	if p.failQuery {
		return adapter.TransferResult{}, errors.New("synthetic query timeout")
	}
	return adapter.TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: "synthetic-bank", State: p.bankState, Amount: r.Amount, BankFeeAmount: 2, BankFeeKnown: true}, nil
}

func TestBankReconciliationSchedulesBeforeQueryAndFollowsLateReturn(t *testing.T) {
	h, _, o := fixture(t)
	ctx := context.Background()
	if e := h.Handle(ctx, job("payment.reconcile", map[string]string{"trade_no": o.TradeNo})); e != nil {
		t.Fatal(e)
	}
	if _, e := h.Pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true}' WHERE id=1; INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	p := &recurringBankProvider{bankState: "SUCCESS"}
	h.Flow.Providers[7] = p
	h.Flow.AllowLive = true
	tr, e := h.Flow.Transfer(ctx, 1, 7, "bank-recheck", adapter.TransferRequest{Type: "bank", Account: "6222020000000000000", Name: "synthetic", BankCode: "1002", Amount: 60, Remark: "synthetic"})
	if e != nil {
		t.Fatal(e)
	}
	h.Flow.AllowLive = false
	normal := job("transfer.reconcile", map[string]int64{"transfer_id": tr.ID})
	if _, e = h.Pool.Exec(ctx, `CREATE FUNCTION reject_bank_schedule_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='transfer.bank_reconcile' THEN RAISE EXCEPTION 'synthetic schedule failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_bank_schedule_test BEFORE INSERT ON jobs FOR EACH ROW EXECUTE FUNCTION reject_bank_schedule_test()`); e != nil {
		t.Fatal(e)
	}
	if e = h.Handle(ctx, normal); e == nil || p.transferQueries != 0 {
		t.Fatal("query ran without durable next generation", e, p.transferQueries)
	}
	if _, e = h.Pool.Exec(ctx, "DROP TRIGGER reject_bank_schedule_test ON jobs"); e != nil {
		t.Fatal(e)
	}
	if e = h.Handle(ctx, normal); e != nil {
		t.Fatal(e)
	}
	b, _ := h.Flow.Store.Balance(ctx, 1)
	if b.Available != 40 || b.Reserved != 0 {
		t.Fatal("verified bank success not settled", b)
	}
	var scheduled int
	if e = h.Pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE kind='transfer.bank_reconcile' AND state='pending'").Scan(&scheduled); e != nil || scheduled != 1 {
		t.Fatal("no durable recurring query", scheduled, e)
	}
	q := jobs.Queue{Pool: h.Pool}
	if _, e = h.Pool.Exec(ctx, "UPDATE jobs SET run_at=now() WHERE kind='transfer.bank_reconcile'"); e != nil {
		t.Fatal(e)
	}
	if _, e = h.Pool.Exec(ctx, "UPDATE bank_transfer_lifecycle SET next_check_at=now()-interval '1 second' WHERE transfer_id=$1", tr.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = q.ClaimProviderChannels(ctx, []string{"transfer.bank_reconcile"}, time.Minute, []int64{8}); !jobs.IsEmpty(e) {
		t.Fatal("bank recheck escaped channel filter", e)
	}
	claimed, e := q.ClaimProviderChannels(ctx, []string{"transfer.bank_reconcile"}, time.Minute, []int64{7})
	if e != nil {
		t.Fatal("bank query was not routed", e)
	}
	p.failQuery = true
	if e = h.Handle(ctx, claimed); e != nil {
		t.Fatal("durably rescheduled read error was not acknowledged", e)
	}
	if e = q.Complete(ctx, claimed); e != nil {
		t.Fatal(e)
	}
	if e = h.Pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE kind='transfer.bank_reconcile' AND state='pending'").Scan(&scheduled); e != nil || scheduled != 1 {
		t.Fatal("query failure lost next check", scheduled, e)
	}
	p.failQuery = false
	p.bankState = "BANK_FAIL"
	check := job("transfer.bank_reconcile", map[string]int64{"transfer_id": tr.ID})
	for i := 0; i < 2; i++ {
		if e = h.Handle(ctx, check); e != nil {
			t.Fatal(e)
		}
	}
	b, _ = h.Flow.Store.Balance(ctx, 1)
	if b.Available != 100 || b.Reserved != 0 || p.transfers != 1 {
		t.Fatal("late bank return not exactly once", b, p.transfers)
	}
	before := p.transferQueries
	if e = h.Handle(ctx, check); e != nil || p.transferQueries != before {
		t.Fatal("final bank return queried again", e)
	}
	if _, e = h.Pool.Exec(ctx, "DELETE FROM jobs WHERE kind='transfer.bank_reconcile'"); e != nil {
		t.Fatal(e)
	}
	if e = h.Handle(ctx, check); e != nil {
		t.Fatal(e)
	}
	if e = h.Pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE kind='transfer.bank_reconcile'").Scan(&scheduled); e != nil || scheduled != 0 {
		t.Fatal("final bank return rescheduled", scheduled, e)
	}
}
