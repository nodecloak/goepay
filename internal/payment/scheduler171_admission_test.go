package payment

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"goepay/internal/platform/jobs"
)

func TestScheduler171DisabledNotificationBlocksOnlyAutomaticRetries(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	v, e := s.ScheduledTasks(ctx, a, "notify")
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(v[0])
	var dto map[string]any
	_ = json.Unmarshal(raw, &dto)
	if dto["control_mode"] != "legacy_managed" {
		t.Error("unconfigured retry worker incorrectly advertised stopped", string(raw))
	}
	_, e = s.UpdateScheduledTask(ctx, a, "notify", ScheduledTaskInput{Enabled: false, Schedule: ScheduledTaskSchedule{"minute", 1, time.Now().UTC().Truncate(time.Second)}, Params: ScheduledTaskParams{}, ExpectedFingerprint: v[0].Fingerprint})
	if e != nil {
		t.Fatal("cannot explicitly control original retry executor", e)
	}
	retry := scheduler171NotifyOrder(t, s, "retry", 1)
	scheduler171NotifyOrder(t, s, "first", 0)
	manual := scheduler171NotifyOrder(t, s, "manual", 3)
	if _, e = s.pool.Exec(ctx, `INSERT INTO notification_resend_requests(order_id,request_hash,job_id,actor_role,actor_id)SELECT $1,$2,id,'admin',171 FROM jobs WHERE event_key=$3`, manual.ID, strings.Repeat("a", 64), "notify:"+manual.TradeNo); e != nil {
		t.Fatal(e)
	}
	q := jobs.Queue{Pool: s.pool, Policy: s}
	for i := 0; i < 2; i++ {
		j, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute)
		if e != nil {
			t.Fatal(e)
		}
		if j.EventKey == "notify:"+retry.TradeNo {
			t.Fatal("disabled automatic retry actually claimed", j.ID)
		}
		if e = q.Complete(ctx, j); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = q.Claim(ctx, []string{"payment.notify"}, time.Minute); !jobs.IsEmpty(e) {
		t.Fatal("disabled cron retry reached consumer", e)
	}
}

func scheduler171NotifyOrder(t *testing.T, s *Store, name string, attempts int) Order {
	t.Helper()
	ctx := context.Background()
	if _, e := s.Merchant(ctx, 901); e != nil {
		if e = s.CreateMerchant(ctx, Merchant{ID: 901, Name: "Synthetic notifier", APIKey: "synthetic171-only"}); e != nil {
			t.Fatal(e)
		}
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 901, OutTradeNo: "scheduler171-" + name, Type: "alipay", Amount: 1000, NotifyURL: "http://127.0.0.1/synthetic171"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "synthetic171-paid-"+name); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE jobs SET attempts=$2 WHERE event_key=$1`, "notify:"+o.TradeNo, attempts); e != nil {
		t.Fatal(e)
	}
	return o
}

func scheduler171ConfigureManaged(t *testing.T, s *Store, a MerchantCredentialActor, key string, enabled bool) ScheduledTaskView {
	t.Helper()
	v, e := s.ScheduledTasks(context.Background(), a, key)
	if e != nil {
		t.Fatal(e)
	}
	saved, e := s.UpdateScheduledTask(context.Background(), a, key, ScheduledTaskInput{Enabled: enabled, Schedule: ScheduledTaskSchedule{"once", 1, time.Now().UTC().Add(time.Hour).Truncate(time.Second)}, Params: scheduledDefaults(key), ExpectedFingerprint: v[0].Fingerprint})
	if e != nil {
		t.Fatal(e)
	}
	return saved
}

func TestScheduler171RetryAdmissionIsFiniteAndActualLeaseBound(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	o := scheduler171NotifyOrder(t, s, "finite", 1)
	q := jobs.Queue{Pool: s.pool, Policy: s}
	scheduler171ConfigureManaged(t, s, a, "notify", true)
	if _, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute); !jobs.IsEmpty(e) {
		t.Fatal("enabled without scheduler intent sent retry", e)
	}
	scheduler171Pending(t, s, a, "notify")
	tick, e := s.RunScheduledTasksOnce(ctx, 20)
	if e != nil || tick.Completed != 1 {
		t.Fatal("original retry admission not executed", tick, e)
	}
	j, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute)
	if e != nil || j.EventKey != "notify:"+o.TradeNo || j.Attempts != 2 {
		t.Fatal("original job/attempt lost", j.ID, j.Attempts, e)
	}
	if e = s.VerifyScheduledJobClaim(ctx, j); e != nil {
		t.Fatal("actual admitted claim rejected", e)
	}
	bad := j
	bad.Token = "different-lease"
	if e = s.VerifyScheduledJobClaim(ctx, bad); e == nil {
		t.Fatal("another lease adopted finite admission")
	}
	scheduler171ConfigureManaged(t, s, a, "notify", false)
	if e = s.VerifyScheduledJobClaim(ctx, j); e != nil {
		t.Fatal("inflight original lease incorrectly revoked", e)
	}
	if e = q.Retry(ctx, j, "synthetic refusal", 0); e != nil {
		t.Fatal(e)
	}
	if _, e = q.Claim(ctx, []string{"payment.notify"}, time.Minute); !jobs.IsEmpty(e) {
		t.Fatal("disabled/consumed admission claimed next attempt", e)
	}
	scheduler171ConfigureManaged(t, s, a, "notify", true)
	if _, e = q.Claim(ctx, []string{"payment.notify"}, time.Minute); !jobs.IsEmpty(e) {
		t.Fatal("new generation adopted old admission", e)
	}
	// A consumer omitting the queue hook still cannot send under a taken-over policy.
	bypass, e := (jobs.Queue{Pool: s.pool}).Claim(ctx, []string{"payment.notify"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.VerifyScheduledJobClaim(ctx, bypass); e == nil {
		t.Fatal("consumer bypassed scheduler admission")
	}
	var total, admitted, claims int
	if e = s.pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM jobs WHERE kind='payment.notify'),(SELECT count(*)FROM scheduled_job_admissions),(SELECT count(*)FROM scheduled_job_claims)`).Scan(&total, &admitted, &claims); e != nil || total != 1 || admitted != 1 || claims != 1 {
		t.Fatal("new sender/job intent created", total, admitted, claims, e)
	}
}

