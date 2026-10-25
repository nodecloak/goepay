package payment

import (
	"context"
	adapter "goepay/internal/adapters/payment"
	"testing"
)

func TestLedgerCheckDetectsMissingBankFundingProjectionAfterAccountIdentityDamage(t *testing.T) {
	s, g, v, _ := settlementBankFixture(t, adapter.BankProductWechat)
	ctx := context.Background()
	if _, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil); e != nil {
		t.Fatal(e)
	}
	// Preserve account ID, balance and journal foreign keys. Only its identity
	// changes, reproducing a damaged restore without disabling constraints.
	if _, e := s.pool.Exec(ctx, "UPDATE accounts SET kind='reserved' WHERE owner_id=0 AND kind='clearing' AND scope_id=0"); e != nil {
		t.Fatal(e)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
		t.Fatal("fixture did not isolate missing funding projection", report, e)
	}
	if report.InvalidSettlementFundings != 1 {
		t.Fatal("missing bank funding projection escaped reconciliation", report)
	}
}
