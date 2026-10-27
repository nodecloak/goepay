package payment

import (
	"context"
	"encoding/json"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
)

func sharingPaymentRule(t *testing.T, g ProviderFlow, rate int64) ChannelSharingRuleView {
	t.Helper()
	ctx := context.Background()
	r, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "payment-receiver", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, r.ID, "payment-receiver-bind", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	rule, e := g.SetChannelSharingRule(ctx, 751, 7, ChannelSharingRuleInput{Enabled: true, Methods: []string{"native"}, Items: []ChannelSharingRuleItem{{ReceiverID: r.ID, Mode: "rate", RateBPS: rate}}}, 0, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	return rule
}
func TestChannelSharingPaymentDeclarationAndNetReserveAreOriginalAndAtomic(t *testing.T) {
	var declared atomic.Bool
	s, g, calls, _ := sharingReceiverFlowFixture(t, func(r *http.Request, in map[string]json.RawMessage) {
		if r.URL.Path == "/v3/pay/transactions/native" {
			var info struct {
				ProfitSharing bool `json:"profit_sharing"`
			}
			json.Unmarshal(in["settle_info"], &info)
			declared.Store(info.ProfitSharing)
		}
	})
	ctx := context.Background()
	rule := sharingPaymentRule(t, g, 2000)
	g.CallbackBaseURL = "https://local.example.invalid"
	// The original order retains a fee, and its entire net must stay unavailable
	// while upstream settlement is frozen, including the unallocated remainder.
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"payment_fee_enabled","value":"true"}'); INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"sharing-fee","enabled":true,"rate_bps":"1000"}')`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_build_object('group_id',(SELECT id::text FROM backoffice_records WHERE resource='groups' AND body->>'name'='sharing-fee')) WHERE id=751`); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "declared-original", Type: "wxpay", Amount: 1000, Name: "local item"})
	if e != nil || o.MerchantAmount != 900 {
		t.Fatal(o, e)
	}
	attempt, e := g.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", "")
	if e != nil || !declared.Load() || calls.Load() != 2 {
		t.Fatal(attempt, e, declared.Load(), calls.Load())
	}
	original, e := s.ChannelSharingPayment(ctx, 751, o.TradeNo, channelSharingActor)
	if e != nil || !original.Declared || original.RuleID != rule.ID || original.GrossAmount != 1000 || original.MerchantNet != 900 || len(original.Items) != 1 || original.Items[0].Amount != 200 {
		t.Fatal(original, e)
	}
	if _, e = s.DisableChannelSharingRule(ctx, 751, 7, rule.ID, channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e != nil || calls.Load() != 2 {
		t.Fatal("replay changed declaration or sent twice", e, calls.Load())
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.markPaid(ctx, o.TradeNo, "sharing-paid", true, PaidReceipt{RealMoney: o.Amount, ChannelID: 7, ProviderTradeNo: "original-provider-trade"}); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	b, e := s.Balance(ctx, 751)
	if e != nil || b.Available != 0 {
		t.Fatal("frozen upstream income was spendable", b, e)
	}
	var reserve, fee, journals int64
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT balance FROM accounts WHERE owner_id=751 AND kind='channel_sharing_reserve' AND scope_id=$1),(SELECT balance FROM accounts WHERE owner_id=0 AND kind='fees'),(SELECT count(*) FROM journals)", o.ID).Scan(&reserve, &fee, &journals); e != nil || reserve != 900 || fee != 100 || journals != 3 {
		t.Fatal(reserve, fee, journals, e)
	}
	if _, e = s.ReserveRefund(ctx, 751, o.OutTradeNo, "sharing-refund-blocked", 100); !errors.Is(e, ErrState) {
		t.Fatal("refund borrowed another order's income while upstream frozen", e)
	}
	if _, e = s.OrderIncomeFreezable(ctx, 751, o.TradeNo); !errors.Is(e, ErrState) {
		t.Fatal("frozen channel income was frozen twice", e)
	}
	if _, e = s.ExecuteProfitRule(ctx, 1, o.TradeNo, "merchant:751"); !errors.Is(e, ErrState) {
		t.Fatal("channel reserve was reused by an internal order allocation", e)
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 751, rule.Items[0].ReceiverID, "sharing-order-unbind", channelSharingActor); !errors.Is(e, ErrState) {
		t.Fatal("disabled current policy erased open payment's relation", e)
	}
}

