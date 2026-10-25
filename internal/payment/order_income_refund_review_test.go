package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
)

// This independent fixture uses the public migrator and an isolated schema.
// It intentionally never loads application environment files or provider keys.
func incomeRefundReviewStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL is required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "host.docker.internal" {
		t.Fatal("review fixture requires an explicitly configured local PostgreSQL")
	}
	ctx := context.Background()
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var token [8]byte
	if _, err = rand.Read(token[:]); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	schema := "review_income_" + hex.EncodeToString(token[:])
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	scopedConfig := cfg.Copy()
	scopedConfig.ConnConfig.RuntimeParams["search_path"] = schema
	scopedConfig.MaxConns = 4
	scoped, err := pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		scoped.Close()
		if _, e := admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); e != nil {
			t.Error(e)
		}
		admin.Close()
	})
	if err = db.Migrate(ctx, scoped); err != nil {
		t.Fatalf("formal migration setup: %v", err)
	}
	store, err := NewWithKey(scoped, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CreateMerchant(ctx, Merchant{ID: 901, Name: "income review local", APIKey: "synthetic-review-key"}); err != nil {
		t.Fatal(err)
	}
	return store
}

func requireIncomeBirthVersionRejection(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("new birth accepted explicit legacy funding_evidence_version=1 without authenticated funding source")
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "P0001" || (!strings.Contains(pg.Message, "funding") && !strings.Contains(pg.Message, "version")) {
		t.Fatalf("unrelated failure did not establish version guard: %v", err)
	}
}

func TestReviewIncomeNewRefundCannotMasqueradeAsHistoricalFunding(t *testing.T) {
	s := incomeRefundReviewStore(t)
	ctx := context.Background()
	o := incomePaidOrder(t, s, 901, "birth-refund")
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var rid int64
	err = tx.QueryRow(ctx, `INSERT INTO refunds(order_id,merchant_id,refund_no,amount,merchant_amount,fee_amount,funding_evidence_version) VALUES($1,901,'review-legacy-birth',50,50,0,1) RETURNING id`, o.ID).Scan(&rid)
	if err != nil {
		requireIncomeBirthVersionRejection(t, err)
		return
	}
	if err = post(ctx, tx, fmt.Sprintf("refund:%d:reserve", rid), "local legacy-shaped reservation", 901, "available", 901, "reserved", 50); err != nil {
		t.Fatal(err)
	}
	requireIncomeBirthVersionRejection(t, tx.Commit(ctx))
}

func TestReviewIncomeNewHoldCannotMasqueradeAsHistoricalFunding(t *testing.T) {
	s := incomeRefundReviewStore(t)
	ctx := context.Background()
	o := incomePaidOrder(t, s, 901, "birth-hold")
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO accounts(owner_id,kind) VALUES(901,'order_income_hold') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	h, err := scanIncomeHold(tx.QueryRow(ctx, "WITH inserted AS (INSERT INTO order_income_holds(order_id,merchant_id,request_no,merchant_net,refund_merchant_amount,channel_sharing_amount,internal_allocated_amount,amount,funding_evidence_version) VALUES($1,901,'review-legacy-hold',100,0,0,0,100,1) RETURNING *) SELECT "+incomeHoldColumns+" FROM inserted h JOIN orders o ON o.id=h.order_id", o.ID))
	if err != nil {
		requireIncomeBirthVersionRejection(t, err)
		return
	}
	if err = post(ctx, tx, fmt.Sprintf("order-income:%d:freeze", h.ID), "local legacy-shaped hold", 901, "available", 901, "order_income_hold", 100); err != nil {
		t.Fatal(err)
	}
	if err = incomeHoldAudit(ctx, tx, h, "freeze", IncomeHoldActor{ID: 1, Name: "local-review-super", Class: "super"}); err != nil {
		t.Fatal(err)
	}
	requireIncomeBirthVersionRejection(t, tx.Commit(ctx))
}

