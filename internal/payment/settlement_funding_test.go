package payment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestSettlementFundingOwnsGrossAndRejectCannotBorrowAnotherReservation(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	first, e := s.RequestFunds(ctx, "settlements", 681, 0, "scoped-first", 100, "merchant:681")
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.RequestFunds(ctx, "settlements", 681, 0, "scoped-second", 200, "merchant:681")
	if e != nil {
		t.Fatal(e)
	}
	var firstAccount, secondAccount, balance, pooled int64
	if e = s.pool.QueryRow(ctx, "SELECT account_id FROM settlement_fundings WHERE request_id=$1", first.ID).Scan(&firstAccount); e != nil {
		t.Fatal("missing dedicated funding", e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT account_id FROM settlement_fundings WHERE request_id=$1", second.ID).Scan(&secondAccount); e != nil || firstAccount == secondAccount {
		t.Fatal(firstAccount, secondAccount, e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE id=$1 AND scope_id=$2 AND kind='settlement_reserve'", firstAccount, first.ID).Scan(&balance); e != nil || balance != 100 {
		t.Fatal(balance, e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=681 AND kind='reserved'").Scan(&pooled); e != nil || pooled != 0 {
		t.Fatal("settlement used pooled reserve", pooled, e)
	}
	b, _ := s.Balance(ctx, 681)
	if b.Available != 9700 || b.Reserved != 300 {
		t.Fatal("summary lost scoped funds", b)
	}
	// Ordinary reserve resolution cannot spend a settlement's dedicated funds.
	if _, e = s.ReserveTransfer(ctx, 681, "ordinary", 9800); !errors.Is(e, ErrInsufficient) {
		t.Fatal("reused scoped funds", e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.ReviewFundsRequest(ctx, first.ID, false, "admin:1"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	b, _ = s.Balance(ctx, 681)
	if b.Available != 9800 || b.Reserved != 200 {
		t.Fatal(b)
	}
	if _, e = s.ReviewFundsRequest(ctx, second.ID, true, "admin:1"); e != nil {
		t.Fatal(e)
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
		t.Fatal(report, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE accounts SET scope_id=$2 WHERE id=$1", firstAccount, second.ID); e == nil {
		t.Fatal("funding identity mutable")
	}
	if _, e = s.pool.Exec(ctx, "UPDATE settlement_fundings SET account_id=$2 WHERE request_id=$1", first.ID, secondAccount); e == nil {
		t.Fatal("funding binding mutable")
	}
}

func TestSettlementFundingProofGuardRejectsCrossSpendingAndReconcileDetectsDamage(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	v, e := s.RequestFunds(ctx, "settlements", 681, 0, "proof", 100, "merchant:681")
	if e != nil {
		t.Fatal(e)
	}
	var account, available int64
	if e = s.pool.QueryRow(ctx, "SELECT account_id FROM settlement_fundings WHERE request_id=$1", v.ID).Scan(&account); e != nil {
		t.Fatal(e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=681 AND kind='available'").Scan(&available); e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = postByAccountIDs(ctx, tx, "foreign-spend", "must rollback", account, available, 1); e != nil {
		tx.Rollback(ctx)
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e == nil {
		t.Fatal("foreign journal spent dedicated money")
	}
	report, e := s.CheckLedger(ctx)
	if e != nil || report.InvalidSettlementFundings != 0 {
		t.Fatal(report, e)
	}
	// A damaged restore can bypass triggers; the read-only checker must still see
	// a wrong request scope even though the two-entry journal remains balanced.
	if _, e = s.pool.Exec(ctx, `ALTER TABLE accounts DISABLE TRIGGER settlement_account_identity;ALTER TABLE accounts DISABLE TRIGGER settlement_account_funding;UPDATE accounts SET scope_id=scope_id+1000000 WHERE kind='settlement_reserve';SET CONSTRAINTS ALL IMMEDIATE;ALTER TABLE accounts ENABLE TRIGGER settlement_account_identity;ALTER TABLE accounts ENABLE TRIGGER settlement_account_funding`); e != nil {
		t.Fatal(e)
	}
	report, e = s.CheckLedger(ctx)
	if e != nil || report.InvalidJournals != 0 || report.InvalidAccounts != 0 || report.InvalidSettlementFundings != 1 {
		t.Fatal(report, e)
	}
}

func TestSettlementFundingHistoricalUnscopedRequestNotAdoptedAndRejectedFromOriginalPool(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var id int64
	if e = tx.QueryRow(ctx, "INSERT INTO ledger_requests(kind,merchant_id,external_no,amount) VALUES('settlements',681,'pre40-historical',100) RETURNING id").Scan(&id); e != nil {
		t.Fatal(e)
	}
	if e = post(ctx, tx, fmt.Sprintf("request:%d:reserve", id), "historical reserve", 681, "available", 681, "reserved", 100); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	old, e := s.SettlementRequest(ctx, 681, id)
	if e != nil || old.Funding.State != "legacy_unscoped" || old.SettlementRecipient.State != "missing" {
		t.Fatal(old, e)
	}
	if _, e = s.ReviewFundsRequest(ctx, id, false, "admin:1"); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM settlement_fundings").Scan(&count); e != nil || count != 0 {
		t.Fatal("legacy adopted", count, e)
	}
	balance, _ := s.Balance(ctx, 681)
	if balance.Available != 10000 || balance.Reserved != 0 {
		t.Fatal(balance)
	}
}
