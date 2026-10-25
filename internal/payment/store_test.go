package payment

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func testStore(t testing.TB) *Store {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	p, e := pgxpool.New(context.Background(), dsn)
	if e != nil {
		t.Fatal(e)
	}
	nonce := make([]byte, 8)
	if _, e = rand.Read(nonce); e != nil {
		t.Fatal(e)
	}
	schema := "test_" + hex.EncodeToString(nonce)
	if _, e = p.Exec(context.Background(), "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	config, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	scoped, e := pgxpool.NewWithConfig(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	paths, e := filepath.Glob("../../db/migrations/*.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range paths {
		migration, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = scoped.Exec(context.Background(), string(migration)); e != nil {
			t.Fatal(e)
		}
	}
	t.Cleanup(func() {
		scoped.Close()
		_, _ = p.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		p.Close()
	})
	store, e := NewWithKey(scoped, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	return store
}
func TestConcurrentPaidAndRefundReservation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	m := Merchant{ID: 910001, Name: "core-test", APIKey: "test-only"}

	if err := s.CreateMerchant(ctx, m); err != nil {
		t.Fatal(err)
	}
	in := CreateOrderInput{MerchantID: m.ID, OutTradeNo: "concurrent", Name: "item", Type: "alipay", Amount: 1000}
	o, e := s.CreateOrder(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.MarkPaid(ctx, o.TradeNo, "evt-test") }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	b, e := s.Balance(ctx, m.ID)
	if e != nil || b.Available != 1000 {
		t.Fatalf("balance %+v %v", b, e)
	}
	_, e = s.ReserveRefund(ctx, m.ID, in.OutTradeNo, "r1", 700)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReserveRefund(ctx, m.ID, in.OutTradeNo, "r2", 400); e == nil {
		t.Fatal("over-refund accepted")
	}
	r, e := s.ReserveRefund(ctx, m.ID, in.OutTradeNo, "r1", 700)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "unknown"); e != nil {
		t.Fatal(e)
	}
	b, _ = s.Balance(ctx, m.ID)
	if b.Available != 300 || b.Reserved != 700 {
		t.Fatalf("unknown released reservation: %+v", b)
	}
	if e = s.ResolveRefund(ctx, r.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	b, _ = s.Balance(ctx, m.ID)
	if b.Available != 1000 || b.Reserved != 0 {
		t.Fatalf("release %+v", b)
	}
}

func TestOrderIDAndIdempotency(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 1, Name: "test", APIKey: "secret-123"}); e != nil {
		t.Fatal(e)
	}
	in := CreateOrderInput{MerchantID: 1, OutTradeNo: "x", Name: "goods", Type: "alipay", Amount: 100}
	o, e := s.CreateOrder(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if len(o.TradeNo) != 19 {
		t.Fatalf("trade_no must be 19 decimal digits: %q", o.TradeNo)
	}
	for _, c := range o.TradeNo {
		if c < '0' || c > '9' {
			t.Fatalf("non-digit in trade no %q", o.TradeNo)
		}
	}
	again, e := s.CreateOrder(ctx, in)
	if e != nil || again.TradeNo != o.TradeNo {
		t.Fatalf("duplicate: %+v %v", again, e)
	}
	in.Amount = 101
	if _, e = s.CreateOrder(ctx, in); !errors.Is(e, ErrConflict) {
		t.Fatalf("wanted conflict, got %v", e)
	}
}
func TestFinancialDatabaseInvariants(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "INSERT INTO journals(event_key,description) VALUES('bad','empty journal')"); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e == nil {
		t.Fatal("unbalanced journal committed")
	}
	if e = s.CreateMerchant(ctx, Merchant{ID: 2, Name: "test", APIKey: "secret-123"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 2, OutTradeNo: "x", Amount: 100, Type: "alipay"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "e1"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE ledger_entries SET amount=1"); e == nil {
		t.Fatal("ledger mutable")
	}
	if _, e = s.pool.Exec(ctx, "DELETE FROM journals"); e == nil {
		t.Fatal("journals mutable")
	}
	var encrypted []byte
	if e = s.pool.QueryRow(ctx, "SELECT api_key FROM merchants WHERE id=2").Scan(&encrypted); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(encrypted, []byte("secret-123")) {
		t.Fatal("plaintext API key")
	}
	m, e := s.Merchant(ctx, 2)
	if e != nil || m.APIKey != "secret-123" {
		t.Fatalf("credential roundtrip: %v", e)
	}
}
func TestConcurrentReservationNoOverspend(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 3, Name: "test", APIKey: "secret-123"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 3, OutTradeNo: "x", Amount: 1000, Type: "alipay"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "e2"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, e := s.ReserveTransfer(ctx, 3, fmt.Sprint(n), 100)
			if e == nil {
				mu.Lock()
				success++
				mu.Unlock()
			} else if !errors.Is(e, ErrInsufficient) {
				t.Errorf("reserve: %v", e)
			}
		}(i)
	}
	wg.Wait()
	if success != 10 {
		t.Fatalf("successful reservations %d", success)
	}
	b, e := s.Balance(ctx, 3)
	if e != nil || b.Available != 0 || b.Reserved != 1000 {
		t.Fatalf("balance %+v %v", b, e)
	}
}
func TestRejectHistoricalAppendAndBalanceTampering(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 4, Name: "test", APIKey: "secret-123"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 4, OutTradeNo: "x", Amount: 100, Type: "alipay"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "e4"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE accounts SET balance=balance+1 WHERE owner_id=4 AND kind='available'"); e == nil {
		t.Fatal("unbacked balance update accepted")
	}
	if _, e = s.pool.Exec(ctx, "INSERT INTO ledger_entries(journal_id,account_id,amount) SELECT journal_id,account_id,amount FROM ledger_entries LIMIT 1"); e == nil {
		t.Fatal("historical journal append accepted")
	}
}
func TestOrderProtocolProfileStored(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 5, Name: "rsa", APIKey: "secret-123", SignatureAlgorithm: "RSA", RSAPublicKey: "test-key"}); e != nil {
		t.Fatal(e)
	}
	m, e := s.Merchant(ctx, 5)
	if e != nil || m.SignatureAlgorithm != "RSA" {
		t.Fatalf("algorithm persisted %+v %v", m, e)
	}
	in := CreateOrderInput{MerchantID: 5, OutTradeNo: "v2", Amount: 100, Version: "v2"}
	o, e := s.CreateOrder(ctx, in)
	if e != nil || o.Version != "v2" {
		t.Fatalf("version persisted %+v %v", o, e)
	}
	in.Version = "v1"
	if _, e = s.CreateOrder(ctx, in); !errors.Is(e, ErrConflict) {
		t.Fatalf("protocol replay should conflict %v", e)
	}
}
func TestRefundAndTransferResultAudit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 7, Name: "audit", APIKey: "secret-123"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 7, OutTradeNo: "x", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "audit"); e != nil {
		t.Fatal(e)
	}
	r, e := s.ReserveRefund(ctx, 7, "x", "r", 100)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "unknown"); e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "failed"); !errors.Is(e, ErrState) {
		t.Fatalf("terminal mutation %v", e)
	}
	tr, e := s.ReserveTransfer(ctx, 7, "tr", 200)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveTransfer(ctx, tr.ID, "unknown"); e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveTransfer(ctx, tr.ID, "failed"); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"refund", "transfer"} {
		var count int
		if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM operation_audit WHERE operation_kind=$1", kind).Scan(&count); e != nil {
			t.Fatal(e)
		}
		if count != 3 {
			t.Fatalf("%s audit count %d", kind, count)
		}
	}
	if _, e = s.pool.Exec(ctx, "DELETE FROM operation_audit"); e == nil {
		t.Fatal("audit mutable")
	}
	b, e := s.Balance(ctx, 7)
	if e != nil || b.Available != 900 || b.Reserved != 0 {
		t.Fatalf("terminal balances %+v %v", b, e)
	}
}
func TestConcurrentRefundReservationsBoundByOrder(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 8, Name: "concurrent refund", APIKey: "secret-123"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 8, OutTradeNo: "x", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "refund-race"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var accepted []Refund
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r, e := s.ReserveRefund(ctx, 8, "x", fmt.Sprint(n), 100)
			if e == nil {
				mu.Lock()
				accepted = append(accepted, r)
				mu.Unlock()
			} else if !errors.Is(e, ErrInvalid) {
				t.Errorf("refund reserve %v", e)
			}
		}(i)
	}
	wg.Wait()
	if len(accepted) != 10 {
		t.Fatalf("accepted %d refunds", len(accepted))
	}
	for _, r := range accepted {
		wg.Add(1)
		go func(r Refund) {
			defer wg.Done()
			if e := s.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
				t.Errorf("refund resolution %v", e)
			}
		}(r)
	}
	wg.Wait()
	b, e := s.Balance(ctx, 8)
	if e != nil || b.Available != 0 || b.Reserved != 0 {
		t.Fatalf("refund final %+v %v", b, e)
	}
}
