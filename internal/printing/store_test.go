package printing

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

var printAdmin = Actor{Role: "admin", Class: "super", ID: 1, Name: "local-print-super"}
var printOwner = Actor{Role: "merchant", ID: 901, Name: "local-print-owner"}

func printFixture(t *testing.T) (*Store, *payment.Store) {
	t.Helper()
	dsn := os.Getenv("GOEPAY_PRINTING_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("GOEPAY_TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("GOEPAY_PRINTING_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	nonce := make([]byte, 8)
	if _, e = rand.Read(nonce); e != nil {
		t.Fatal(e)
	}
	schema := "printing_" + hex.EncodeToString(nonce)
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	p, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		p.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	if e = db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	pay, e := payment.NewWithKey(p, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	s, e := New(p, pay)
	if e != nil {
		t.Fatal(e)
	}
	for _, mid := range []int64{901, 902} {
		if e = pay.CreateMerchant(ctx, payment.Merchant{ID: mid, Name: "Local Print Merchant", APIKey: "synthetic-print-merchant-key"}); e != nil {
			t.Fatal(e)
		}
	}
	return s, pay
}
func configurePrinting(t *testing.T, s *Store, mode int) {
	t.Helper()
	ctx := context.Background()
	if _, e := s.SetPlatform(ctx, PlatformInput{Provider: "local_fake", Enabled: true, CredentialAction: "replace", AppID: "LOCAL-APP-ID", Secret: "LOCAL-SYNTHETIC-PRINT-SECRET"}, printAdmin); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SetMerchant(ctx, 901, MerchantInput{Mode: mode, Copies: 2, DeviceAction: "replace", DeviceID: "LOCAL-DEVICE-123456"}, printOwner); e != nil {
		t.Fatal(e)
	}
}

type printPaymentVerifier struct{ provider.Provider }

func (printPaymentVerifier) ID() string { return "wxpay" }
func (printPaymentVerifier) Capabilities() provider.Capabilities {
	return provider.Capabilities{Methods: []string{"native"}}
}
func (printPaymentVerifier) VerifyNotification(_ context.Context, h http.Header, b []byte, e provider.ExpectedPayment) (provider.PaymentEvent, error) {
	m := hmac.New(sha256.New, []byte("local-only-payment-signer"))
	m.Write(b)
	if !hmac.Equal([]byte(h.Get("Local-Signature")), []byte(hex.EncodeToString(m.Sum(nil)))) || string(b) != e.TradeNo+":123" {
		return provider.PaymentEvent{}, provider.ErrAuthentication
	}
	return provider.PaymentEvent{TradeNo: e.TradeNo, Amount: e.Amount, Currency: "CNY", State: "SUCCESS", EventID: "LOCAL-" + e.TradeNo, ProviderTradeNo: "LOCAL_PROVIDER_" + e.TradeNo, BuyerID: "PRIVATE-BUYER-MUST-NOT-BE-IN-RECEIPT"}, nil
}
func printPaidOrder(t *testing.T, pay *payment.Store, no string) (payment.Order, payment.ProviderFlow, http.Header, []byte) {
	t.Helper()
	ctx := context.Background()
	o, e := pay.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 901, OutTradeNo: no, Name: "local item", Amount: 123, Type: "wxpay"})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = pay.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); e != nil {
		t.Fatal(e)
	}
	flow := payment.ProviderFlow{Store: pay, Providers: map[int64]provider.Provider{7: printPaymentVerifier{}}}
	b := []byte(o.TradeNo + ":123")
	m := hmac.New(sha256.New, []byte("local-only-payment-signer"))
	m.Write(b)
	h := http.Header{"Local-Signature": []string{hex.EncodeToString(m.Sum(nil))}}
	if e = flow.HandleNotification(ctx, 7, o.TradeNo, h, b); e != nil {
		t.Fatal(e)
	}
	return o, flow, h, b
}
func TestPrintingConfigEncryptionOwnerCASAndAuditRollback(t *testing.T) {
	s, _ := printFixture(t)
	configurePrinting(t, s, 2)
	ctx := context.Background()
	view, e := s.Platform(ctx, printAdmin)
	if e != nil || !view.SecretPresent || strings.Contains(view.AppIDMasked, "LOCAL-APP-ID") {
		t.Fatal(view, e)
	}
	if _, e = s.Merchant(ctx, 901, Actor{Role: "merchant", ID: 902, Name: "other"}); !errors.Is(e, ErrInvalid) {
		t.Fatal("cross owner", e)
	}
	if _, e = s.SetPlatform(ctx, PlatformInput{ExpectedRevision: 1, Provider: "local_fake", CredentialAction: "keep"}, Actor{Role: "admin", Class: "operator", ID: 1, Name: "op"}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if _, e = s.SetMerchant(ctx, 901, MerchantInput{ExpectedRevision: 0, Mode: 0, Copies: 1, DeviceAction: "clear"}, printOwner); !errors.Is(e, ErrConflict) {
		t.Fatal("stale optin", e)
	}
	var cipher []byte
	if e = s.pool.QueryRow(ctx, "SELECT credential_cipher FROM printing_platform_configs LIMIT 1").Scan(&cipher); e != nil || strings.Contains(string(cipher), "SYNTHETIC") {
		t.Fatal("plaintext credential stored", e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION local_print_audit_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='printing-merchant' THEN RAISE EXCEPTION 'local config audit failed'; END IF; RETURN NEW; END $$;CREATE TRIGGER local_print_audit_fail BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION local_print_audit_fail()`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetMerchant(ctx, 901, MerchantInput{ExpectedRevision: 1, Mode: 0, Copies: 1, DeviceAction: "clear"}, printOwner); e == nil {
		t.Fatal("audit failure ignored")
	}
	got, e := s.Merchant(ctx, 901, printOwner)
	if e != nil || got.Revision != 1 || got.Mode != 2 {
		t.Fatal(got, e)
	}
}

type countedPrintConnector struct {
	LocalFake
	calls atomic.Int32
}

func (c *countedPrintConnector) Send(ctx context.Context, p PrintCommand) (Outcome, error) {
	c.calls.Add(1)
	return c.LocalFake.Send(ctx, p)
}
func TestPrintingVerifiedPaidAutoDedupUnknownNeverResendsAndExplicitReprint(t *testing.T) {
	s, pay := printFixture(t)
	configurePrinting(t, s, 2)
	ctx := context.Background()
	o, flow, h, b := printPaidOrder(t, pay, "paid-auto")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := flow.HandleNotification(ctx, 7, o.TradeNo, h, b); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	var jobID atomic.Int64
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, e := s.PrepareAutomatic(ctx, o.ID)
			if e != nil || j == nil {
				t.Error(j, e)
				return
			}
			jobID.Store(j.ID)
		}()
	}
	wg.Wait()
	id := jobID.Load()
	if id == 0 {
		t.Fatal("no auto intent")
	}
	connector := &countedPrintConnector{LocalFake: LocalFake{State: "unknown"}}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.Dispatch(ctx, id, connector); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	j, e := s.Job(ctx, 901, id, printOwner)
	if e != nil || j.State != "unknown" || connector.calls.Load() != 1 || j.Copies != 2 {
		t.Fatal(j, e, connector.calls.Load())
	}
	if _, e = s.Dispatch(ctx, id, connector); e != nil || connector.calls.Load() != 1 {
		t.Fatal("unknown auto resend", e, connector.calls.Load())
	}
	if _, e = s.CreateManual(ctx, 901, o.TradeNo, ManualInput{ExternalNo: "new-unconfirmed"}, printOwner); !errors.Is(e, ErrState) {
		t.Fatal("unknown bypassed by new number", e)
	}
	in := ManualInput{ExternalNo: "confirmed-local-reprint", PreviousJobID: id, AcknowledgeDuplicate: true}
	manual, e := s.CreateManual(ctx, 901, o.TradeNo, in, printOwner)
	if e != nil || manual.ID == id || manual.PreviousJobID != id {
		t.Fatal(manual, e)
	}
	again, e := s.CreateManual(ctx, 901, o.TradeNo, in, printOwner)
	if e != nil || again.ID != manual.ID {
		t.Fatal("same manual external allocated second intent", again, e)
	}
	r, e := s.Receipt(ctx, 901, o.TradeNo, printOwner)
	if e != nil || r.Amount != 123 || strings.Contains(r.Remark, "PRIVATE-BUYER") {
		t.Fatal(r, e)
	}
	if _, e = s.Job(ctx, 902, id, Actor{Role: "merchant", ID: 902, Name: "other"}); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross merchant job read", e)
	}
	var sources, autos, claims int
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM payment_print_sources),(SELECT count(*) FROM printing_intents WHERE source='auto'),(SELECT count(*) FROM printing_claims)").Scan(&sources, &autos, &claims); e != nil || sources != 1 || autos != 1 || claims != 1 {
		t.Fatal(sources, autos, claims, e)
	}
}
func TestPrintingPaymentSnapshotCannotBeRetrofittedAndConfigFailureCannotRollbackPaid(t *testing.T) {
	s, pay := printFixture(t)
	ctx := context.Background()
	o, _, _, _ := printPaidOrder(t, pay, "disabled-at-payment")
	configurePrinting(t, s, 2)
	if j, e := s.PrepareAutomatic(ctx, o.ID); e != nil || j != nil {
		t.Fatal("later config retroactively printed old order", j, e)
	}
	if _, e := s.pool.Exec(ctx, "ALTER TABLE printing_platform_head RENAME TO local_temporarily_unavailable_print_head"); e != nil {
		t.Fatal(e)
	}
	paid, _, _, _ := printPaidOrder(t, pay, "config-query-unavailable")
	if _, e := s.pool.Exec(ctx, "ALTER TABLE local_temporarily_unavailable_print_head RENAME TO printing_platform_head"); e != nil {
		t.Fatal(e)
	}
	got, e := pay.OrderByTradeNo(ctx, paid.TradeNo)
	if e != nil || got.Status != "paid" {
		t.Fatal("optional print config failure rolled back payment", got, e)
	}
	if j, e := s.PrepareAutomatic(ctx, paid.ID); e != nil || j != nil {
		t.Fatal("unavailable snapshot used current config", j, e)
	}
	if _, e = s.Receipt(ctx, 901, paid.TradeNo, printOwner); e != nil {
		t.Fatal("core verified payment fact was lost", e)
	}
}

func TestPrintingRunnerRequiresExplicitLocalSandboxAndConsumesDurableJobs(t *testing.T) {
	s, pay := printFixture(t)
	configurePrinting(t, s, 2)
	ctx := context.Background()
	o, _, _, _ := printPaidOrder(t, pay, "runner-paid")
	for _, flags := range [][2]bool{{false, false}, {false, true}, {true, false}} {
		if _, e := s.RunOne(ctx, flags[0], flags[1]); !errors.Is(e, ErrState) {
			t.Fatal("runner opened without explicit local sandbox", flags, e)
		}
	}
	var attempts int
	if e := s.pool.QueryRow(ctx, "SELECT COALESCE(sum(attempts),0) FROM jobs WHERE kind IN ('receipt-print.prepare','receipt-print.send')").Scan(&attempts); e != nil || attempts != 0 {
		t.Fatal("disabled runner consumed durable queue", attempts, e)
	}
	for i := 0; i < 2; i++ {
		ok, e := s.RunOne(ctx, true, true)
		if e != nil || !ok {
			t.Fatal(ok, e)
		}
	}
	list, e := s.OrderJobs(ctx, 901, o.TradeNo, 0, 20, printOwner)
	if e != nil || len(list) != 1 || list[0].State != "accepted" {
		t.Fatal("durable fake flow was not completed", list, e)
	}
	if ok, e := s.RunOne(ctx, true, true); e != nil || ok {
		t.Fatal("completed original job was requeued", ok, e)
	}
}
