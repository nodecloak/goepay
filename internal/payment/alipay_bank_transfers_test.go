package payment

import (
	"context"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"sync"
	"sync/atomic"
	"testing"
)

type alipayBankProvider struct {
	flowProvider
	writes, queries atomic.Int32
	mode            atomic.Value
	lost            bool
}

func (*alipayBankProvider) TransferTypes() []string     { return []string{"bank"} }
func (*alipayBankProvider) BankTransferProduct() string { return adapter.BankProductAlipay }
func (p *alipayBankProvider) Transfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.writes.Add(1)
	if p.lost {
		return adapter.TransferResult{}, &adapter.UnknownOutcome{Operation: "ali-bank", Cause: errors.New("lost acknowledgement")}
	}
	return adapter.TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: "ali-bank-1", Amount: r.Amount, State: "PROCESSING", AlipayBank: &adapter.AlipayBankEvidence{Product: adapter.BankProductAlipay, SubmissionBound: true}}, nil
}
func (p *alipayBankProvider) QueryTransfer(_ context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.queries.Add(1)
	state := "SUCCESS"
	known := true
	if p.mode.Load() == "refund-missing" {
		state = "REFUND"
		known = false
	}
	if p.mode.Load() == "refund" {
		state = "REFUND"
	}
	if p.mode.Load() == "fail" {
		state = "FAIL"
	}
	if p.mode.Load() == "fail-missing" {
		state = "FAIL"
		known = false
	}
	no := "ali-bank-1"
	if p.mode.Load() == "wrong-order" {
		no = "wrong-bank-order"
	}
	return adapter.TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: no, Amount: r.Amount, State: state, PayDate: "2026-10-23 12:00:00", AlipayBank: &adapter.AlipayBankEvidence{Product: adapter.BankProductAlipay, AmountKnown: known, EstimatedFeeKnown: true, EstimatedFee: 12}}, nil
}
func aliBankFixture(t *testing.T) (*Store, ProviderFlow, *alipayBankProvider) {
	t.Helper()
	s, g, _ := transferFixture(t)
	p := &alipayBankProvider{}
	p.mode.Store("success")
	g.Providers[7] = p
	return s, g, p
}
func TestAlipayBankBoundSuccessAndLateRefundNeedExactEvidence(t *testing.T) {
	s, g, p := aliBankFixture(t)
	ctx := context.Background()
	r := adapter.TransferRequest{Type: "bank", Account: "6222020000000000", Name: "Synthetic Person", Amount: 100, Remark: "local"}
	tx, e := g.Transfer(ctx, 501, 7, "ali-bank", r)
	if e != nil || tx.BankProduct != adapter.BankProductAlipay || tx.Status != "pending" {
		t.Fatal("ack finalized without amount", tx, e)
	}
	if e = s.resolveBankTransferResult(ctx, tx, adapter.TransferResult{TransferNo: fmt.Sprintf("TF%d", tx.ID), ProviderTransferNo: "ali-bank-1", Amount: 100, State: "SUCCESS", BankFeeKnown: true}); !errors.Is(e, ErrInvalid) {
		t.Fatal("Ali bank entered WeChat fee/return resolver", e)
	}
	tx, e = g.QueryTransfer(ctx, 501, "ali-bank")
	if e != nil || tx.Status != "succeeded" || tx.BankFeeKnown || !tx.BankEstimatedFeeKnown || tx.BankEstimatedFeeAmount != 12 {
		t.Fatal(tx, e)
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 890 || b.Reserved != 0 || tx.BankServiceFeeHeld != 10 {
		t.Fatal("bank success financials", b, tx)
	}
	p.mode.Store("refund-missing")
	tx, e = g.QueryTransfer(ctx, 501, "ali-bank")
	if !errors.Is(e, ErrBankReversalPending) || tx.Status != "reversal_pending" || tx.LedgerStatus != "succeeded" {
		t.Fatal("missing refund amount treated as settled success", tx, e)
	}
	b, _ = s.Balance(ctx, 501)
	if b.Available != 890 {
		t.Fatal("unproven refund credited", b)
	}
	p.mode.Store("success")
	tx, e = g.QueryTransfer(ctx, 501, "ali-bank")
	if !errors.Is(e, ErrBankReversalPending) || tx.Status != "reversal_pending" {
		t.Fatal("later success erased observed return", tx, e)
	}
	p.mode.Store("refund")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := g.QueryTransfer(ctx, 501, "ali-bank"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	tx, e = s.Transfer(ctx, 501, "ali-bank")
	b, _ = s.Balance(ctx, 501)
	if e != nil || tx.Status != "reversed" || b.Available != 1000 || b.Reserved != 0 || tx.BankReturnedPrincipal != 100 || tx.BankReturnedServiceFee != 10 || tx.BankReturnedFee != 0 || len(tx.BankReversalJournals) != 2 {
		t.Fatal("reversal not exact once", tx, b, e)
	}
	var feeExpense int64
	if e = s.pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=0 AND kind='bank_fee_expense'").Scan(&feeExpense); e != nil || feeExpense != 0 {
		t.Fatal("estimated Ali fee entered actual expense ledger", feeExpense, e)
	}
	if p.writes.Load() != 1 {
		t.Fatal("bank resubmitted", p.writes.Load())
	}
	if _, e = s.pool.Exec(ctx, "UPDATE bank_transfer_lifecycle SET provider_kind='wxpay_v2' WHERE transfer_id=$1", tx.ID); e == nil {
		t.Fatal("bank provider kind mutable")
	}
	if _, e = s.pool.Exec(ctx, "UPDATE alipay_bank_bindings SET request_digest=decode(repeat('00',32),'hex') WHERE transfer_id=$1", tx.ID); e == nil {
		t.Fatal("original recipient request binding mutable")
	}
	if report, e := s.CheckLedger(ctx); e != nil || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
		t.Fatal(report, e)
	}
}
func TestAlipayBankLostOriginalBindingNeverAssumesRecipient(t *testing.T) {
	s, g, p := aliBankFixture(t)
	p.lost = true
	ctx := context.Background()
	r := adapter.TransferRequest{Type: "bank", Account: "6222020000000000", Name: "Synthetic Person", Amount: 100, Remark: "local"}
	tx, e := g.Transfer(ctx, 501, 7, "lost-ali-bank", r)
	if e == nil || tx.ID == 0 {
		t.Fatal(tx, e)
	}
	tx, e = g.QueryTransfer(ctx, 501, "lost-ali-bank")
	if !errors.Is(e, ErrBankReversalPending) || tx.Status != "reversal_pending" {
		t.Fatal("query fabricated original card/product identity", tx, e)
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 890 || b.Reserved != 110 {
		t.Fatal("unknown original submission released money", b)
	}
	if _, e = g.Transfer(ctx, 501, 7, "lost-ali-bank", r); e != nil {
		t.Fatal("idempotent replay failed", e)
	}
	if p.writes.Load() != 1 {
		t.Fatal("unknown bank resubmitted")
	}
}

func TestAlipayBankAuditFailureRollsBackRefundAndContradictionNeedsReview(t *testing.T) {
	s, g, p := aliBankFixture(t)
	ctx := context.Background()
	r := adapter.TransferRequest{Type: "bank", Account: "6222020000000000", Name: "Synthetic Person", Amount: 100, Remark: "local"}
	tx, e := g.Transfer(ctx, 501, 7, "atomic-bank", r)
	if e != nil {
		t.Fatal(e)
	}
	p.mode.Store("wrong-order")
	if v, e := g.QueryTransfer(ctx, 501, tx.TransferNo); !errors.Is(e, ErrBankReversalPending) || v.Status != "reversal_pending" {
		t.Fatal("wrong provider order released or settled reservation", v, e)
	}
	b, _ := s.Balance(ctx, 501)
	if b.Available != 890 || b.Reserved != 110 {
		t.Fatal(b)
	}
	p.mode.Store("success")
	if _, e = g.QueryTransfer(ctx, 501, tx.TransferNo); e != nil {
		t.Fatal(e)
	}
	p.mode.Store("fail")
	if v, e := g.QueryTransfer(ctx, 501, tx.TransferNo); !errors.Is(e, ErrBankReversalPending) || v.Status != "reversal_pending" {
		t.Fatal("ordinary FAIL mistaken for a refund", v, e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION fail_ali_refund_observation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.provider_state='REFUND' THEN RAISE EXCEPTION 'injected observation failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_ali_refund_observation BEFORE INSERT ON alipay_bank_observations FOR EACH ROW EXECUTE FUNCTION fail_ali_refund_observation()`); e != nil {
		t.Fatal(e)
	}
	p.mode.Store("refund")
	if _, e = g.QueryTransfer(ctx, 501, tx.TransferNo); e == nil {
		t.Fatal("observation failure ignored")
	}
	b, _ = s.Balance(ctx, 501)
	if b.Available != 890 || b.Reserved != 0 {
		t.Fatal("refund committed without immutable observation", b)
	}
	var journals int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM journals WHERE event_key LIKE $1", fmt.Sprintf("transfer:%d:bank-return:%%", tx.ID)).Scan(&journals); e != nil || journals != 0 {
		t.Fatal(journals, e)
	}
	if _, e = s.pool.Exec(ctx, "DROP TRIGGER fail_ali_refund_observation ON alipay_bank_observations"); e != nil {
		t.Fatal(e)
	}
	if _, e = g.QueryTransfer(ctx, 501, tx.TransferNo); e != nil {
		t.Fatal(e)
	}
	b, _ = s.Balance(ctx, 501)
	if b.Available != 1000 || b.Reserved != 0 {
		t.Fatal(b)
	}
}