func incomeReviewBalance(t *testing.T, s *Store, mid int64, kind string, scope, want int64) {
	t.Helper()
	var got int64
	if err := s.pool.QueryRow(context.Background(), `SELECT balance FROM accounts WHERE owner_id=$1 AND kind=$2 AND scope_id=$3`, mid, kind, scope).Scan(&got); err != nil || got != want {
		t.Fatalf("account (%d,%s,%d): got %d want %d: %v", mid, kind, scope, got, want, err)
	}
}

func incomeReviewHealthyCheck(t *testing.T, s *Store) {
	t.Helper()
	report, err := s.CheckLedger(context.Background())
	if err != nil || report.InvalidOrderIncomeFundings != 0 || report.InvalidRefundFundings != 0 || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
		t.Fatal("healthy original-income evidence rejected", report, err)
	}
}

func TestReviewIncomeRefundSourcesStayWithinOriginalHoldAndZeroHeldPendingBlocksRelease(t *testing.T) {
	s := incomeRefundReviewStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 902, 1000, "platform")
	a := incomePaidOrder(t, s, 902, "review-own-hold")
	b := incomePaidOrder(t, s, 902, "review-other-hold")
	ha, err := s.FreezeOrderIncome(ctx, 902, a.TradeNo, "freeze-a", incomeTestActor)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := s.FreezeOrderIncome(ctx, 902, b.TradeNo, "freeze-b", incomeTestActor)
	if err != nil {
		t.Fatal(err)
	}
	refund, err := s.ReserveRefund(ctx, 902, a.OutTradeNo, "consume-own", 90)
	if err != nil {
		t.Fatal(err)
	}
	incomeReviewBalance(t, s, 902, "order_income_reserve", ha.ID, 0)
	incomeReviewBalance(t, s, 902, "order_income_reserve", hb.ID, 90)
	incomeReviewBalance(t, s, 902, "order_income_refund_reserve", refund.ID, 90)
	if err = s.ResolveRefund(ctx, refund.ID, "succeeded"); err != nil {
		t.Fatal(err)
	}
	feeOnly, err := s.ReserveRefund(ctx, 902, a.OutTradeNo, "pending-fee-only", 10)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.RefundFundingPreview(ctx, s.pool, a.ID, feeOnly.RefundNo, 10)
	if err != nil || p.HoldID != ha.ID || p.HeldAmount != 0 || p.AvailableAmount != 0 || p.FeeAmount != 10 {
		t.Fatal("fee-only funding", p, err)
	}
	for _, state := range []string{"pending", "unknown"} {
		if state == "unknown" {
			if err = s.ResolveRefund(ctx, feeOnly.ID, state); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = s.UnfreezeOrderIncome(ctx, 902, a.TradeNo, ha.ID, incomeTestActor); !errors.Is(err, ErrState) {
			t.Fatalf("%s H=0 must still block release with domain state error: %v", state, err)
		}
		raw, snapshotErr := s.IncomeHoldReleaseSnapshot(ctx, s.pool, 902, a.TradeNo, ha.ID)
		var snapshot struct {
			Pending string `json:"pending_refunds"`
		}
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		if snapshotErr = json.Unmarshal(raw, &snapshot); snapshotErr != nil || snapshot.Pending != "1" {
			t.Fatalf("%s H=0 release preview must bind the pending refund: %s %v", state, raw, snapshotErr)
		}
	}
	if err = s.ResolveRefund(ctx, feeOnly.ID, "failed"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UnfreezeOrderIncome(ctx, 902, a.TradeNo, ha.ID, incomeTestActor); err != nil {
		t.Fatal(err)
	}
	incomeReviewBalance(t, s, 902, "available", 0, 0)
	incomeReviewBalance(t, s, 902, "order_income_reserve", ha.ID, 0)
	incomeReviewBalance(t, s, 902, "order_income_reserve", hb.ID, 90)
	var releaseAmount, consumed, journals int64
	if err = s.pool.QueryRow(ctx, `SELECT amount,consumed_amount,(SELECT count(*) FROM journals WHERE event_key=$2) FROM order_income_hold_releases WHERE hold_id=$1`, ha.ID, fmt.Sprintf("order-income:%d:unfreeze", ha.ID)).Scan(&releaseAmount, &consumed, &journals); err != nil || releaseAmount != 0 || consumed != 90 || journals != 0 {
		t.Fatal("zero release proof", releaseAmount, consumed, journals, err)
	}
	if _, err = s.UnfreezeOrderIncome(ctx, 902, a.TradeNo, ha.ID, incomeTestActor); err != nil {
		t.Fatal("idempotent release", err)
	}
	incomeReviewBalance(t, s, 902, "order_income_reserve", hb.ID, 90)
	incomeReviewHealthyCheck(t, s)
}

func TestReviewIncomeFailedRefundReturnsHeldAndAvailableToTheirOriginalSources(t *testing.T) {
	s := incomeRefundReviewStore(t)
	ctx := context.Background()
	feeMerchant(t, s, 903, 1000, "merchant")
	o := incomePaidOrder(t, s, 903, "review-mixed-source")
	_ = incomePaidOrder(t, s, 903, "review-available-topup")
	h, err := s.FreezeOrderIncome(ctx, 903, o.TradeNo, "freeze-mixed", incomeTestActor)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.ReserveRefund(ctx, 903, o.OutTradeNo, "mixed-refund", 100)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.RefundFundingPreview(ctx, s.pool, o.ID, r.RefundNo, 100)
	if err != nil || p.HeldAmount != 90 || p.AvailableAmount != 10 || p.FeeAmount != 0 {
		t.Fatal("original source split", p, err)
	}
	incomeReviewBalance(t, s, 903, "available", 0, 80)
	incomeReviewBalance(t, s, 903, "order_income_reserve", h.ID, 0)
	incomeReviewBalance(t, s, 903, "order_income_refund_reserve", r.ID, 100)
	if err = s.ResolveRefund(ctx, r.ID, "unknown"); err != nil {
		t.Fatal(err)
	}
	incomeReviewBalance(t, s, 903, "order_income_refund_reserve", r.ID, 100)
	if err = s.ResolveRefund(ctx, r.ID, "failed"); err != nil {
		t.Fatal(err)
	}
	incomeReviewBalance(t, s, 903, "available", 0, 90)
	incomeReviewBalance(t, s, 903, "order_income_reserve", h.ID, 90)
	incomeReviewBalance(t, s, 903, "order_income_refund_reserve", r.ID, 0)
	if err = s.ResolveRefund(ctx, r.ID, "failed"); err != nil {
		t.Fatal("idempotent failed result", err)
	}
	incomeReviewBalance(t, s, 903, "available", 0, 90)
	incomeReviewBalance(t, s, 903, "order_income_reserve", h.ID, 90)
	if _, err = s.UnfreezeOrderIncome(ctx, 903, o.TradeNo, h.ID, incomeTestActor); err != nil {
		t.Fatal(err)
	}
	incomeReviewBalance(t, s, 903, "available", 0, 180)
	incomeReviewHealthyCheck(t, s)
}

func TestReviewIncomeForgedFailedFundingProofCannotReopenOriginalBudget(t *testing.T) {
	s := incomeRefundReviewStore(t)
	ctx := context.Background()
	o := incomePaidOrder(t, s, 901, "review-failed-proof")
	h, err := s.FreezeOrderIncome(ctx, 901, o.TradeNo, "freeze-proof", incomeTestActor)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.ReserveRefund(ctx, 901, o.OutTradeNo, "failed-proof", 60)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ResolveRefund(ctx, r.ID, "failed"); err != nil {
		t.Fatal(err)
	}
	// Model a damaged restore only in this random schema: retain the full SQL
	// shape and original provider59 evidence, but destroy the new result HMAC.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `ALTER TABLE refund_funding_resolutions DISABLE TRIGGER refund_funding_result_immutable`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE refund_funding_resolutions SET result_hash=decode(repeat('00',32),'hex') WHERE refund_id=$1`, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `ALTER TABLE refund_funding_resolutions ENABLE TRIGGER refund_funding_result_immutable`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var sqlValid bool
	if err = s.pool.QueryRow(ctx, `SELECT valid FROM refund_funding_invariants WHERE refund_id=$1`, r.ID).Scan(&sqlValid); err != nil || !sqlValid {
		t.Fatal("fixture must retain SQL-valid source shape", sqlValid, err)
	}
	if _, err = s.Refund(ctx, 901, r.RefundNo); !errors.Is(err, ErrConflict) {
		t.Fatal("public refund accepted zero result HMAC", err)
	}
	if _, err = s.RefundFundingPreview(ctx, s.pool, o.ID, "replacement", 100); !errors.Is(err, ErrConflict) {
		t.Fatal("preview reopened forged failed budget", err)
	}
	if _, err = s.ReserveRefund(ctx, 901, o.OutTradeNo, "replacement", 100); !errors.Is(err, ErrConflict) {
		t.Fatal("reservation reopened forged failed budget", err)
	}
	if err = s.ResolveRefund(ctx, r.ID, "failed"); !errors.Is(err, ErrConflict) {
		t.Fatal("terminal replay accepted forged funding result", err)
	}
	if _, err = s.OrderIncomeHold(ctx, 901, o.TradeNo); !errors.Is(err, ErrConflict) {
		t.Fatal("hold view omitted failed source history", err)
	}
	if _, err = s.UnfreezeOrderIncome(ctx, 901, o.TradeNo, h.ID, incomeTestActor); !errors.Is(err, ErrConflict) {
		t.Fatal("release accepted forged failed source history", err)
	}
	incomeReviewBalance(t, s, 901, "order_income_reserve", h.ID, 100)
	incomeReviewBalance(t, s, 901, "available", 0, 0)
	var refunds int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM refunds WHERE order_id=$1`, o.ID).Scan(&refunds); err != nil || refunds != 1 {
		t.Fatal("rejected source changed refund intents", refunds, err)
	}
	report, err := s.CheckLedger(ctx)
	if err != nil || report.InvalidOrderIncomeFundings != 1 || report.InvalidRefundFundings != 1 || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
		t.Fatal("checker missed SQL-valid zero-HMAC failed source", report, err)
	}
}

func TestReviewIncomeLegacyAdoptionRollbackAndConcurrentReplayKeepOtherHoldIsolated(t *testing.T) {
	s := incomeFundingFixture(t, true)
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 904, Name: "local adoption review", APIKey: "synthetic-only"}); err != nil {
		t.Fatal(err)
	}
	o, h := incomeLegacyHold(t, s, 904, "review-adopt-original")
	_, other := incomeLegacyHold(t, s, 904, "review-adopt-other")
	if err := db.Migrate(ctx, s.pool); err != nil {
		t.Fatal(err)
	}
	super := IncomeHoldActor{ID: 1, Name: "local-review-super", Class: "super"}
	incomeReviewBalance(t, s, 904, "order_income_hold", 0, 200)
	if _, err := s.AdoptOrderIncomeHold(ctx, 904, o.TradeNo, h.ID, "review-adopt", super); !errors.Is(err, ErrInvalid) {
		t.Fatal("new adoption without mandatory guard", err)
	}
	rejected := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { return ErrConflict })
	if _, err := s.AdoptOrderIncomeHold(rejected, 904, o.TradeNo, h.ID, "review-adopt", super); !errors.Is(err, ErrConflict) {
		t.Fatal("rejected funds authorization", err)
	}
	var calls atomic.Int32
	guarded := WithFundsActionGuard(ctx, func(_ context.Context, _ pgx.Tx, kind, key string) error {
		if kind != "income-hold-adopt" || key != fmt.Sprint(h.ID) {
			return ErrInvalid
		}
		calls.Add(1)
		return nil
	})
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION review_adoption_audit_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='income-fundings' THEN RAISE EXCEPTION 'review adoption audit unavailable';END IF;RETURN NEW;END $$; CREATE TRIGGER review_adoption_audit_fault BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION review_adoption_audit_fault()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdoptOrderIncomeHold(guarded, 904, o.TradeNo, h.ID, "review-adopt", super); err == nil {
		t.Fatal("adoption ignored original actor audit failure")
	}
	incomeReviewBalance(t, s, 904, "order_income_hold", 0, 200)
	var fundings, scoped int
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM order_income_hold_fundings),(SELECT count(*) FROM accounts WHERE kind='order_income_reserve')`).Scan(&fundings, &scoped); err != nil || fundings != 0 || scoped != 0 {
		t.Fatal("failed adoption escaped transaction", fundings, scoped, err)
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER review_adoption_audit_fault ON backoffice_audit; DROP FUNCTION review_adoption_audit_fault()`); err != nil {
		t.Fatal(err)
	}
	calls.Store(0)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := s.AdoptOrderIncomeHold(guarded, 904, o.TradeNo, h.ID, "review-adopt", super)
			if err != nil || v.FundingID != h.ID || v.SourceMode != "legacy_adopt" {
				t.Error(v, err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("concurrent same adoption consumed more than one authorization", calls.Load())
	}
	incomeReviewBalance(t, s, 904, "order_income_hold", 0, 100)
	incomeReviewBalance(t, s, 904, "order_income_reserve", h.ID, 100)
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM order_income_hold_fundings`).Scan(&fundings); err != nil || fundings != 1 {
		t.Fatal(fundings, err)
	}
	var otherSource bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM order_income_hold_fundings WHERE hold_id=$1)`, other.ID).Scan(&otherSource); err != nil || otherSource {
		t.Fatal("other hold was adopted or resealed", otherSource, err)
	}
	if _, err := s.AdoptOrderIncomeHold(guarded, 904, o.TradeNo, h.ID, "different-adoption", super); !errors.Is(err, ErrConflict) {
		t.Fatal("changed external number replay", err)
	}
	incomeReviewBalance(t, s, 904, "order_income_hold", 0, 100)
	incomeReviewHealthyCheck(t, s)
}

func TestReviewIncomeCheckerAuthenticatesRefundSourceBeyondFirstPage(t *testing.T) {
	s := incomeRefundReviewStore(t)
	ctx := context.Background()
	o, err := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 901, OutTradeNo: "review-page101", Name: "local page review", Amount: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkPaid(ctx, o.TradeNo, "local-page101-paid"); err != nil {
		t.Fatal(err)
	}
	var last Refund
	for i := 0; i < 101; i++ {
		last, err = s.ReserveRefund(ctx, 901, o.OutTradeNo, fmt.Sprintf("page-%03d", i), 1)
		if err != nil {
			t.Fatal(i, err)
		}
	}
	incomeReviewHealthyCheck(t, s)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `ALTER TABLE refund_fundings DISABLE TRIGGER refund_funding_immutable`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE refund_fundings SET request_hash=decode(repeat('00',32),'hex') WHERE refund_id=$1`, last.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `ALTER TABLE refund_fundings ENABLE TRIGGER refund_funding_immutable`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var valid bool
	if err = s.pool.QueryRow(ctx, `SELECT valid FROM refund_funding_invariants WHERE refund_id=$1`, last.ID).Scan(&valid); err != nil || !valid {
		t.Fatal("page101 must remain SQL-valid", valid, err)
	}
	report, err := s.CheckLedger(ctx)
	if err != nil || report.InvalidRefundFundings != 1 || report.InvalidOrderIncomeFundings != 0 || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
		t.Fatal("checker omitted source HMAC after page100", report, err)
	}
	if _, err = s.RefundFundingPreview(ctx, s.pool, o.ID, "page102", 1); !errors.Is(err, ErrConflict) {
		t.Fatal("history omitted source HMAC after page100", err)
	}
}
