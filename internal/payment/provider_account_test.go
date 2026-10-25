package payment

import (
	"context"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type accountOperationsProvider struct {
	operationsProvider
	account                           string
	reads, refundReads, notifications atomic.Int32
}

func TestProviderAccountProofFailureRollsBackBeforeCreate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 1, Name: "test", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1, OutTradeNo: "atomic-account", Name: "test", Type: "wxpay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	p := &accountOperationsProvider{account: "original"}
	g := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://local.example.invalid"}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION reject_account_proof_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected account proof failure';END $$;CREATE TRIGGER reject_account_proof_test BEFORE INSERT ON provider_attempt_accounts FOR EACH ROW EXECUTE FUNCTION reject_account_proof_test()`); e != nil {
		t.Fatal(e)
	}
	if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.1", ""); e == nil {
		t.Fatal("missing account proof still sent create")
	}
	var attempts, jobs int
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM provider_attempts),(SELECT count(*) FROM jobs WHERE kind='payment.reconcile')").Scan(&attempts, &jobs); e != nil || attempts != 0 || jobs != 0 || p.calls.Load() != 0 {
		t.Fatal(attempts, jobs, e, p.calls.Load())
	}
	current, e := s.OrderByTradeNo(ctx, o.TradeNo)
	if e != nil || current.Status != "created" {
		t.Fatal(current, e)
	}
	if _, e = s.pool.Exec(ctx, "DROP TRIGGER reject_account_proof_test ON provider_attempt_accounts"); e != nil {
		t.Fatal(e)
	}
	if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.1", ""); e != nil || p.calls.Load() != 1 {
		t.Fatal("rolled-back account proof consumed send", e, p.calls.Load())
	}
}

func TestHistoricalRealProviderAttemptCannotInferCurrentReceivingAccount(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 1, Name: "test", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1, OutTradeNo: "historical-account", Name: "test", Type: "wxpay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	a, _, e := s.BeginProviderAttemptWithIdentity(ctx, o.TradeNo, 7, "native", "old-app", "")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RecordProviderAttemptResult(ctx, a.ID, "pending", "qrcode", "weixin://historical-local", ""); e != nil {
		t.Fatal(e)
	}
	var requests atomic.Int32
	up := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
		t.Error("historical account inferred from configuration")
	}))
	defer up.Close()
	p, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "old-app", MerchantID: "current-mch", APIKey: "0123456789abcdef0123456789abcdef", BaseURL: up.URL, HTTPClient: up.Client()})
	if e != nil {
		t.Fatal(e)
	}
	g := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://local.example.invalid"}
	if _, e = g.Query(ctx, 1, o.TradeNo); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.1", ""); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if e = g.HandleNotification(ctx, 7, o.TradeNo, nil, []byte("no historical proof")); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	var proofs int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM provider_attempt_accounts").Scan(&proofs); e != nil || proofs != 0 || requests.Load() != 0 {
		t.Fatal(proofs, e, requests.Load())
	}
}

type accountScanFlowProvider struct {
	scanFlowProvider
	account string
}

func (*accountScanFlowProvider) ApplicationID() string             { return "same-scan-app" }
func (p *accountScanFlowProvider) TransferAccountIdentity() string { return p.account }

type accountPreauthFlowProvider struct {
	preauthFlowProvider
	account string
}

func (*accountPreauthFlowProvider) ApplicationID() string             { return "same-preauth-app" }
func (p *accountPreauthFlowProvider) TransferAccountIdentity() string { return p.account }

func TestProviderAccountScanAndAuthorizationBindBeforeFirstSend(t *testing.T) {
	for _, kind := range []string{"scan", "preauth"} {
		t.Run(kind, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			typ := "wxpay"
			if kind == "preauth" {
				typ = "alipay"
			}
			if e := s.CreateMerchant(ctx, Merchant{ID: 1, Name: "test", APIKey: "local-only"}); e != nil {
				t.Fatal(e)
			}
			o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1, OutTradeNo: kind, Name: "test", Type: typ, Amount: 100})
			if e != nil {
				t.Fatal(e)
			}
			g := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{}, AllowLive: true, CallbackBaseURL: "https://local.example.invalid"}
			if kind == "scan" {
				original := &accountScanFlowProvider{account: "original"}
				original.lost = true
				g.Providers[7] = original
				if _, e = g.SubmitScan(ctx, o.TradeNo, 7, "192.0.2.1", "120000000000000001"); e == nil {
					t.Fatal("fixture must remain unknown")
				}
				changed := &accountScanFlowProvider{account: "changed"}
				g.Providers[7] = changed
				if _, e = g.SubmitScan(ctx, o.TradeNo, 7, "192.0.2.1", "120000000000000001"); !errors.Is(e, ErrConflict) || changed.scans.Load() != 0 {
					t.Fatal(e)
				}
			} else {
				original := &accountPreauthFlowProvider{account: "original"}
				g.Providers[7] = original
				if _, e = g.SubmitPreauthorization(ctx, o.TradeNo, 7, "192.0.2.1", ""); e != nil {
					t.Fatal(e)
				}
				changed := &accountPreauthFlowProvider{account: "changed"}
				changed.queryAuthorized = true
				g.Providers[7] = changed
				if _, e = g.SubmitPreauthorization(ctx, o.TradeNo, 7, "192.0.2.1", ""); !errors.Is(e, ErrConflict) {
					t.Fatal(e)
				}
				if e = g.RefreshPreauthorization(ctx, o.TradeNo, 7); !errors.Is(e, ErrConflict) {
					t.Fatal(e)
				}
				if e = g.HandlePreauthorizationNotification(ctx, 7, o.TradeNo, nil, []byte("valid-authorization")); !errors.Is(e, ErrConflict) || changed.captures != 0 {
					t.Fatal(e)
				}
			}
			var proofs int
			if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM provider_attempt_accounts").Scan(&proofs); e != nil || proofs != 1 {
				t.Fatal(proofs, e)
			}
		})
	}
}

