package payment

import (
	"context"
	"testing"
	"time"

	"goepay/internal/platform/jobs"
)

func TestScheduler171OriginalExplicitJobRetryRemainsIndependent(t *testing.T) {
	for _, task := range []string{"notify", "order_query"} {
		t.Run(task, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			a := scheduler171Admin(t, s)
			var jobID int64
			kind := "payment.notify"
			if task == "notify" {
				o := scheduler171NotifyOrder(t, s, "explicit-old-job-retry", 3)
				if e := s.pool.QueryRow(ctx, `SELECT id FROM jobs WHERE event_key=$1`, "notify:"+o.TradeNo).Scan(&jobID); e != nil {
					t.Fatal(e)
				}
			} else {
				if e := s.CreateMerchant(ctx, Merchant{ID: 901, Name: "Synthetic explicit query", APIKey: "synthetic171"}); e != nil {
					t.Fatal(e)
				}
				o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 901, OutTradeNo: "scheduler171-explicit-query", Type: "alipay", Amount: 100})
				if e != nil {
					t.Fatal(e)
				}
				if _, _, e = s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); e != nil {
					t.Fatal(e)
				}
				kind = "payment.reconcile"
				if e = s.pool.QueryRow(ctx, `UPDATE jobs SET run_at=clock_timestamp()-interval '1 second' WHERE event_key=$1 RETURNING id`, "payment-reconcile:"+o.TradeNo).Scan(&jobID); e != nil {
					t.Fatal(e)
				}
			}
			scheduler171ConfigureManaged(t, s, a, task, false)
			// Synthetic original persistent audit emitted only by the existing explicit
			// administrator /operations/jobs/{id}/retry transaction. This is not cron.
			if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation)VALUES('admin',171,'Synthetic explicit actor','jobs',$1,'retry')`, jobID); e != nil {
				t.Fatal(e)
			}
			q := jobs.Queue{Pool: s.pool, Policy: s}
			j, e := q.Claim(ctx, []string{kind}, time.Minute)
			if e != nil || j.ID != jobID {
				t.Fatal("cron disabled an independently authorized explicit retry", e)
			}
			if e = s.VerifyScheduledJobClaim(ctx, j); e != nil {
				t.Fatal("handler reclassified explicit retry as cron", e)
			}
		})
	}
}
