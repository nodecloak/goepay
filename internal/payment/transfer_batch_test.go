package payment

import (
	"context"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"strings"
	"sync"
	"testing"
)

type batchTestProvider struct{ outgoingProvider }

func (p *batchTestProvider) ApplicationID() string { return "batch-test-app" }
func batchFixture(t *testing.T) (*Store, ProviderFlow, *batchTestProvider) {
	t.Helper()
	s, g, _ := transferFixture(t)
	p := &batchTestProvider{}
	g.Providers[7] = p
	return s, g, p
}

var batchMerchantActor = TransferBatchActor{Role: "merchant", ID: 501, Name: "batch merchant"}

type changedBatchProvider struct{ batchTestProvider }

func (*changedBatchProvider) ApplicationID() string { return "different-application" }

func TestTransferBatchClaimRechecksWhitelistApplicationAndStop(t *testing.T) {
	s, g, p := batchFixture(t)
	ctx := context.Background()
	b, e := g.PrepareTransferBatch(ctx, 501, "recheck", batchInputs(100), batchMerchantActor)
	if e != nil {
		t.Fatal(e)
	}
	denied := func(context.Context, int64) error { return ErrState }
	if _, e = g.ExecuteTransferBatch(ctx, 501, b.ID, 1, batchMerchantActor, denied); !errors.Is(e, ErrState) {
		t.Fatal("revoked whitelist not checked", e)
	}
	g.Providers[7] = &changedBatchProvider{}
	if _, e = g.ExecuteTransferBatch(ctx, 501, b.ID, 1, batchMerchantActor, nil); !errors.Is(e, ErrConflict) {
		t.Fatal("changed application not checked", e)
	}
	g.Providers[7] = p
	g.AllowLive = false
	if _, e = g.ExecuteTransferBatch(ctx, 501, b.ID, 1, batchMerchantActor, nil); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal("write stop not checked", e)
	}
	current, e := s.TransferBatch(ctx, 501, b.ID)
	if e != nil || current.Summary.Unsent != 1 || p.transfers.Load() != 0 {
		t.Fatal("failed checks consumed send or called upstream", e)
	}
	g.AllowLive = true
	p.unknown = true
	if _, e = g.ExecuteTransferBatch(ctx, 501, b.ID, 1, batchMerchantActor, nil); e != nil {
		t.Fatal(e)
	}
	g.Providers[7] = &changedBatchProvider{}
	if _, e = g.QueryTransfer(ctx, 501, b.Items[0].Transfer.TransferNo); !errors.Is(e, ErrConflict) {
		t.Fatal("claimed item queried another application", e)
	}
	if p.query.Load() != 0 || p.transfers.Load() != 1 {
		t.Fatal("wrong app touched upstream")
	}
}

func batchInputs(amounts ...int64) []TransferBatchInput {
	var out []TransferBatchInput
	for _, n := range amounts {
		out = append(out, TransferBatchInput{ChannelID: 7, Request: adapter.TransferRequest{Type: "wxpay", Account: "synthetic-openid", Name: "synthetic-name", Amount: n, Remark: "batch"}})
	}
	return out
}
func TestTransferBatchPrepareAtomicEncryptedIdempotentAndQueryUnsentLocal(t *testing.T) {
	s, g, p := batchFixture(t)
	ctx := context.Background()
	if _, e := g.PrepareTransferBatch(ctx, 501, "too-large", batchInputs(600, 600), batchMerchantActor); !errors.Is(e, ErrInsufficient) {
		t.Fatal(e)
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 1000 || b.Reserved != 0 {
		t.Fatal("partial reservation", b)
	}
	batch, e := g.PrepareTransferBatch(ctx, 501, "stable-batch", batchInputs(100, 200), batchMerchantActor)
	if e != nil || batch.Summary.Unsent != 2 || batch.CostAmount != 330 {
		t.Fatal(batch, e)
	}
	replay, e := g.PrepareTransferBatch(ctx, 501, "stable-batch", batchInputs(100, 200), batchMerchantActor)
	if e != nil || replay.ID != batch.ID {
		t.Fatal(replay, e)
	}
	if _, e = g.PrepareTransferBatch(ctx, 501, "stable-batch", batchInputs(200, 100), batchMerchantActor); !errors.Is(e, ErrConflict) {
		t.Fatal("batch order swapped", e)
	}
	for _, item := range batch.Items {
		q, e := g.QueryTransfer(ctx, 501, item.Transfer.TransferNo)
		if e != nil || q.Status != "unsent" {
			t.Fatal(q, e)
		}
	}
	if p.transfers.Load() != 0 || p.query.Load() != 0 {
		t.Fatal("prepare/unsent query called upstream")
	}
	var jobs int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE kind='transfer.reconcile'").Scan(&jobs); e != nil || jobs != 0 {
		t.Fatal("unsent scheduled work", jobs, e)
	}
}
func TestTransferBatchExecuteConcurrentSingleSendUnknownAndContinueOnlyUnsent(t *testing.T) {
	s, g, p := batchFixture(t)
	ctx := context.Background()
	p.unknown = true
	batch, e := g.PrepareTransferBatch(ctx, 501, "execute", batchInputs(100, 100, 100), batchMerchantActor)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); g.ExecuteTransferBatch(ctx, 501, batch.ID, 1, batchMerchantActor, nil) }()
	}
	wg.Wait()
	if p.transfers.Load() != 3 {
		t.Fatal("batch sent duplicates", p.transfers.Load())
	}
	current, e := s.TransferBatch(ctx, 501, batch.ID)
	if e != nil || current.Summary.Unknown != 3 {
		t.Fatal(current, e)
	}
	if _, e = g.ExecuteTransferBatch(ctx, 501, batch.ID, 100, batchMerchantActor, nil); e != nil || p.transfers.Load() != 3 {
		t.Fatal("unknown resent", e, p.transfers.Load())
	}
	current, e = g.QueryTransferBatch(ctx, 501, batch.ID, batchMerchantActor)
	if e != nil || current.Summary.Succeeded != 3 || p.query.Load() != 3 {
		t.Fatal(current, e, p.query.Load())
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 670 || b.Reserved != 0 {
		t.Fatal(b)
	}
}

