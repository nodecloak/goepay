package contract_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/server"
)

type historyRuntimeProvider struct {
	batchProjectionProvider
	queries atomic.Int32
}

func (p *historyRuntimeProvider) QueryTransfer(ctx context.Context, in adapter.TransferRequest) (adapter.TransferResult, error) {
	p.queries.Add(1)
	return p.batchProjectionProvider.QueryTransfer(ctx, in)
}

// Preparation reserves synthetic ledger money but never sends it. The optional
// claimed result comes only from the existing in-memory UnknownOutcome mock.
func seedMerchantHistoryBatch(t *testing.T, s *server.Server, pool *pgxpool.Pool, no string, mixed bool) (payment.TransferBatch, *historyRuntimeProvider) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"transfer_api_enabled":true,"transfer_rate":"0"}'::jsonb WHERE id=1; INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); err != nil {
		t.Fatal(err)
	}
	order, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: no + "-income", Amount: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.MarkPaid(ctx, order.TradeNo, no+"-paid"); err != nil {
		t.Fatal(err)
	}
	p := &historyRuntimeProvider{}
	g := &payment.ProviderFlow{Store: s.Store, AllowLive: true, Providers: map[int64]adapter.Provider{7: p}}
	s.ProviderFlow, s.MerchantChannels = g, map[int64][]int64{1: {7}}
	actor := payment.TransferBatchActor{Role: "merchant", ID: 1, Name: "merchant"}
	batch, err := g.PrepareTransferBatch(ctx, 1, no, []payment.TransferBatchInput{
		{ChannelID: 7, Request: adapter.TransferRequest{Type: "alipay", Account: "synthetic-history-first", Amount: 100}},
		{ChannelID: 7, Request: adapter.TransferRequest{Type: "alipay", Account: "synthetic-history-second", Amount: 100}},
	}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if mixed {
		p.unknown = true
		batch, err = g.ExecuteTransferBatch(ctx, 1, batch.ID, 1, actor, nil)
		if err != nil || batch.Summary.Unknown != 1 || batch.Summary.Unsent != 1 {
			t.Fatal("invalid local mixed-history fixture", batch.Summary, err)
		}
	}
	return batch, p
}

func TestMerchantBatchHistorySurvivesMissingRuntimeWithoutResendingUnknown(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	batch, provider := seedMerchantHistoryBatch(t, s, pool, "offline-history", true)
	if _, err := pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"user_transfer_batch","value":"0"}')`); err != nil {
		t.Fatal(err)
	}
	s.ProviderFlow = nil
	token, csrf, err := s.Sessions.Create(identity.User{ID: 1, Username: "merchant", Role: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	counts := func() [4]int64 {
		t.Helper()
		var n [4]int64
		if e := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM transfer_batches),(SELECT count(*) FROM transfers),(SELECT count(*) FROM transfer_batch_events),(SELECT count(*) FROM journals)`).Scan(&n[0], &n[1], &n[2], &n[3]); e != nil {
			t.Fatal(e)
		}
		return n
	}
	beforeCounts := counts()
	before, err := s.Store.Balance(ctx, 1)
	if err != nil || before.Available != 800 || before.Reserved != 200 {
		t.Fatal("history fixture has no real reserved balance", before, err)
	}
	base := "/console/v1/merchant/transfer-batches"
	for _, path := range []string{base, fmt.Sprintf("%s/%d", base, batch.ID)} {
		w := lookupHTTP(t, s.Handler(), "GET", path, "", token, "merchant", csrf)
		if w.Code != 200 {
			t.Errorf("real historical batch inaccessible without provider runtime: %d %s", w.Code, w.Body.String())
		}
	}
	if w := lookupHTTP(t, s.Handler(), "GET", base+"?merchant_id=2", "", token, "merchant", csrf); w.Code != 403 {
		t.Error("batch list lost merchant ownership", w.Code)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash)VALUES('history-other','merchant',2,'Other','unused-local-history-password')`); err != nil {
		t.Fatal(err)
	}
	other, _, err := s.Sessions.Create(identity.User{ID: 2, Username: "history-other", Role: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	if w := lookupHTTP(t, s.Handler(), "GET", fmt.Sprintf("%s/%d", base, batch.ID), "", other, "merchant", ""); w.Code != 404 {
		t.Error("batch detail lost owner boundary", w.Code)
	}
	for _, path := range []string{base, fmt.Sprintf("%s/%d/execute", base, batch.ID)} {
		if w := lookupHTTP(t, s.Handler(), "POST", path, `{"limit":1}`, token, "merchant", csrf); w.Code != 403 {
			t.Error("feature-off admitted a new batch use", path, w.Code)
		}
	}
	if w := lookupHTTP(t, s.Handler(), "POST", fmt.Sprintf("%s/%d/query", base, batch.ID), `{}`, token, "merchant", csrf); w.Code != 503 {
		t.Error("provider query invented a runtime", w.Code)
	}
	if w := lookupHTTP(t, s.Handler(), "POST", fmt.Sprintf("%s/%d/cancel-unsent", base, batch.ID), `{}`, token, "merchant", "wrong-csrf"); w.Code != 403 {
		t.Error("recovery cancellation lost CSRF", w.Code)
	}
	if got := counts(); got != beforeCounts {
		t.Error("read or refused operation modified batch history", beforeCounts, got)
	}
	if b, e := s.Store.Balance(ctx, 1); e != nil || b.Available != before.Available || b.Reserved != before.Reserved {
		t.Error("read or refused operation moved money", b, e)
	}
	w := lookupHTTP(t, s.Handler(), "POST", fmt.Sprintf("%s/%d/cancel-unsent", base, batch.ID), `{}`, token, "merchant", csrf)
	if w.Code != 200 {
		t.Errorf("proven-unsent cancellation required an upstream runtime: %d %s", w.Code, w.Body.String())
	}
	current, err := s.Store.TransferBatch(ctx, 1, batch.ID)
	if err != nil || current.Summary.Unknown != 1 || current.Summary.Cancelled != 1 || current.Items[0].SendState != "claimed" {
		t.Error("recovery altered an unknown original send", current.Summary, err)
	}
	if b, e := s.Store.Balance(ctx, 1); e != nil || b.Available != 900 || b.Reserved != 100 {
		t.Error("recovery released unknown money or failed to release only unsent", b, e)
	}
	if provider.writes.Load() != 1 || provider.queries.Load() != 0 {
		t.Error("history/recovery performed a provider operation", provider.writes.Load(), provider.queries.Load())
	}
}
