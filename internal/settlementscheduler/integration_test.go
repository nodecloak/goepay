package settlementscheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
	"os"
	"sync"
	"testing"
	"time"
)

func schedulerFixture(t *testing.T) (*pgxpool.Pool, *payment.Store, int64) {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	var nonce [8]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	schema := "scheduler_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = base.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		base.Close()
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = base.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		base.Close()
	})
	if err = db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store, err := payment.NewWithKey(pool, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	const mid int64 = 901
	if err = store.CreateMerchant(ctx, payment.Merchant{ID: mid, Name: "Local scheduler merchant", APIKey: "local-scheduler-only"}); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	recipient, err := store.SetSettlementRecipient(ctx, mid, payment.SettlementRecipientInput{Type: "alipay", Account: "local-payee@example.test", Name: "Local Payee"}, &zero, payment.SettlementRecipientActor{Role: "merchant", ID: mid, Name: "local-owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SetSettlementSchedulePreference(ctx, mid, 0, true, recipient.ConfigID, payment.SettlementPlanActor{Role: "merchant", ID: mid, Name: "local-owner"}); err != nil {
		t.Fatal(err)
	}
	admin := payment.SettlementPlanActor{Role: "admin", Class: "super", ID: 1, Name: "local-authorizer"}
	var now time.Time
	if err = pool.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		t.Fatal(err)
	}
	// A due one-shot fixture must stay in today's business window. Backdating
	// one minute makes the real scheduler correctly refuse it just after midnight.
	p, err := store.CreateSettlementPlan(ctx, payment.SettlementPlanInput{Name: "Local generated application", Mode: "generate_only", MerchantIDs: []int64{mid}, Schedule: payment.SettlementScheduleSpec{Frequency: "once", AnchorAt: now.Truncate(time.Second)}}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "CREATE TABLE local_scheduler_grants(action_key text PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	guarded := payment.WithFundsActionGuard(ctx, func(ctx context.Context, tx pgx.Tx, kind, key string) error {
		if kind != "settlement-plan-authorize" {
			return fmt.Errorf("unexpected authorization kind")
		}
		_, e := tx.Exec(ctx, "INSERT INTO local_scheduler_grants(action_key)VALUES($1)", key)
		return e
	})
	if _, err = store.AuthorizeSettlementPlan(guarded, p.ID, p.Revision, payment.SettlementPlanAuthorizationInput{ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), MaxItemGross: 1000, MaxRunGross: 1000, MaxDayGross: 1000, MaxDayItems: 1}, admin); err != nil {
		t.Fatal(err)
	}
	return pool, store, p.ID
}

