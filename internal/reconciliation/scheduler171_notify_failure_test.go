package reconciliation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"goepay/internal/notification"
	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
)

func TestScheduler171VerifiedConsecutiveNotifyFailureClosesOnlyFuturePaymentPermission(t *testing.T) {
	h, p, original := fixture(t)
	ctx := context.Background()
	a := scheduler171Actor(t, h)
	scheduler171Policy(t, h, a, "notify", true)
	for key, value := range map[string]string{"auto_check_notify": "1", "check_notify_count": "2", "check_notify_notice": "0"} {
		if _, e := h.Pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))`, key, value); e != nil {
			t.Fatal(e)
		}
	}
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer sink.Close()
	for _, no := range []string{"scheduler171-failure-a", "scheduler171-failure-b"} {
		o, e := h.Flow.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: no, Type: "wxpay", Amount: 100, NotifyURL: sink.URL})
		if e != nil {
			t.Fatal(e)
		}
		if e = h.Flow.Store.MarkPaid(ctx, o.TradeNo, "synthetic-paid-"+no); e != nil {
			t.Fatal(e)
		}
	}
	q := jobs.Queue{Pool: h.Pool, Policy: h.Flow.Store}
	sender := notification.Handler{Store: h.Flow.Store, Client: sink.Client()}
	for range 2 {
		j, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute)
		if e != nil || j.Attempts != 1 {
			t.Fatal(e)
		}
		failure := sender.Handle(ctx, j)
		code, ok := notification.FailureCode(failure, j)
		if !ok {
			t.Fatal("actual failed notification required", failure)
		}
		if e = q.RetryNotificationFailure(ctx, j, code, 0); e != nil {
			t.Fatal(e)
		}
		var seconds float64
		if e = h.Pool.QueryRow(ctx, `SELECT extract(epoch FROM run_at-clock_timestamp())FROM jobs WHERE id=$1`, j.ID).Scan(&seconds); e != nil {
			t.Fatal(e)
		}
		if seconds < 55 || seconds > 65 {
			t.Error("original first retry delay must be one minute", seconds)
		}
	}
	var pay int
	if e := h.Pool.QueryRow(ctx, `SELECT pay FROM merchants WHERE id=1`).Scan(&pay); e != nil || pay != 1 {
		t.Fatal("first independent sends triggered cron-only risk action", pay, e)
	}
	// Local synthetic clock acceleration; preserve the original jobs and attempts.
	if _, e := h.Pool.Exec(ctx, `UPDATE jobs SET run_at=clock_timestamp()-interval '1 second' WHERE kind='payment.notify'`); e != nil {
		t.Fatal(e)
	}
	scheduler171Run(t, h, a, "notify")
	retry, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute)
	if e != nil || retry.Attempts != 2 {
		t.Fatal(e)
	}
	failure := sender.Handle(ctx, retry)
	code, ok := notification.FailureCode(failure, retry)
	if !ok {
		t.Fatal(failure)
	}
	if e = q.RetryNotificationFailure(ctx, retry, code, 0); e != nil {
		t.Fatal(e)
	}
	if e = h.Pool.QueryRow(ctx, `SELECT pay FROM merchants WHERE id=1`).Scan(&pay); e != nil || pay != 0 {
		t.Error("verified consecutive automatic failures did not close payment permission", pay, e)
	}
	var risks int
	if e = h.Pool.QueryRow(ctx, `SELECT count(*)FROM backoffice_records WHERE resource='risk-records' AND body->>'source'='notification-failure' AND body->>'merchant_id'='1'`).Scan(&risks); e != nil || risks != 1 {
		t.Error("original source risk record missing or duplicated", risks, e)
	}
	if _, e = h.Flow.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "scheduler171-after-risk", Amount: 100}); !errors.Is(e, payment.ErrState) {
		t.Error("new payment admitted after automatic close", e)
	}
	// Already admitted original uncertain payment still reconciles; no new send.
	if _, e = h.Flow.Query(ctx, 1, original.TradeNo); e != nil || p.creates != 1 || p.queries != 1 {
		t.Fatal("original outcome recovery was incorrectly stopped", p, e)
	}
	if e = q.RetryNotificationFailure(ctx, retry, code, 0); !errors.Is(e, jobs.ErrLeaseLost) {
		t.Fatal("completed failure lease replayed", e)
	}
}
