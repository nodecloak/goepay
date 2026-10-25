package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestFundsActionGuardCoversLocalReviewsAndIncome(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	denied := errors.New("authorization required")
	guarded := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { return denied })
	request, err := s.RequestFunds(ctx, "settlements", 681, 0, "review", 100, "merchant:681")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReviewFundsRequest(guarded, request.ID, false, "admin:1"); !errors.Is(err, denied) {
		t.Fatal("review bypass", err)
	}
	if _, err = s.Deposit(guarded, 681, "hold", "denied-deposit", 10, "admin:1"); !errors.Is(err, denied) {
		t.Fatal("deposit bypass", err)
	}
	if _, err = s.Deposit(ctx, 681, "hold", "hold", 100, "merchant:681"); err != nil {
		t.Fatal(err)
	}
	d, err := s.Deposit(ctx, 681, "release", "release", 20, "merchant:681")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReviewDeposit(guarded, d.ID, true, "admin:1"); !errors.Is(err, denied) {
		t.Fatal("deposit review bypass", err)
	}
	var group int64
	if err = s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"paid","price_cents":"10","duration_days":"1","enabled":true}') RETURNING id`).Scan(&group); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PurchaseMembership(guarded, 681, group, "denied-membership", "admin:1"); !errors.Is(err, denied) {
		t.Fatal("membership bypass", err)
	}
	o, err := s.Order(ctx, 681, "minimum-funding")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.FreezeOrderIncome(guarded, 681, o.TradeNo, "denied-freeze", incomeTestActor); !errors.Is(err, denied) {
		t.Fatal("income freeze bypass", err)
	}
	// The separate paid order is fully spendable and can be held in isolation.
	funded, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 681, OutTradeNo: "income", Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkPaid(ctx, funded.TradeNo, "income"); err != nil {
		t.Fatal(err)
	}
	h, err := s.FreezeOrderIncome(ctx, 681, funded.TradeNo, "income", incomeTestActor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UnfreezeOrderIncome(guarded, 681, funded.TradeNo, h.ID, incomeTestActor); !errors.Is(err, denied) {
		t.Fatal("income unfreeze bypass", err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM deposit_actions WHERE external_no='denied-deposit'").Scan(&count); err != nil || count != 0 {
		t.Fatal("denied deposit left intent", count, err)
	}
}

func TestFundsActionGuardCoversAuthorizationAndClose(t *testing.T) {
	for _, kind := range []string{"capture", "release", "close"} {
		t.Run(kind, func(t *testing.T) {
			s, o := releaseTestOrder(t)
			ctx := context.Background()
			denied := errors.New("authorization required")
			guarded := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { return denied })
			var err error
			switch kind {
			case "capture":
				_, _, err = s.BeginPreauthorizationCapture(guarded, o.TradeNo)
			case "release":
				_, _, err = s.BeginAuthorizationRelease(guarded, o.MerchantID, o.TradeNo)
			case "close":
				a, e := s.ProviderAttempt(ctx, o.TradeNo)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = s.pool.Exec(ctx, "UPDATE provider_attempts SET state='pending' WHERE id=$1", a.ID); e != nil {
					t.Fatal(e)
				}
				a.State = "pending"
				_, _, _, err = s.beginProviderOperation(guarded, "close", o.TradeNo, o, a, 0)
			}
			if !errors.Is(err, denied) {
				t.Fatal(fmt.Sprintf("%s bypassed funds guard", kind), err)
			}
		})
	}
}

func TestFundsActionGuardDeniesBeforeMoneyAndIntent(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	denied := errors.New("funds authorization required")
	guarded := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { return denied })
	if _, err := s.ReserveRefund(guarded, 681, "minimum-funding", "denied-refund", 10); !errors.Is(err, denied) {
		t.Fatal("refund bypassed guard", err)
	}
	if _, err := s.ReserveTransfer(guarded, 681, "denied-transfer", 10); !errors.Is(err, denied) {
		t.Fatal("transfer bypassed guard", err)
	}
	b, err := s.Balance(ctx, 681)
	if err != nil || b.Available != 10000 || b.Reserved != 0 {
		t.Fatal("denied authorization changed money", b, err)
	}
	var n int
	if err = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM refunds)+(SELECT count(*) FROM transfers)").Scan(&n); err != nil || n != 0 {
		t.Fatal("denied authorization left intent", n, err)
	}
}

func TestFundsActionGuardCommitsWithIntentAndSkipsSameIdempotentIntent(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, "CREATE TABLE test_funds_guards(kind text,business_key text)"); err != nil {
		t.Fatal(err)
	}
	guarded := WithFundsActionGuard(ctx, func(ctx context.Context, tx pgx.Tx, kind, key string) error {
		_, err := tx.Exec(ctx, "INSERT INTO test_funds_guards VALUES($1,$2)", kind, key)
		return err
	})
	r, err := s.ReserveRefund(guarded, 681, "minimum-funding", "once", 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReserveRefund(guarded, 681, "minimum-funding", "once", 20); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReserveTransfer(guarded, 681, "once-transfer", 30); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReserveTransfer(guarded, 681, "once-transfer", 30); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM test_funds_guards").Scan(&n); err != nil || n != 2 {
		t.Fatal("authorization not one per durable intent", n, err, r.ID)
	}
	if _, err = s.pool.Exec(ctx, `CREATE FUNCTION fail_refund_insert_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'business failure'; END $$; CREATE TRIGGER fail_refund_insert_test BEFORE INSERT ON refunds FOR EACH ROW EXECUTE FUNCTION fail_refund_insert_test()`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReserveRefund(guarded, 681, "minimum-funding", "rollback", 20); err == nil {
		t.Fatal("injected intent failure ignored")
	}
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM test_funds_guards").Scan(&n); err != nil || n != 2 {
		t.Fatal("business rollback consumed authorization", n, err)
	}
}