func TestChannelSharingPaymentNoRuleIsAnImmutableNegativeDeclaration(t *testing.T) {
	var declared atomic.Bool
	s, g, calls, _ := sharingReceiverFlowFixture(t, func(r *http.Request, in map[string]json.RawMessage) {
		if r.URL.Path == "/v3/pay/transactions/native" {
			_, present := in["settle_info"]
			declared.Store(present)
		}
	})
	ctx := context.Background()
	g.CallbackBaseURL = "https://local.example.invalid"
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "negative-declaration", Type: "wxpay", Amount: 101, Name: "item"})
	if e != nil {
		t.Fatal(e)
	}
	a, e := g.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", "")
	if e != nil || declared.Load() {
		t.Fatal(a, e, declared.Load())
	}
	before := calls.Load()
	sharingPaymentRule(t, g, 1000)
	if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e != nil || calls.Load() != before+1 {
		t.Fatal("new configuration adopted a previous ordinary payment", e, calls.Load())
	}
	original, e := s.ChannelSharingPayment(ctx, 751, o.TradeNo, channelSharingActor)
	if e != nil || original.Declared || original.RuleID != 0 || len(original.Items) != 0 {
		t.Fatal(original, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE provider_attempt_sharing SET declared=true WHERE attempt_id=$1", a.ID); e == nil {
		t.Fatal("original negative declaration was mutable")
	}
	if e = s.markPaid(ctx, o.TradeNo, "negative-paid", true, PaidReceipt{RealMoney: o.Amount, ChannelID: 7, ProviderTradeNo: "negative-provider"}); e != nil {
		t.Fatal(e)
	}
	b, e := s.Balance(ctx, 751)
	if e != nil || b.Available != 101 {
		t.Fatal(b, e)
	}
}

func TestChannelSharingPaymentT1DoesNotBlockOldAvailableFundsTwice(t *testing.T) {
	s, g, _, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	g.CallbackBaseURL = "https://local.example.invalid"
	old, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "old-unfrozen-income", Type: "wxpay", Amount: 100, Name: "old"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, old.TradeNo, "old-unfrozen-paid"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE orders SET paid_at=now()-interval '2 days' WHERE id=$1", old.ID); e != nil {
		t.Fatal(e)
	}
	sharingPaymentRule(t, g, 1000)
	today, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "today-sharing", Type: "wxpay", Amount: 1000, Name: "today"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.Submit(ctx, today.TradeNo, 7, "native", "127.0.0.1", ""); e != nil {
		t.Fatal(e)
	}
	if e = s.markPaid(ctx, today.TradeNo, "today-sharing-paid", true, PaidReceipt{RealMoney: today.Amount, ChannelID: 7, ProviderTradeNo: "today-upstream"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"settle_type","value":"1"}')`); e != nil {
		t.Fatal(e)
	}
	available, e := s.FundsAvailability(ctx, 751)
	if e != nil || available.LedgerAvailable != 100 || available.Available != 100 || available.TodayGross != 0 {
		t.Fatal("T1 double-blocked an already isolated sharing reserve", available, e)
	}
}

