package contract_test

import (
	"context"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestNotificationDeadJobRetrySharesResendLockAndCooldown(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "notification-retry", Name: "retry", Type: "wxpay", Amount: 123, NotifyURL: "https://merchant.example.invalid/notify"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "retry-paid"); e != nil {
		t.Fatal(e)
	}
	var originalID int64
	if e = pool.QueryRow(ctx, "UPDATE jobs SET state='dead' WHERE event_key=$1 RETURNING id", "notify:"+o.TradeNo).Scan(&originalID); e != nil {
		t.Fatal(e)
	}
	token, csrf, _ := s.Sessions.Create(identity.User{Username: "admin", ID: 1, Role: "admin"})
	invoke := func(path, body string) int {
		r := httptest.NewRequest("POST", "/console/v1/admin"+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	resend := "/orders/" + o.TradeNo + "/resend-notification"
	retry := fmt.Sprintf("/operations/jobs/%d/retry", originalID)
	if status := invoke(resend, `{"external_no":"new-manual"}`); status != 202 {
		t.Fatalf("manual enqueue %d", status)
	}
	if status := invoke(retry, `{}`); status != 409 {
		t.Fatalf("dead retry created second active delivery: %d", status)
	}
	if _, e = pool.Exec(ctx, "UPDATE jobs SET state='dead' WHERE kind='payment.notify'"); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "UPDATE notification_send_controls SET last_manual_at=statement_timestamp()-interval '2 minutes'"); e != nil {
		t.Fatal(e)
	}
	// Either entry point may win, but both use the same original-order lock.
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	wg.Add(2)
	go func() { defer wg.Done(); statuses <- invoke(retry, `{}`) }()
	go func() { defer wg.Done(); statuses <- invoke(resend, `{"external_no":"parallel-manual"}`) }()
	wg.Wait()
	close(statuses)
	for status := range statuses {
		if status != 200 && status != 202 && status != 409 {
			t.Fatalf("parallel retry status %d", status)
		}
	}
	var active int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE kind='payment.notify' AND payload->>'trade_no'=$1 AND state IN ('pending','running')", o.TradeNo).Scan(&active); e != nil || active != 1 {
		t.Fatal("parallel entry points multiplied deliveries", active, e)
	}
	if _, e = pool.Exec(ctx, "UPDATE jobs SET state='dead' WHERE kind='payment.notify'"); e != nil {
		t.Fatal(e)
	}
	if status := invoke(retry, `{}`); status != 429 {
		t.Fatalf("dead retry bypassed shared cooldown: %d", status)
	}
	if status := invoke(resend, `{"external_no":"cooldown-manual"}`); status != 429 {
		t.Fatalf("resend bypassed retry cooldown: %d", status)
	}
}