func TestScheduler171OrderQueryWindowAndOtherReconcileRemainIndependent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	if e := s.CreateMerchant(ctx, Merchant{ID: 901, Name: "Synthetic query", APIKey: "synthetic171"}); e != nil {
		t.Fatal(e)
	}
	// Old and too-old rows model imported pre104 history. New104 order birth
	// proofs authenticate created_at, so a newly born order must not be aged by
	// rewriting its timestamp. Fresh/preauth still use normal current creation.
	var e error
	trades := map[string]string{}
	for _, name := range []string{"old", "fresh", "too-old", "preauth"} {
		var o Order
		if name == "old" || name == "too-old" {
			o, e = scanOrder(s.pool.QueryRow(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount,platform_fee,fee_policy,refund_fee_payer) VALUES(nextval('trade_no_seq')::text,901,$1,'historical query','alipay',1000,1000,0,'none','platform') RETURNING `+orderColumns, "scheduler171-"+name))
		} else {
			o, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: 901, OutTradeNo: "scheduler171-" + name, Type: "alipay", Amount: 1000})
		}
		if e != nil {
			t.Fatal(e)
		}
		age := 0
		if name == "old" {
			age = 120
		}
		if name == "too-old" {
			age = 480
		}
		if age > 0 {
			if _, e = s.pool.Exec(ctx, `UPDATE orders SET created_at=clock_timestamp()-$2::bigint*interval '1 minute' WHERE id=$1`, o.ID, age); e != nil {
				t.Fatal(e)
			}
		}
		method := "native"
		if name == "preauth" {
			method = "preauth"
		}
		if _, _, e = s.BeginProviderAttempt(ctx, o.TradeNo, 7, method); e != nil {
			t.Fatal(e)
		}
		trades[name] = o.TradeNo
	}
	if _, e = s.pool.Exec(ctx, `UPDATE jobs SET run_at=clock_timestamp()-interval '1 second' WHERE kind='payment.reconcile'`); e != nil {
		t.Fatal(e)
	}
	scheduler171ConfigureManaged(t, s, a, "order_query", false)
	q := jobs.Queue{Pool: s.pool, Policy: s}
	pre, e := q.Claim(ctx, []string{"payment.reconcile"}, time.Minute)
	if e != nil || pre.EventKey != "payment-reconcile:"+trades["preauth"] {
		t.Fatal("unpaid switch stopped preauth recovery", pre.EventKey, e)
	}
	if e = s.VerifyScheduledJobClaim(ctx, pre); e != nil {
		t.Fatal("preauth was reclassified as unpaid polling", e)
	}
	if e = q.Complete(ctx, pre); e != nil {
		t.Fatal(e)
	}
	if _, e = q.Claim(ctx, []string{"payment.reconcile"}, time.Minute); !jobs.IsEmpty(e) {
		t.Fatal("disabled ordinary payment polling claimed", e)
	}
	scheduler171ConfigureManaged(t, s, a, "order_query", true)
	scheduler171Pending(t, s, a, "order_query")
	tick, e := s.RunScheduledTasksOnce(ctx, 20)
	if e != nil || tick.Completed != 1 {
		t.Fatal("payment query admission failed", tick, e)
	}
	original, e := q.ClaimProviderChannels(ctx, []string{"payment.reconcile"}, time.Minute, []int64{7})
	if e != nil || original.EventKey != "payment-reconcile:"+trades["old"] {
		t.Fatal("original age window not consumed", original.EventKey, e)
	}
	if e = s.VerifyScheduledJobClaim(ctx, original); e != nil {
		t.Fatal(e)
	}
	if e = q.Complete(ctx, original); e != nil {
		t.Fatal(e)
	}
	if _, e = q.Claim(ctx, []string{"payment.reconcile"}, time.Minute); !jobs.IsEmpty(e) {
		t.Fatal("outside original 60-minute+6h window admitted", e)
	}
	var attempts, orders, admissions int
	if e = s.pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM provider_attempts),(SELECT count(*)FROM orders),(SELECT count(*)FROM scheduled_job_admissions)`).Scan(&attempts, &orders, &admissions); e != nil || attempts != 4 || orders != 4 || admissions != 1 {
		t.Fatal("query task created new financial intent", attempts, orders, admissions, e)
	}
}
