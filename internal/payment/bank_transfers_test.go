package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"sync"
	"sync/atomic"
	"testing"
)

type bankTestProvider struct {
	flowProvider
	queryState  atomic.Value
	providerNo  atomic.Value
	fee         atomic.Int64
	feeKnown    atomic.Bool
	queries     atomic.Int32
	submitError error
}

func (*bankTestProvider) TransferTypes() []string { return []string{"bank"} }
func (p *bankTestProvider) Transfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	if p.submitError != nil {
		return adapter.TransferResult{}, p.submitError
	}
	return adapter.TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: p.providerNo.Load().(string), State: "PROCESSING", Amount: r.Amount, BankFeeAmount: p.fee.Load(), BankFeeKnown: p.feeKnown.Load()}, nil
}
func (p *bankTestProvider) QueryTransfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.queries.Add(1)
	return adapter.TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: p.providerNo.Load().(string), State: p.queryState.Load().(string), Amount: r.Amount, BankFeeAmount: p.fee.Load(), BankFeeKnown: p.feeKnown.Load(), PayDate: "2026-10-23 10:00:00"}, nil
}
func bankFixture(t *testing.T) (*Store, ProviderFlow, *bankTestProvider, Transfer) {
	t.Helper()
	s, flow, _ := transferFixture(t)
	p := &bankTestProvider{}
	p.queryState.Store("SUCCESS")
	p.providerNo.Store("bank-payment-1")
	p.fee.Store(2)
	p.feeKnown.Store(true)
	flow.Providers[7] = p
	tr, e := flow.Transfer(context.Background(), 501, 7, "bank", adapter.TransferRequest{Type: "bank", Account: "6222020202020202020", Name: "Synthetic Person", BankCode: "1002", Amount: 100, Remark: "test"})
	if e != nil {
		t.Fatal(e)
	}
	return s, flow, p, tr
}
func TestBankSuccessAndConcurrentReturnUseSeparateBalancedReversal(t *testing.T) {
	s, flow, p, tr := bankFixture(t)
	ctx := context.Background()
	b, _ := s.Balance(ctx, 501)
	if b.Available != 890 || b.Reserved != 110 {
		t.Fatal("bank acceptance settled funds", b)
	}
	paid, e := flow.QueryTransfer(ctx, 501, tr.TransferNo)
	if e != nil || paid.Status != "succeeded" {
		t.Fatal(paid, e)
	}
	var held, expense int64
	if e = s.pool.QueryRow(ctx, "SELECT COALESCE(sum(balance)FILTER(WHERE kind='bank_service_fee_hold'),0),COALESCE(sum(balance)FILTER(WHERE kind='bank_fee_expense'),0) FROM accounts WHERE owner_id=0").Scan(&held, &expense); e != nil || held != 10 || expense != -2 {
		t.Fatalf("bank/platform fee mixed held%d expense%d %v", held, expense, e)
	}
	p.queryState.Store("BANK_FAIL")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := flow.QueryTransfer(ctx, 501, tr.TransferNo)
			if e != nil || r.Status != "reversed" {
				t.Errorf("bank return %+v %v", r, e)
			}
		}()
	}
	wg.Wait()
	b, _ = s.Balance(ctx, 501)
	if b.Available != 1000 || b.Reserved != 0 {
		t.Fatal("bank return failed or double credited", b)
	}
	var history string
	if e = s.pool.QueryRow(ctx, "SELECT status FROM transfers WHERE id=$1", tr.ID).Scan(&history); e != nil || history != "succeeded" {
		t.Fatal("settled transfer history rewritten", history, e)
	}
	current, e := s.Transfer(ctx, 501, tr.TransferNo)
	raw, _ := json.Marshal(current)
	if e != nil || !json.Valid(raw) || current.Status != "reversed" {
		t.Fatal("bank lifecycle hidden", string(raw), e)
	}
	calls := p.queries.Load()
	flow.QueryTransfer(ctx, 501, tr.TransferNo)
	if p.queries.Load() != calls {
		t.Fatal("returned bank order queried again")
	}
	if e = s.pool.QueryRow(ctx, "SELECT COALESCE(sum(balance)FILTER(WHERE kind='bank_service_fee_hold'),0),COALESCE(sum(balance)FILTER(WHERE kind='bank_fee_expense'),0) FROM accounts WHERE owner_id=0").Scan(&held, &expense); e != nil || held != 0 || expense != 0 {
		t.Fatal("bank fee return missing", held, expense, e)
	}
}
func TestBankReturnMissingOrMismatchedProofDoesNotCredit(t *testing.T) {
	s, flow, p, tr := bankFixture(t)
	ctx := context.Background()
	if _, e := flow.QueryTransfer(ctx, 501, tr.TransferNo); e != nil {
		t.Fatal(e)
	}
	p.queryState.Store("BANK_FAIL")
	p.feeKnown.Store(false)
	r, e := flow.QueryTransfer(ctx, 501, tr.TransferNo)
	if e == nil || r.Status != "reversal_pending" {
		t.Fatal("missing fee silently credited", r, e)
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 890 || b.Reserved != 0 {
		t.Fatal("unverified bank return credited", b)
	}
	p.feeKnown.Store(true)
	p.fee.Store(3)
	r, e = flow.QueryTransfer(ctx, 501, tr.TransferNo)
	if e == nil || r.Status != "reversal_pending" {
		t.Fatal("changed fee silently accepted", r, e)
	}
	p.fee.Store(2)
	p.providerNo.Store("different-bank-payment")
	r, e = flow.QueryTransfer(ctx, 501, tr.TransferNo)
	if e == nil || r.Status != "reversal_pending" {
		t.Fatal("changed provider identity accepted", r, e)
	}
	p.providerNo.Store("bank-payment-1")
	r, e = flow.QueryTransfer(ctx, 501, tr.TransferNo)
	if e != nil || r.Status != "reversed" {
		t.Fatal("verified followup unable to resolve", r, e)
	}
}
func TestBankFirstObservedReturnReleasesOnlyOriginalReservation(t *testing.T) {
	s, flow, p, tr := bankFixture(t)
	ctx := context.Background()
	p.queryState.Store("BANK_FAIL")
	r, e := flow.QueryTransfer(ctx, 501, tr.TransferNo)
	if e != nil || r.Status != "reversed" {
		t.Fatal(r, e)
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 1000 || b.Reserved != 0 {
		t.Fatal("unsettled bank return minted funds", b)
	}
}
func TestBankSubmitRejectionRemainsUnknownUntilTrustedQuery(t *testing.T) {
	s, flow, _ := transferFixture(t)
	ctx := context.Background()
	p := &bankTestProvider{submitError: &adapter.ProviderError{Code: "NOTENOUGH"}}
	flow.Providers[7] = p
	r, e := flow.Transfer(ctx, 501, 7, "bank-error", adapter.TransferRequest{Type: "bank", Account: "6222020202020202020", Name: "Synthetic Person", BankCode: "1002", Amount: 100})
	if e == nil {
		t.Fatal("provider error missing")
	}
	var pe *adapter.ProviderError
	if !errors.As(e, &pe) {
		t.Fatal(e)
	}
	b, _ := s.Balance(ctx, 501)
	if r.Status != "unknown" || b.Available != 890 || b.Reserved != 110 {
		t.Fatal("uncertain bank rejection released reserve", r, b)
	}
}

func TestBankRecheckScheduleIsDurableUniqueAndStopsAfterReturn(t *testing.T) {
	s, flow, p, tr := bankFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.ScheduleBankRecheck(ctx, tr.ID); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	var tasks, generation int64
	var spaced bool
	if e := s.pool.QueryRow(ctx, "SELECT count(*),COALESCE(bool_and(run_at>now()+interval '5 hours' AND run_at<now()+interval '7 hours'),false) FROM jobs WHERE kind='transfer.bank_reconcile'").Scan(&tasks, &spaced); e != nil || tasks != 1 || !spaced {
		t.Fatal("unbounded or immediate bank recheck", tasks, spaced, e)
	}
	if e := s.pool.QueryRow(ctx, "SELECT generation FROM bank_transfer_lifecycle WHERE transfer_id=$1", tr.ID).Scan(&generation); e != nil || generation != 1 {
		t.Fatal("schedule generation raced", generation, e)
	}
	if _, e := flow.QueryTransfer(ctx, 501, tr.TransferNo); e != nil {
		t.Fatal(e)
	}
	p.queryState.Store("BANK_FAIL")
	if _, e := flow.QueryTransfer(ctx, 501, tr.TransferNo); e != nil {
		t.Fatal(e)
	}
	if e := s.ScheduleBankRecheck(ctx, tr.ID); e != nil {
		t.Fatal(e)
	}
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE kind='transfer.bank_reconcile'").Scan(&tasks); e != nil || tasks != 1 {
		t.Fatal("returned transfer rescheduled", tasks, e)
	}
}
func TestBankReturnLedgerAndObservationCommitAtomically(t *testing.T) {
	s, flow, p, tr := bankFixture(t)
	ctx := context.Background()
	if _, e := flow.QueryTransfer(ctx, 501, tr.TransferNo); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION bank_observation_failure_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.provider_state='BANK_FAIL' THEN RAISE EXCEPTION 'injected bank audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER bank_observation_failure_test BEFORE INSERT ON bank_transfer_observations FOR EACH ROW EXECUTE FUNCTION bank_observation_failure_test()`); e != nil {
		t.Fatal(e)
	}
	p.queryState.Store("BANK_FAIL")
	if _, e := flow.QueryTransfer(ctx, 501, tr.TransferNo); e == nil {
		t.Fatal("injected bank audit failure ignored")
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 890 || b.Reserved != 0 {
		t.Fatal("bank return ledger committed without observation", b)
	}
	var journals int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM journals WHERE event_key LIKE $1", fmt.Sprintf("transfer:%d:bank-return:%%", tr.ID)).Scan(&journals); e != nil || journals != 0 {
		t.Fatal("partial reverse journals survived", journals, e)
	}
	if _, e := s.pool.Exec(ctx, "DROP TRIGGER bank_observation_failure_test ON bank_transfer_observations"); e != nil {
		t.Fatal(e)
	}
	if _, e := flow.QueryTransfer(ctx, 501, tr.TransferNo); e != nil {
		t.Fatal(e)
	}
	b, _ = s.Balance(ctx, 501)
	if b.Available != 1000 || b.Reserved != 0 {
		t.Fatal("verified return cannot retry atomically", b)
	}
}
