package reconciliation

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/notification"
	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
)

func scheduler171Actor(t *testing.T, h *Handler) payment.MerchantCredentialActor {
	t.Helper()
	ctx := context.Background()
	if _, e := h.Pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class)VALUES('scheduler171','admin',171,'Synthetic Scheduler','unused','super')`); e != nil {
		t.Fatal(e)
	}
	u := identity.User{Username: "scheduler171", ID: 171, Role: "admin", AdminClass: "super", AuthVersion: 1}
	token, _, e := identity.NewPersistentSessions(h.Pool, false).Create(u)
	if e != nil {
		t.Fatal(e)
	}
	return payment.MerchantCredentialActor{User: u, SessionToken: token}
}
func scheduler171Policy(t *testing.T, h *Handler, a payment.MerchantCredentialActor, key string, enabled bool) {
	t.Helper()
	ctx := context.Background()
	v, e := h.Flow.Store.ScheduledTasks(ctx, a, key)
	if e != nil {
		t.Fatal(e)
	}
	params := payment.ScheduledTaskParams{}
	if key == "order_query" {
		params["unpaid_minutes"] = 60
	}
	if _, e = h.Flow.Store.UpdateScheduledTask(ctx, a, key, payment.ScheduledTaskInput{Enabled: enabled, Schedule: payment.ScheduledTaskSchedule{Frequency: "once", Interval: 1, ExecuteAt: time.Now().UTC().Add(time.Hour).Truncate(time.Second)}, Params: params, ExpectedFingerprint: v[0].Fingerprint}); e != nil {
		t.Fatal(e)
	}
}
func scheduler171Run(t *testing.T, h *Handler, a payment.MerchantCredentialActor, key string) {
	t.Helper()
	ctx := context.Background()
	v, e := h.Flow.Store.ScheduledTasks(ctx, a, key)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.Flow.Store.RequestScheduledTaskRun(ctx, a, key, v[0].Fingerprint); e != nil {
		t.Fatal(e)
	}
	if tick, e := h.Flow.Store.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 || tick.Failed != 0 {
		t.Fatal("real admission handler failed", tick, e)
	}
}

func TestScheduler171ActualPaymentQueryConsumerRequiresOriginalAdmission(t *testing.T) {
	h, p, o := fixtureWithLegacyAge190(t, 2*time.Hour)
	ctx := context.Background()
	a := scheduler171Actor(t, h)
	if _, e := h.Pool.Exec(ctx, `UPDATE jobs SET run_at=clock_timestamp()-interval '1 second' WHERE kind='payment.reconcile'`); e != nil {
		t.Fatal(e)
	}
	q := jobs.Queue{Pool: h.Pool, Policy: h.Flow.Store}
	scheduler171Policy(t, h, a, "order_query", false)
	if _, e := q.Claim(ctx, []string{"payment.reconcile"}, time.Minute); !jobs.IsEmpty(e) {
		t.Fatal("disabled ordinary polling was claimed", e)
	}
	if e := h.Handle(ctx, job("payment.reconcile", map[string]string{"trade_no": o.TradeNo})); e == nil || p.queries != 0 {
		t.Fatal("direct consumer bypassed admission", p.queries, e)
	}
	scheduler171Policy(t, h, a, "order_query", true)
	scheduler171Run(t, h, a, "order_query")
	j, e := q.ClaimProviderChannels(ctx, []string{"payment.reconcile"}, time.Minute, []int64{7})
	if e != nil {
		t.Fatal(e)
	}
	scheduler171Policy(t, h, a, "order_query", false)
	for i := 0; i < 2; i++ {
		if e = h.Handle(ctx, j); e != nil {
			t.Fatal("original in-flight query could not complete", e)
		}
	}
	if e = q.Complete(ctx, j); e != nil {
		t.Fatal(e)
	}
	paid, e := h.Flow.Store.OrderByTradeNo(ctx, o.TradeNo)
	if e != nil || paid.Status != "paid" || p.queries != 1 || p.creates != 1 || p.refunds != 0 || p.transfers != 0 {
		t.Fatal("query re-created funds intent", paid.Status, p, e)
	}
}

func TestScheduler171ActualNotifyConsumerKeepsFirstSendAndUsesRetryPolicy(t *testing.T) {
	h, _, _ := fixture(t)
	ctx := context.Background()
	a := scheduler171Actor(t, h)
	var sends atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, e := compat.Parse(r.URL.Query())
		if e != nil || !compat.VerifyMD5(p, "synthetic-secret", p["sign"]) {
			t.Error("original notification signature changed", e)
		}
		if sends.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, "success")
	}))
	defer sink.Close()
	o, e := h.Flow.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "scheduler171-notify", Type: "wxpay", Amount: 100, NotifyURL: sink.URL})
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Flow.Store.MarkPaid(ctx, o.TradeNo, "synthetic171-notify-paid"); e != nil {
		t.Fatal(e)
	}
	scheduler171Policy(t, h, a, "notify", false)
	q := jobs.Queue{Pool: h.Pool, Policy: h.Flow.Store}
	sender := notification.Handler{Store: h.Flow.Store, Client: sink.Client()}
	first, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute)
	if e != nil {
		t.Fatal("cron disable suppressed original first send", e)
	}
	if e = sender.Handle(ctx, first); e == nil || sends.Load() != 1 {
		t.Fatal("real first notification not exercised", sends.Load(), e)
	}
	if e = q.Retry(ctx, first, "synthetic first ack refused", 0); e != nil {
		t.Fatal(e)
	}
	if _, e = q.Claim(ctx, []string{"payment.notify"}, time.Minute); !jobs.IsEmpty(e) || sends.Load() != 1 {
		t.Fatal("disabled retry actually sent", sends.Load(), e)
	}
	bypass, e := (jobs.Queue{Pool: h.Pool}).Claim(ctx, []string{"payment.notify"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = sender.Handle(ctx, bypass); e == nil || sends.Load() != 1 {
		t.Fatal("unhooked consumer performed external I/O", sends.Load(), e)
	}
	if e = q.Retry(ctx, bypass, "synthetic unhooked claim rejected", 0); e != nil {
		t.Fatal(e)
	}
	scheduler171Policy(t, h, a, "notify", true)
	scheduler171Run(t, h, a, "notify")
	retry, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute)
	if e != nil || retry.ID != first.ID || retry.Attempts != 3 {
		t.Fatal("retry replaced original job/attempt", retry.ID, retry.Attempts, e)
	}
	if e = sender.Handle(ctx, retry); e != nil || sends.Load() != 2 {
		t.Fatal("admitted original retry failed", sends.Load(), e)
	}
	if e = q.Complete(ctx, retry); e != nil {
		t.Fatal(e)
	}
	if _, e = q.Claim(ctx, []string{"payment.notify"}, time.Minute); !jobs.IsEmpty(e) {
		t.Fatal("successful notification was re-enqueued", e)
	}
}
