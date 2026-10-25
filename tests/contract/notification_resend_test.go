package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/notification"
	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConsoleNotificationResendIsScopedDurableAndCoalesced(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	var deliveries atomic.Int32
	merchant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, e := compat.Parse(r.URL.Query())
		if e != nil || !compat.VerifyMD5(p, "contract-secret", p["sign"]) || p["money"] != "1.23" || p["trade_status"] != "TRADE_SUCCESS" {
			t.Error("resend payload/signature mismatch", e)
			http.Error(w, "bad", 400)
			return
		}
		deliveries.Add(1)
		fmt.Fprint(w, "success")
	}))
	defer merchant.Close()
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "notify-resend", Name: "notify", Type: "wxpay", Amount: 123, NotifyURL: merchant.URL + "/notify"})
	if e != nil {
		t.Fatal(e)
	}
	mt, mc, _ := s.Sessions.Create(identity.User{Username: "merchant", ID: 1, Role: "merchant"})
	at, ac, _ := s.Sessions.Create(identity.User{Username: "admin", ID: 1, Role: "admin"})
	invoke := func(role, token, csrf, no string) (int, map[string]any) {
		r := httptest.NewRequest("POST", "/console/v1/"+role+"/orders/"+o.TradeNo+"/resend-notification", strings.NewReader(fmt.Sprintf(`{"external_no":%q}`, no)))
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	if status, _ := invoke("merchant", mt, mc, "unpaid"); status != 409 {
		t.Fatalf("unpaid notification accepted: %d", status)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "notify-paid"); e != nil {
		t.Fatal(e)
	}
	if status, _ := invoke("merchant", mt, "wrong", "csrf"); status != 403 {
		t.Fatalf("missing CSRF: %d", status)
	}
	// The existing automatic pending delivery is reused, never multiplied.
	if status, out := invoke("merchant", mt, mc, "automatic-pending"); status != 202 || out["data"].(map[string]any)["state"] != "pending" {
		t.Fatal(status, out)
	}
	if _, e = pool.Exec(ctx, "UPDATE jobs SET state='succeeded' WHERE event_key=$1", "notify:"+o.TradeNo); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan int, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			status, _ := invoke("admin", at, ac, fmt.Sprint("concurrent-", i))
			results <- status
		}(i)
	}
	wg.Wait()
	close(results)
	for status := range results {
		if status != 202 {
			t.Fatalf("concurrent resend: %d", status)
		}
	}
	var count int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE kind='payment.notify' AND payload->>'trade_no'=$1", o.TradeNo).Scan(&count); e != nil || count != 2 {
		t.Fatal("resend multiplied jobs", count, e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE resource='orders' AND operation='resend-notification' AND resource_id=$1", o.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("enqueue audit not atomic", count, e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM journals").Scan(&count); e != nil || count != 1 {
		t.Fatal("resend changed ledger", count, e)
	}
	q := jobs.Queue{Pool: pool}
	j, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = (notification.Handler{Store: s.Store, Client: merchant.Client()}).Handle(ctx, j); e != nil {
		t.Fatal(e)
	}
	if e = q.Complete(ctx, j); e != nil {
		t.Fatal(e)
	}
	if deliveries.Load() != 1 {
		t.Fatal("durable resend not delivered once", deliveries.Load())
	}
	if status, out := invoke("admin", at, ac, "concurrent-0"); status != 200 || out["data"].(map[string]any)["state"] != "succeeded" {
		t.Fatal("idempotency lost after completion", status, out)
	}
	if status, _ := invoke("admin", at, ac, "too-soon"); status != 429 {
		t.Fatalf("completed resend ignored cooldown: %d", status)
	}
	if _, e = pool.Exec(ctx, "UPDATE notification_send_controls SET last_manual_at=statement_timestamp()-interval '2 minutes'"); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "ALTER TABLE backoffice_audit RENAME TO notification_test_audit_hidden"); e != nil {
		t.Fatal(e)
	}
	if status, _ := invoke("admin", at, ac, "audit-failure"); status != 500 {
		t.Fatalf("audit failure allowed resend: %d", status)
	}
	if _, e = pool.Exec(ctx, "ALTER TABLE notification_test_audit_hidden RENAME TO backoffice_audit"); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE kind='payment.notify'").Scan(&count); e != nil || count != 2 {
		t.Fatal("audit failure leaked queued delivery", count, e)
	}
	// A different merchant gets the same not-found response as an absent order.
	if _, e = pool.Exec(ctx, "UPDATE console_users SET subject_id=2 WHERE username='merchant' AND role='merchant'"); e != nil {
		t.Fatal(e)
	}
	other, cs, _ := s.Sessions.Create(identity.User{Username: "merchant", ID: 2, Role: "merchant"})
	if status, _ := invoke("merchant", other, cs, "foreign"); status != 404 {
		t.Fatalf("other merchant notified order: %d", status)
	}
}