func TestPostgresMultipleTicksOneRunAndExpiredLeaseRecovery(t *testing.T) {
	pool, store, planID := schedulerFixture(t)
	ctx := context.Background()
	o, err := store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 901, OutTradeNo: "local-scheduler-income", Name: "Local", Amount: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.MarkPaid(ctx, o.TradeNo, "local-only-payment"); err != nil {
		t.Fatal(err)
	}
	var before int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM journals").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err = (Scheduler{Store: store}).Tick(ctx); !errors.Is(err, ErrDisabled) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := (Scheduler{Store: store, Enabled: true}).Tick(ctx); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	var runs, jobsCount, journals int
	var runID int64
	if err = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM settlement_plan_runs),(SELECT count(*) FROM jobs WHERE kind='settlement-plan.generate'),(SELECT count(*) FROM journals),(SELECT id FROM settlement_plan_runs LIMIT 1)").Scan(&runs, &jobsCount, &journals, &runID); err != nil || runs != 1 || jobsCount != 1 || journals != before {
		t.Fatal("tick changed funds or duplicated original run", runs, jobsCount, journals, before, err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO jobs(event_key,kind,payload)VALUES('local-unrelated','payment.notify','{}')"); err != nil {
		t.Fatal(err)
	}
	q := jobs.Queue{Pool: pool}
	old, err := q.Claim(ctx, []string{GenerateKind}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	h := Handler{Store: store}
	if err = h.Handle(ctx, old); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE jobs SET lease_until=now()-interval '1 second' WHERE id=$1", old.ID); err != nil {
		t.Fatal(err)
	}
	fresh, err := q.Claim(ctx, []string{GenerateKind}, time.Minute)
	if err != nil || fresh.ID != old.ID || fresh.Token == old.Token {
		t.Fatal("expired original job not recovered", fresh, err)
	}
	if err = q.Complete(ctx, old); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("old lease completed", err)
	}
	if err = q.Renew(ctx, old, time.Minute); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("old lease renewed after recovery", err)
	}
	if err = q.Retry(ctx, old, "local stale attempt", time.Second); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatal("old lease retried after recovery", err)
	}
	if err = h.Handle(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if err = q.Complete(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	view, err := store.SettlementPlanRun(ctx, runID, payment.SettlementPlanActor{Role: "admin", Class: "super", ID: 1, Name: "local-authorizer"})
	if err != nil || view.PlanID != planID || view.CreatedCount != 1 {
		t.Fatal("handler did not preserve original single generated request", view, err)
	}
	var requests, uses, grants, unrelated int
	if err = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM ledger_requests),(SELECT count(*) FROM settlement_plan_budget_uses),(SELECT count(*) FROM local_scheduler_grants),(SELECT count(*) FROM jobs WHERE event_key='local-unrelated' AND state='pending')").Scan(&requests, &uses, &grants, &unrelated); err != nil || requests != 1 || uses != 1 || grants != 1 || unrelated != 1 {
		t.Fatal("recovery duplicated reserve or touched other worker queue", requests, uses, grants, unrelated, err)
	}
	if err = (Scheduler{Store: store, Enabled: true}).Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var after int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE kind='settlement-plan.generate'").Scan(&after); err != nil || after != 1 {
		t.Fatal("terminal original run requeued", after, err)
	}
}

type gatedGenerator struct {
	store   *payment.Store
	started chan struct{}
	release chan struct{}
}

func (g gatedGenerator) GenerateSettlementPlanRun(ctx context.Context, id int64) error {
	close(g.started)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-g.release:
		return g.store.GenerateSettlementPlanRun(ctx, id)
	}
}
func TestPostgresRunnerRenewsOriginalLeaseBeforeCompletion(t *testing.T) {
	pool, store, _ := schedulerFixture(t)
	ctx := context.Background()
	s := Scheduler{Store: store, Enabled: true}
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	gate := gatedGenerator{store: store, started: make(chan struct{}), release: make(chan struct{})}
	q := jobs.Queue{Pool: pool}
	runner := Runner{Scheduler: s, Queue: q, Handler: Handler{Store: gate}, Lease: time.Second, RenewInterval: 50 * time.Millisecond}
	done := make(chan error, 1)
	go func() { done <- runner.ProcessOne(ctx) }()
	select {
	case <-gate.started:
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not start")
	}
	var initial, current time.Time
	if err := pool.QueryRow(ctx, "SELECT lease_until FROM jobs WHERE kind='settlement-plan.generate'").Scan(&initial); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := pool.QueryRow(ctx, "SELECT lease_until FROM jobs WHERE kind='settlement-plan.generate'").Scan(&current); err != nil {
			t.Fatal(err)
		}
		if current.After(initial.Add(20 * time.Millisecond)) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !current.After(initial.Add(20 * time.Millisecond)) {
		t.Fatal("original live lease was not renewed", initial, current)
	}
	close(gate.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not finish")
	}
	var state string
	var attempts int
	if err := pool.QueryRow(ctx, "SELECT state,attempts FROM jobs WHERE kind='settlement-plan.generate'").Scan(&state, &attempts); err != nil || state != "succeeded" || attempts != 1 {
		t.Fatal("renewed lease not completed with original token", state, attempts, err)
	}
}
