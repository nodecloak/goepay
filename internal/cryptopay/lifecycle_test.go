package cryptopay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testService(t *testing.T) *Service {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	p, e := pgxpool.New(t.Context(), dsn)
	if e != nil {
		t.Fatal(e)
	}
	b := make([]byte, 8)
	rand.Read(b)
	schema := "crypto_" + hex.EncodeToString(b)
	if _, e = p.Exec(t.Context(), "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg, _ := pgxpool.ParseConfig(dsn)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	q, e := pgxpool.NewWithConfig(t.Context(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		q.Close()
		_, e := p.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		p.Close()
		if e != nil {
			t.Error(e)
		}
	})
	_, e = q.Exec(t.Context(), `CREATE TABLE merchants(id bigint PRIMARY KEY,enabled boolean NOT NULL); INSERT INTO merchants VALUES(1,true),(2,true); CREATE TABLE accounts(owner_id bigint,kind text,balance bigint); INSERT INTO accounts VALUES(1,'available',12345);CREATE TABLE orders(merchant_id bigint,out_trade_no text)`)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "000199_crypto_custody.up.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = q.Exec(t.Context(), string(raw)); e != nil {
		t.Fatal(e)
	}
	raw, e = os.ReadFile(filepath.Join("..", "..", "db", "migrations", "000200_crypto_payment_channels.up.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = q.Exec(t.Context(), string(raw)); e != nil {
		t.Fatal(e)
	}
	return &Service{Pool: q, TokenKey: make([]byte, 32), Networks: map[string]NetworkConfig{"tron": {Enabled: true, FeeBPS: 100}, "ethereum": {Enabled: true, FeeBPS: 100}}}
}

const depositOne = "TMVQGm1qAQYVdetCeGRRkTWYYrLXuHK2HC"
const depositTwo = "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE"

func invoice(t *testing.T, s *Service, order string) Invoice {
	t.Helper()
	if e := s.AddAddress(t.Context(), "tron", depositOne, "deposit"); e != nil {
		t.Fatal(e)
	}
	v, e := s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: 1, OutTradeNo: order, Network: "tron", Amount: "10", ExpiresIn: time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestFinalDepositsCreditOnceAndNeverTouchCNY(t *testing.T) {
	s := testService(t)
	v := invoice(t, s, "paid")
	ev := Deposit{Network: "tron", TxID: "tx-a", EventIndex: 0, BlockHeight: 8, BlockHash: "block-a", Address: v.Address, Amount: "10", OccurredAt: time.Now().Add(2 * time.Second)}
	if e := s.RecordDeposit(t.Context(), ev); e != nil {
		t.Fatal(e)
	}
	if e := s.RecordDeposit(t.Context(), ev); e != nil {
		t.Fatal(e)
	}
	got, e := s.GetInvoice(t.Context(), 1, v.ID)
	if e != nil || got.State != "paid" {
		t.Fatalf("%+v %v", got, e)
	}
	balances, e := s.Balances(t.Context(), 1)
	if e != nil || len(balances) != 1 || balances[0].Available != "9.9" {
		t.Fatalf("%+v %v", balances, e)
	}
	var cny int64
	s.Pool.QueryRow(t.Context(), "SELECT balance FROM accounts WHERE owner_id=1").Scan(&cny)
	if cny != 12345 {
		t.Fatal(cny)
	}
	report, e := s.Reconcile(t.Context())
	if e != nil || report.ImbalancedJournals != 0 || report.BalanceMismatches != 0 {
		t.Fatalf("%+v %v", report, e)
	}
	ev.Amount = "11"
	if e = s.RecordDeposit(t.Context(), ev); e == nil {
		t.Fatal("event identity changed amount")
	}
	if _, e = s.GetInvoice(t.Context(), 2, v.ID); e == nil {
		t.Fatal("merchant isolation")
	}
}
func TestConcurrentWithdrawalsCannotOverspend(t *testing.T) {
	s := testService(t)
	v := invoice(t, s, "withdraw")
	if e := s.RecordDeposit(t.Context(), Deposit{Network: "tron", TxID: "fund", BlockHeight: 1, BlockHash: "b", Address: v.Address, Amount: "10", OccurredAt: time.Now().Add(2 * time.Second)}); e != nil {
		t.Fatal(e)
	}
	seedRecipient(t, s, depositTwo)
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for _, id := range []string{"one", "two"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, e := s.RequestWithdrawal(context.Background(), WithdrawalInput{MerchantID: 1, RequestID: id, Network: "tron", Address: depositTwo, Amount: "6"})
			if e == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}(id)
	}
	wg.Wait()
	if success != 1 {
		t.Fatal(success)
	}
	b, _ := s.Balances(t.Context(), 1)
	if b[0].Available != "3.9" || b[0].Reserved != "6" {
		t.Fatalf("%+v", b)
	}
}
func TestUnderpaymentExcessAndLateFundsAreNotLost(t *testing.T) {
	s := testService(t)
	v := invoice(t, s, "partial")
	for i, amount := range []string{"4", "6", "1"} {
		if e := s.RecordDeposit(t.Context(), Deposit{Network: "tron", TxID: []string{"part1", "part2", "excess"}[i], BlockHeight: int64(i + 1), BlockHash: "b", Address: v.Address, Amount: amount, OccurredAt: time.Now().Add(2 * time.Second)}); e != nil {
			t.Fatal(e)
		}
	}
	got, _ := s.GetInvoice(t.Context(), 1, v.ID)
	if got.State != "overpaid" || got.Received != "11" {
		t.Fatalf("%+v", got)
	}
	b, _ := s.Balances(t.Context(), 1)
	if b[0].Available != "9.9" {
		t.Fatalf("excess automatically spendable %+v", b)
	}
}

func seedRecipient(t *testing.T, s *Service, address string) {
	t.Helper()
	_, e := s.Pool.Exec(t.Context(), "INSERT INTO crypto_recipients(id,merchant_id,network,address,state,usable_at) VALUES($1,1,'tron',$2,'approved',clock_timestamp()-interval '1 second') ON CONFLICT DO NOTHING", randomID(), address)
	if e != nil {
		t.Fatal(e)
	}
}
func TestCNYAndUSDTOrdersShareMerchantOrderNamespace(t *testing.T) {
	s := testService(t)
	_ = invoice(t, s, "same-order")
	if _, e := s.Pool.Exec(t.Context(), "INSERT INTO orders(merchant_id,out_trade_no) VALUES(1,'same-order')"); e == nil {
		t.Fatal("CNY order shadows USDT order")
	}
}
func TestCollectionDisabledMerchantCannotConsumeDepositAddress(t *testing.T) {
	s := testService(t)
	if _, e := s.Pool.Exec(t.Context(), "ALTER TABLE merchants ADD COLUMN pay integer NOT NULL DEFAULT 1;UPDATE merchants SET pay=0 WHERE id=1"); e != nil {
		t.Fatal(e)
	}
	if e := s.AddAddress(t.Context(), "tron", depositOne, "deposit"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: 1, OutTradeNo: "blocked", Network: "tron", Amount: "10"}); e == nil {
		t.Fatal("disabled merchant collected USDT")
	}
	var used bool
	s.Pool.QueryRow(t.Context(), "SELECT used FROM crypto_addresses WHERE network='tron' AND address=$1", depositOne).Scan(&used)
	if used {
		t.Fatal("disabled request consumed address")
	}
}