func TestChannelSharingFundingCheckerDetectsMissingProjectionAndRejectsForeignSpend(t *testing.T) {
	s, g, _, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	g.CallbackBaseURL = "https://local.example.invalid"
	sharingPaymentRule(t, g, 1000)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "funding-integrity", Type: "wxpay", Amount: 100, Name: "item"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e != nil {
		t.Fatal(e)
	}
	if e = s.markPaid(ctx, o.TradeNo, "sharing-integrity-paid", true, PaidReceipt{RealMoney: o.Amount, ChannelID: 7, ProviderTradeNo: "integrity-upstream"}); e != nil {
		t.Fatal(e)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidChannelSharingFundings != 0 {
		t.Fatal(report, e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var source, available int64
	if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE kind='channel_sharing_reserve' AND scope_id=$1", o.ID).Scan(&source); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=751 AND kind='available'").Scan(&available); e != nil {
		t.Fatal(e)
	}
	if e = postByAccountIDs(ctx, tx, "forged:sharing-spend", "must reject", source, available, 1); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e == nil {
		t.Fatal("non-sharing journal spent the dedicated original reserve")
	}
	if _, e = s.pool.Exec(ctx, "UPDATE accounts SET scope_id=scope_id+100000 WHERE id=$1", source); e == nil {
		t.Fatal("original order reserve was rebound")
	}
	// No constraints are disabled: changing the pooled clearing classification
	// removes a required original credit proof from the projection.
	if _, e = s.pool.Exec(ctx, "UPDATE accounts SET kind='reserved' WHERE owner_id=0 AND kind='clearing'"); e != nil {
		t.Fatal(e)
	}
	report, e = s.CheckLedger(ctx)
	if e != nil || report.InvalidChannelSharingFundings != 1 {
		t.Fatal("missing valid projection was counted as healthy", report, e)
	}
}

func TestChannelSharingPaymentProofAndFundingFailuresRollbackBeforeAnyPartialCommit(t *testing.T) {
	s, g, calls, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	g.CallbackBaseURL = "https://local.example.invalid"
	sharingPaymentRule(t, g, 1000)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "proof-rollback", Type: "wxpay", Amount: 100, Name: "item"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION reject_sharing_payment_proof_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected sharing proof failure'; END $$;CREATE TRIGGER reject_sharing_payment_proof_test BEFORE INSERT ON provider_attempt_sharing FOR EACH ROW EXECUTE FUNCTION reject_sharing_payment_proof_test()`); e != nil {
		t.Fatal(e)
	}
	if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e == nil || calls.Load() != 1 {
		t.Fatal("failed declaration reached payment provider", e, calls.Load())
	}
	var attempts, jobs, proofs int
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM provider_attempts),(SELECT count(*) FROM jobs),(SELECT count(*) FROM provider_attempt_accounts)").Scan(&attempts, &jobs, &proofs); e != nil || attempts != 0 || jobs != 0 || proofs != 0 {
		t.Fatal(attempts, jobs, proofs, e)
	}
	if _, e = s.pool.Exec(ctx, "DROP TRIGGER reject_sharing_payment_proof_test ON provider_attempt_sharing"); e != nil {
		t.Fatal(e)
	}
	if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e != nil || calls.Load() != 2 {
		t.Fatal(e, calls.Load())
	}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION reject_sharing_funding_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected sharing funding failure'; END $$;CREATE TRIGGER reject_sharing_funding_test BEFORE INSERT ON channel_sharing_fundings FOR EACH ROW EXECUTE FUNCTION reject_sharing_funding_test()`); e != nil {
		t.Fatal(e)
	}
	receipt := PaidReceipt{RealMoney: o.Amount, ChannelID: 7, ProviderTradeNo: "rollback-upstream"}
	if e = s.markPaid(ctx, o.TradeNo, "rollback-paid", true, receipt); e == nil {
		t.Fatal("funding failure still acknowledged a paid order")
	}
	current, e := s.OrderByTradeNo(ctx, o.TradeNo)
	if e != nil || current.Status != "pending" {
		t.Fatal(current, e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM journals),(SELECT count(*) FROM inbox),(SELECT count(*) FROM verified_payment_receipts)").Scan(&attempts, &jobs, &proofs); e != nil || attempts != 0 || jobs != 0 || proofs != 0 {
		t.Fatal("partial paid evidence escaped rollback", attempts, jobs, proofs, e)
	}
	if _, e = s.pool.Exec(ctx, "DROP TRIGGER reject_sharing_funding_test ON channel_sharing_fundings"); e != nil {
		t.Fatal(e)
	}
	if e = s.markPaid(ctx, o.TradeNo, "rollback-paid", true, receipt); e != nil {
		t.Fatal(e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM channel_sharing_fundings").Scan(&proofs); e != nil || proofs != 1 {
		t.Fatal(proofs, e)
	}
}

func TestChannelSharingPaymentPreparationCannotCrossPolicyCASAndCannotReserveMoreThanOriginalNet(t *testing.T) {
	s, g, calls, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	g.CallbackBaseURL = "https://local.example.invalid"
	rule := sharingPaymentRule(t, g, 1000)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "snapshot-cas", Type: "wxpay", Amount: 100, Name: "item"})
	if e != nil {
		t.Fatal(e)
	}
	scope, e := adapter.TransferAccountScope(g.Providers[7])
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := g.prepareSharingPayment(ctx, o, 7, "native", "local-app", scope)
	if e != nil {
		t.Fatal(e)
	}
	disabled, e := s.DisableChannelSharingRule(ctx, 751, 7, rule.ID, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.beginProviderAttemptWithIdentity(ctx, o.TradeNo, 7, "native", "local-app", "", scope, &prepared); !errors.Is(e, ErrConflict) {
		t.Fatal("stale prepared declaration crossed configuration change", e)
	}
	var attempts int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM provider_attempts").Scan(&attempts); e != nil || attempts != 0 || calls.Load() != 1 {
		t.Fatal(attempts, e, calls.Load())
	}
	// Applying a fixed share larger than this particular order must fail before
	// claiming a payment, even when the merchant has unrelated available income.
	input := rule.ChannelSharingRuleInput
	input.Items = []ChannelSharingRuleItem{{ReceiverID: rule.Items[0].ReceiverID, Mode: "fixed", FixedAmount: 101}}
	if _, e = g.SetChannelSharingRule(ctx, 751, 7, input, disabled.ID, channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); !errors.Is(e, ErrInsufficient) || calls.Load() != 1 {
		t.Fatal("overallocated original payment reached provider", e, calls.Load())
	}
}
