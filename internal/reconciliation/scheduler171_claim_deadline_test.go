package reconciliation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"goepay/internal/notification"
	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
)

func TestScheduler171ClaimLastAdmissionWritePastOriginalLeaseRollsBack(t *testing.T) {
	for _, task := range []string{"notify", "order_query"} {
		t.Run(task, func(t *testing.T) {
			age := time.Duration(0)
			if task == "order_query" {
				age = 2 * time.Hour
			}
			h, provider, order := fixtureWithLegacyAge190(t, age)
			ctx := context.Background()
			a := scheduler171Actor(t, h)
			var calls atomic.Int32
			sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, "success") }))
			defer sink.Close()
			kind, event, baseline := "payment.reconcile", "payment-reconcile:"+order.TradeNo, 0
			if task == "notify" {
				var e error
				order, e = h.Flow.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "scheduler171-claim-deadline", Type: "wxpay", Amount: 100, NotifyURL: sink.URL})
				if e != nil {
					t.Fatal(e)
				}
				if e = h.Flow.Store.MarkPaid(ctx, order.TradeNo, "scheduler171-claim-paid"); e != nil {
					t.Fatal(e)
				}
				kind, event, baseline = "payment.notify", "notify:"+order.TradeNo, 1
			}
			if _, e := h.Pool.Exec(ctx, `UPDATE jobs SET run_at=clock_timestamp()-interval '1 second',attempts=$2 WHERE event_key=$1`, event, baseline); e != nil {
				t.Fatal(e)
			}
			scheduler171Policy(t, h, a, task, true)
			scheduler171Run(t, h, a, task)
			if _, e := h.Pool.Exec(ctx, `CREATE FUNCTION scheduler171_claim_write_delay()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM pg_sleep(1.2);RETURN NEW;END$$;CREATE TRIGGER scheduler171_claim_write_delay BEFORE INSERT ON scheduled_job_claims FOR EACH ROW EXECUTE FUNCTION scheduler171_claim_write_delay()`); e != nil {
				t.Fatal(e)
			}
			q := jobs.Queue{Pool: h.Pool, Policy: h.Flow.Store}
			j, e := q.Claim(ctx, []string{kind}, time.Second)
			if !errors.Is(e, jobs.ErrLeaseLost) || j.ID != 0 {
				t.Errorf("expired claim committed instead of atomic rollback: returned_id=%d err=%v", j.ID, e)
			}
			if e == nil {
				// This is the actual original consumer. Its subsequent rejection is
				// deliberately insufficient: the attempt/claim must not commit.
				if task == "notify" {
					_ = (notification.Handler{Store: h.Flow.Store, Client: sink.Client()}).Handle(ctx, j)
				} else {
					_ = h.Handle(ctx, j)
				}
			}
			var state string
			var attempts, claims int
			var leaseFree bool
			if e = h.Pool.QueryRow(ctx, `SELECT state,attempts,lease_token IS NULL AND lease_until IS NULL,(SELECT count(*)FROM scheduled_job_claims)FROM jobs WHERE event_key=$1`, event).Scan(&state, &attempts, &leaseFree, &claims); e != nil {
				t.Fatal(e)
			}
			if state != "pending" || attempts != baseline || !leaseFree || claims != 0 {
				t.Errorf("expired original lease consumed retry budget: state=%s attempts=%d baseline=%d lease_free=%v claims=%d", state, attempts, baseline, leaseFree, claims)
			}
			if calls.Load() != 0 || provider.queries != 0 {
				t.Fatal("expired claim reached actual external consumer", calls.Load(), provider.queries)
			}
			if _, e = h.Pool.Exec(ctx, `DROP TRIGGER scheduler171_claim_write_delay ON scheduled_job_claims`); e != nil {
				t.Fatal(e)
			}
			recovered, e := q.Claim(ctx, []string{kind}, time.Minute)
			if e != nil || recovered.Attempts != baseline+1 || recovered.EventKey != event {
				t.Fatal("rollback lost original pending attempt or finite admission", recovered.ID, recovered.Attempts, e)
			}
			if task == "notify" {
				if e = (notification.Handler{Store: h.Flow.Store, Client: sink.Client()}).Handle(ctx, recovered); e != nil || calls.Load() != 1 {
					t.Fatal("original notification did not recover once", calls.Load(), e)
				}
			} else {
				if e = h.Handle(ctx, recovered); e != nil || provider.queries != 1 || provider.creates != 1 {
					t.Fatal("original query did not recover without a new create", provider.queries, provider.creates, e)
				}
			}
			if e = q.Complete(ctx, recovered); e != nil {
				t.Fatal(e)
			}
			var admitted int
			if e = h.Pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM scheduled_job_admissions),(SELECT count(*)FROM scheduled_job_claims)`).Scan(&admitted, &claims); e != nil || admitted != 1 || claims != 1 {
				t.Fatal("recovery replaced original admission or consumed it twice", admitted, claims, e)
			}
		})
	}
}

type scheduler171ObservedClaimPolicy struct {
	jobs.ClaimPolicy
	backend chan int
}

func (p scheduler171ObservedClaimPolicy) ClaimFilter(ctx context.Context, tx pgx.Tx, arg int) (string, []any, error) {
	var pid int
	if e := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		return "", nil, e
	}
	p.backend <- pid
	return p.ClaimPolicy.ClaimFilter(ctx, tx, arg)
}

func TestScheduler171PolicyShareWaitDoesNotSpendNewOriginalLease(t *testing.T) {
	h, _, _ := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	a := scheduler171Actor(t, h)
	var calls atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, "success") }))
	defer sink.Close()
	o, e := h.Flow.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "scheduler171-policy-share-wait", Type: "wxpay", Amount: 100, NotifyURL: sink.URL})
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Flow.Store.MarkPaid(ctx, o.TradeNo, "scheduler171-share-paid"); e != nil {
		t.Fatal(e)
	}
	if _, e = h.Pool.Exec(ctx, `UPDATE jobs SET attempts=1 WHERE event_key=$1`, "notify:"+o.TradeNo); e != nil {
		t.Fatal(e)
	}
	scheduler171Policy(t, h, a, "notify", true)
	scheduler171Run(t, h, a, "notify")
	blocker, e := h.Pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(ctx)
	if _, e = blocker.Exec(ctx, `UPDATE scheduled_tasks SET enabled=enabled WHERE task_key='notify'`); e != nil {
		t.Fatal(e)
	}
	pidCh := make(chan int, 1)
	q := jobs.Queue{Pool: h.Pool, Policy: scheduler171ObservedClaimPolicy{ClaimPolicy: h.Flow.Store, backend: pidCh}}
	type claimed struct {
		job jobs.Job
		err error
	}
	result := make(chan claimed, 1)
	go func() { j, e := q.Claim(ctx, []string{"payment.notify"}, time.Second); result <- claimed{j, e} }()
	pid := <-pidCh
	for {
		var waiting bool
		if e = h.Pool.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1))>0`, pid).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("actual policy SHARE wait was not observed")
		case <-time.After(10 * time.Millisecond):
		}
	}
	time.Sleep(1200 * time.Millisecond)
	if e = blocker.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	got := <-result
	if got.err != nil {
		t.Fatal("new lease began before actual claim after policy lock", got.err)
	}
	var left float64
	if e = h.Pool.QueryRow(ctx, `SELECT EXTRACT(epoch FROM(lease_until-clock_timestamp()))FROM jobs WHERE id=$1`, got.job.ID).Scan(&left); e != nil || left <= 0 || left > 1.01 {
		t.Fatal("policy SHARE waiting consumed newly created lease", left, e)
	}
	if e = (notification.Handler{Store: h.Flow.Store, Client: sink.Client()}).Handle(ctx, got.job); e != nil || calls.Load() != 1 {
		t.Fatal("normal original consumer after policy wait failed", calls.Load(), e)
	}
	if e = q.Complete(ctx, got.job); e != nil {
		t.Fatal(e)
	}
}