func (*accountOperationsProvider) ApplicationID() string             { return "same-payment-app" }
func (p *accountOperationsProvider) TransferAccountIdentity() string { return p.account }
func (p *accountOperationsProvider) Query(ctx context.Context, r adapter.QueryRequest) (adapter.QueryResult, error) {
	p.reads.Add(1)
	return p.operationsProvider.Query(ctx, r)
}
func (p *accountOperationsProvider) QueryRefund(ctx context.Context, r adapter.RefundRequest) (adapter.RefundResult, error) {
	p.refundReads.Add(1)
	return p.operationsProvider.QueryRefund(ctx, r)
}
func (p *accountOperationsProvider) VerifyNotification(ctx context.Context, h http.Header, raw []byte, e adapter.ExpectedPayment) (adapter.PaymentEvent, error) {
	p.notifications.Add(1)
	return p.operationsProvider.VerifyNotification(ctx, h, raw, e)
}

func TestProviderAccountSwitchCannotInheritOriginalPaymentIntent(t *testing.T) {
	for _, action := range []string{"submit", "query", "notification", "close", "refund", "refund-query"} {
		t.Run(action, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			if e := s.CreateMerchant(ctx, Merchant{ID: 1, Name: "test", APIKey: "local-account-test"}); e != nil {
				t.Fatal(e)
			}
			o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1, OutTradeNo: "account-intent", Name: "local", Type: "wxpay", Amount: 100})
			if e != nil {
				t.Fatal(e)
			}
			original := &accountOperationsProvider{account: "original-mch"}
			g := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: original}, AllowLive: true, CallbackBaseURL: "https://local.example.invalid"}
			if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.1", ""); e != nil {
				t.Fatal(e)
			}
			if action == "refund" || action == "refund-query" {
				if _, e = g.Query(ctx, 1, o.TradeNo); e != nil {
					t.Fatal(e)
				}
			}
			if action == "refund-query" {
				original.unknown = true
				if _, e = g.Refund(ctx, 1, o.TradeNo, "original-refund", 60); e == nil {
					t.Fatal("fixture did not reserve unknown refund")
				}
			}
			changed := &accountOperationsProvider{account: "reconfigured-mch"}
			g.Providers[7] = changed
			switch action {
			case "submit":
				_, e = g.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.1", "")
			case "query":
				_, e = g.Query(ctx, 1, o.TradeNo)
			case "notification":
				e = g.HandleNotification(ctx, 7, o.TradeNo, nil, []byte("valid-signed-test-event"))
			case "close":
				e = g.Close(ctx, 1, o.TradeNo)
			case "refund":
				_, e = g.Refund(ctx, 1, o.TradeNo, "changed-refund", 60)
			case "refund-query":
				_, e = g.QueryRefund(ctx, 1, "original-refund")
			}
			if !errors.Is(e, ErrConflict) {
				t.Fatal("new merchant inherited old payment", action, e)
			}
			if changed.calls.Load()+changed.reads.Load()+changed.refunds.Load()+changed.closes.Load()+changed.refundReads.Load()+changed.notifications.Load() != 0 {
				t.Fatal("changed merchant called provider")
			}
			bal, e := s.Balance(ctx, 1)
			if e != nil {
				t.Fatal(e)
			}
			wantAvailable, wantReserved := int64(0), int64(0)
			if action == "refund" {
				wantAvailable = 100
			}
			if action == "refund-query" {
				wantAvailable = 40
				wantReserved = 60
			}
			if bal.Available != wantAvailable || bal.Reserved != wantReserved {
				t.Fatal("wrong account mutated original funds", bal)
			}
			g.Providers[7] = original
			if action == "refund-query" {
				if r, e := g.QueryRefund(ctx, 1, "original-refund"); e != nil || r.Status != "succeeded" {
					t.Fatal("original refund not recoverable", r, e)
				}
			}
		})
	}
}
