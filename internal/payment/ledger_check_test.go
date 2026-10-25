package payment

import (
	"context"
	"testing"
)

func TestLedgerCheckDetectsRestoredDataCorruption(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 61001, Name: "local integrity fixture", APIKey: "test-only-key"}); err != nil {
		t.Fatal(err)
	}
	o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 61001, OutTradeNo: "check", Name: "fixture", Type: "sandbox", Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkPaid(ctx, o.TradeNo, "check-paid"); err != nil {
		t.Fatal(err)
	}
	r, err := s.CheckLedger(ctx)
	if err != nil || r.InvalidJournals != 0 || r.InvalidAccounts != 0 {
		t.Fatalf("healthy: %+v %v", r, err)
	}
	// Deliberate damage is confined to this test's disposable schema. This models
	// a broken restore/import that bypassed database constraint triggers.
	_, err = s.pool.Exec(ctx, `ALTER TABLE journals DISABLE TRIGGER journal_balanced;
	 INSERT INTO journals(event_key,description) VALUES('damaged-empty-journal','test only');
	 ALTER TABLE journals ENABLE TRIGGER journal_balanced;
	 ALTER TABLE accounts DISABLE TRIGGER account_balance_guard;
	 UPDATE accounts SET balance=balance+1 WHERE owner_id=61001 AND kind='available';
	 ALTER TABLE accounts ENABLE TRIGGER account_balance_guard`)
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.CheckLedger(ctx)
	if err != nil || r.InvalidJournals != 1 || r.InvalidAccounts != 1 {
		t.Fatalf("damaged: %+v %v", r, err)
	}
}