func TestTransferBatchCancelOnlyUnsentAndRaceWithExecute(t *testing.T) {
	s, g, p := batchFixture(t)
	ctx := context.Background()
	p.unknown = true
	batch, e := g.PrepareTransferBatch(ctx, 501, "partial-cancel", batchInputs(100, 100), batchMerchantActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.Transfer(ctx, 501, 7, batch.Items[0].Transfer.TransferNo, batchInputs(100)[0].Request); e != nil || p.transfers.Load() != 0 {
		t.Fatal("single create acquired prepared batch send", e)
	}
	if _, e = g.ExecuteTransferBatch(ctx, 501, batch.ID, 1, batchMerchantActor, nil); e != nil {
		t.Fatal(e)
	}
	cancelled, e := g.CancelUnsentTransferBatch(ctx, 501, batch.ID, batchMerchantActor)
	if e != nil || cancelled.Summary.Cancelled != 1 || cancelled.Summary.Unknown != 1 {
		t.Fatal(cancelled, e)
	}
	if _, e = g.CancelUnsentTransferBatch(ctx, 501, batch.ID, batchMerchantActor); e != nil {
		t.Fatal(e)
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 890 || b.Reserved != 110 {
		t.Fatal("cancel released claimed funds", b)
	}
	second, e := g.PrepareTransferBatch(ctx, 501, "cancel-race", batchInputs(100), batchMerchantActor)
	if e != nil {
		t.Fatal(e)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	before := p.transfers.Load()
	go func() {
		defer wg.Done()
		<-start
		g.ExecuteTransferBatch(ctx, 501, second.ID, 1, batchMerchantActor, nil)
	}()
	go func() { defer wg.Done(); <-start; g.CancelUnsentTransferBatch(ctx, 501, second.ID, batchMerchantActor) }()
	close(start)
	wg.Wait()
	got, e := s.TransferBatch(ctx, 501, second.ID)
	if e != nil || p.transfers.Load()-before > 1 || got.Summary.Cancelled+got.Summary.Unknown != 1 {
		t.Fatal(got, e)
	}
}

type batchBankProvider struct{ bankTestProvider }

func (*batchBankProvider) ApplicationID() string { return "batch-bank-app" }
func TestTransferBatchBankCancelUsesUnsentProofWithoutInventedBankFailure(t *testing.T) {
	s, g, _ := batchFixture(t)
	ctx := context.Background()
	p := &batchBankProvider{}
	g.Providers[7] = p
	in := []TransferBatchInput{{ChannelID: 7, Request: adapter.TransferRequest{Type: "bank", Account: "6222020202020202020", Name: "Synthetic Person", BankCode: "1002", Amount: 100, Remark: "local"}}}
	batch, e := g.PrepareTransferBatch(ctx, 501, "unsent-bank", in, batchMerchantActor)
	if e != nil {
		t.Fatal(e)
	}
	cancelled, e := g.CancelUnsentTransferBatch(ctx, 501, batch.ID, batchMerchantActor)
	if e != nil || cancelled.Summary.Cancelled != 1 || cancelled.Items[0].Transfer.BankLifecycle != "cancelled" {
		t.Fatal(cancelled, e)
	}
	q, e := g.QueryTransfer(ctx, 501, cancelled.Items[0].Transfer.TransferNo)
	if e != nil || q.Status != "cancelled" || p.queries.Load() != 0 {
		t.Fatal("cancelled bank queried upstream", q, e)
	}
	var proofs, observations int
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM transfer_unsent_cancellations),(SELECT count(*) FROM bank_transfer_observations)").Scan(&proofs, &observations); e != nil || proofs != 1 || observations != 0 {
		t.Fatal("invented bank evidence", proofs, observations, e)
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 1000 || b.Reserved != 0 {
		t.Fatal(b)
	}
	if e = s.ScheduleBankRecheck(ctx, cancelled.Items[0].Transfer.ID); e != nil {
		t.Fatal("cancelled bank scheduled", e)
	}
}

func TestTransferBatchListOwnershipAndStableExternalNumberRecovery(t *testing.T) {
	s, g, _ := batchFixture(t)
	ctx := context.Background()
	first, e := g.PrepareTransferBatch(ctx, 501, "original-prepare-number", batchInputs(100), batchMerchantActor)
	if e != nil {
		t.Fatal(e)
	}
	second, e := g.PrepareTransferBatch(ctx, 501, "later-number", batchInputs(100), batchMerchantActor)
	if e != nil {
		t.Fatal(e)
	}
	list, e := s.ListTransferBatches(ctx, 501, "", 0, 1)
	if e != nil || list.Total != 2 || len(list.Items) != 1 || list.Items[0].ID != second.ID || list.NextBeforeID != second.ID {
		t.Fatal(list, e)
	}
	next, e := s.ListTransferBatches(ctx, 501, "", list.NextBeforeID, 1)
	if e != nil || len(next.Items) != 1 || next.Items[0].ID != first.ID {
		t.Fatal(next, e)
	}
	found, e := s.ListTransferBatches(ctx, 501, first.ExternalNo, 0, 20)
	if e != nil || found.Total != 1 || len(found.Items) != 1 || found.Items[0].ID != first.ID {
		t.Fatal(found, e)
	}
	other, e := s.ListTransferBatches(ctx, 502, first.ExternalNo, 0, 20)
	if e != nil || other.Total != 0 || len(other.Items) != 0 {
		t.Fatal(other, e)
	}
	if _, e = s.TransferBatch(ctx, 502, first.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestTransferBatchAuditFailClosedAndIntentHashBindsRecipient(t *testing.T) {
	s, g, p := batchFixture(t)
	ctx := context.Background()
	_, e := s.pool.Exec(ctx, `CREATE FUNCTION batch_test_fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='transfer-batches' THEN RAISE EXCEPTION 'test audit rejected'; END IF; RETURN NEW; END $$; CREATE TRIGGER batch_test_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION batch_test_fail_audit()`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.PrepareTransferBatch(ctx, 501, "audit-fail", batchInputs(100), batchMerchantActor); e == nil {
		t.Fatal("prepare audit failed open")
	}
	balance, _ := s.Balance(ctx, 501)
	if balance.Available != 1000 || balance.Reserved != 0 {
		t.Fatal(balance)
	}
	if _, e = s.pool.Exec(ctx, "DROP TRIGGER batch_test_audit ON backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	batch, e := g.PrepareTransferBatch(ctx, 501, "intent", batchInputs(100), batchMerchantActor)
	if e != nil {
		t.Fatal(e)
	}
	for _, field := range []string{"account", "name", "channel", "amount", "remark"} {
		in := batchInputs(100)
		switch field {
		case "account":
			in[0].Request.Account = "other"
		case "name":
			in[0].Request.Name = "other"
		case "channel":
			g.Providers[8] = p
			in[0].ChannelID = 8
		case "amount":
			in[0].Request.Amount = 101
		case "remark":
			in[0].Request.Remark = "other"
		}
		if _, e = g.PrepareTransferBatch(ctx, 501, "intent", in, batchMerchantActor); !errors.Is(e, ErrConflict) {
			t.Fatal(field, e)
		}
	}
	if _, e = s.pool.Exec(ctx, "CREATE TRIGGER batch_test_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION batch_test_fail_audit()"); e != nil {
		t.Fatal(e)
	}
	if _, e = g.ExecuteTransferBatch(ctx, 501, batch.ID, 1, batchMerchantActor, nil); e == nil || p.transfers.Load() != 0 {
		t.Fatal("claim audit failed open", e, p.transfers.Load())
	}
	if _, e = g.CancelUnsentTransferBatch(ctx, 501, batch.ID, batchMerchantActor); e == nil {
		t.Fatal("cancel audit failed open")
	}
	current, _ := s.TransferBatch(ctx, 501, batch.ID)
	balance, _ = s.Balance(ctx, 501)
	if current.Summary.Unsent != 1 || balance.Reserved != 110 {
		t.Fatal(current, balance)
	}
	var audit string
	if e = s.pool.QueryRow(ctx, "SELECT COALESCE(string_agg(changes::text,''),'') FROM backoffice_audit").Scan(&audit); e != nil || strings.Contains(audit, "synthetic-openid") || strings.Contains(audit, "synthetic-name") {
		t.Fatal("private recipient audit", audit, e)
	}
}
