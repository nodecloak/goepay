package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

// An isolated test schema shadows only this connection's SQL clock. There is no
// production clock override, application hook, or dependency on wall midnight.
func settlementClockStore(t *testing.T, s *Store, at string) *Store {
	t.Helper()
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `CREATE TABLE settlement_test_clock(moment timestamptz NOT NULL); CREATE FUNCTION clock_timestamp() RETURNS timestamptz LANGUAGE sql VOLATILE AS 'SELECT moment FROM settlement_test_clock'`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, "INSERT INTO settlement_test_clock VALUES($1::timestamptz)", at); e != nil {
		t.Fatal(e)
	}
	config := s.pool.Config()
	config.ConnConfig.RuntimeParams["search_path"] += ",pg_catalog"
	pool, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	controlled, e := NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	return controlled
}

func TestSettlementTermsAccountLockCrossMidnightUsesOneOperationDay(t *testing.T) {
	original := testStore(t)
	settlementMerchant(t, original)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	transferSetting(t, original, "settlement_daily_limit", "1")
	s := settlementClockStore(t, original, "2042-06-01T15:59:59Z")
	locker, e := original.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer locker.Rollback(context.Background())
	if _, e = locker.Exec(ctx, "SELECT id FROM accounts WHERE owner_id=681 AND kind='available' FOR UPDATE"); e != nil {
		t.Fatal(e)
	}
	connection, e := s.pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer connection.Release()
	pid := connection.Conn().PgConn().PID()
	tx, e := connection.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	type result struct {
		request LedgerRequest
		err     error
	}
	completed := make(chan result, 1)
	go func() {
		v, err := s.requestFundsTxExpected(ctx, tx, "settlements", 681, 0, "midnight-wait", 100, "merchant:681", nil)
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		completed <- result{v, err}
	}()
	waiting := false
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if e = original.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock' AND query LIKE '%accounts%')", pid).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !waiting {
		t.Fatal("request never reached the held account lock")
	}
	const afterMidnight = "2042-06-01T16:00:01Z"
	if _, e = original.pool.Exec(ctx, "UPDATE settlement_test_clock SET moment=$1::timestamptz", afterMidnight); e != nil {
		t.Fatal(e)
	}
	if e = locker.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	out := <-completed
	if out.err != nil {
		t.Fatal(out.err)
	}
	wanted, _ := time.Parse(time.RFC3339, afterMidnight)
	if !out.request.CreatedAt.Equal(wanted) {
		t.Fatalf("reservation used transaction time or pre-lock day: got %s want %s", out.request.CreatedAt, wanted)
	}
	if _, e = s.RequestFunds(ctx, "settlements", 681, 0, "new-day-limit", 100, "merchant:681"); !errors.Is(e, ErrState) {
		t.Fatalf("midnight reservation did not consume its operation day's limit: %v", e)
	}
	balance, e := s.Balance(ctx, 681)
	if e != nil || balance.Available != 9900 || balance.Reserved != 100 {
		t.Fatal("rejected request changed funds", balance, e)
	}
}

func TestSettlementTermsDailyWindowExcludesFutureBusinessDay(t *testing.T) {
	original := testStore(t)
	settlementMerchant(t, original)
	ctx := context.Background()
	transferSetting(t, original, "settlement_daily_limit", "1")
	s := settlementClockStore(t, original, "2042-06-01T16:00:01Z")
	if _, e := original.pool.Exec(ctx, `INSERT INTO ledger_requests(kind,merchant_id,external_no,amount,created_at) VALUES('settlements',681,'future-record',100,'2042-06-02T16:00:00Z')`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.RequestFunds(ctx, "settlements", 681, 0, "current-day", 100, "merchant:681"); e != nil {
		t.Fatal("a different business day's record consumed the current daily limit", e)
	}
}
