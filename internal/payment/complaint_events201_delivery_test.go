package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	provider "goepay/internal/adapters/payment"
	"goepay/internal/platform/jobs"
	"goepay/internal/refundevent201"
)

func complaintEventJobs(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if e := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE kind='merchant.complaint-event'`).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}

// subscribeComplaints saves a merchant subscription with the given event types
// directly through the sealed store (the HTTP/session guard is tested in the
// server package); it returns nothing but leaves an active subscription.
func subscribeComplaints(t *testing.T, s *Store, mid int64, endpoint string, types []string) {
	t.Helper()
	ctx := context.Background()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	store := s.refundEvents201()
	view, e := store.ViewTx(ctx, tx, mid)
	if e != nil {
		t.Fatal("view", e)
	}
	if _, e = store.UpdateTx(ctx, tx, mid, endpoint, types, view.Fingerprint); e != nil {
		t.Fatal("subscribe", e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
}

// syncComplaint upserts one provider complaint linked to the merchant's order,
// at the given provider status, exactly as ux-complain's sync does.
func syncComplaint(t *testing.T, s *Store, channelID int64, thirdID, tradeNo, status string) {
	t.Helper()
	if e := s.upsertComplaint133(context.Background(), channelID, provider.ComplaintRecord{
		ThirdID: thirdID, PayType: "wxpay", Status: status, Times: 1, TradeNos: []string{tradeNo},
	}, &flowProvider{}); e != nil {
		t.Fatal("sync complaint", e)
	}
}

func complaintTransport(f func(*http.Request) (*http.Response, error)) *http.Client {
	return &http.Client{Transport: roundTripFunc(f), Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (r roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return r(req) }

func complaintResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestComplaintEvent201NewComplaintDeliversOnceAndReSyncDeliversNothing(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 30201, 0, "platform")
	o := complaintVerifiedPaid231(t, s, 30201, "complaint-order-1", &flowProvider{})
	subscribeComplaints(t, s, 30201, "https://events.example.invalid/complain201", []string{"complain"})
	key, _ := rsa.GenerateKey(rand.Reader, 2048)

	// A new complaint enqueues exactly one event.
	syncComplaint(t, s, 7, "CMP-201-A", o.TradeNo, "PENDING")
	if got := complaintEventJobs(t, s); got != 1 {
		t.Fatalf("new complaint did not enqueue exactly one event: %d", got)
	}
	// A re-sync at the SAME state enqueues nothing (idempotent per complaint+state).
	syncComplaint(t, s, 7, "CMP-201-A", o.TradeNo, "PENDING")
	if got := complaintEventJobs(t, s); got != 1 {
		t.Fatalf("re-sync at same state enqueued a duplicate: %d", got)
	}

	// Deliver it: the webhook posts event=complain with the complaint content.
	j, e := (jobs.Queue{Pool: s.pool}).Claim(t.Context(), []string{refundevent201.ComplaintJobKind}, time.Minute)
	if e != nil {
		t.Fatal("no claimable complaint event", e)
	}
	calls := 0
	client := complaintTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(io.LimitReader(req.Body, 32<<10))
		if !strings.Contains(string(raw), "event=complain") || !strings.Contains(string(raw), "sign_type=RSA") || !strings.Contains(string(raw), "pid=30201") {
			t.Fatal("complaint webhook not the signed form", string(raw))
		}
		return complaintResponse(200, "success"), nil
	})
	if e = s.DeliverMerchantComplaintEvent201(t.Context(), j, key, client); e != nil {
		t.Fatal("deliver", e)
	}
	if e = (jobs.Queue{Pool: s.pool}).Complete(t.Context(), j); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatalf("complaint event not delivered exactly once: %d", calls)
	}
}

func TestComplaintEvent201StateChangeDeliversOnceMore(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 30202, 0, "platform")
	o := complaintVerifiedPaid231(t, s, 30202, "complaint-order-2", &flowProvider{})
	subscribeComplaints(t, s, 30202, "https://events.example.invalid/complain202", []string{"complain"})

	syncComplaint(t, s, 7, "CMP-202", o.TradeNo, "PENDING") // open
	if got := complaintEventJobs(t, s); got != 1 {
		t.Fatalf("open event missing: %d", got)
	}
	syncComplaint(t, s, 7, "CMP-202", o.TradeNo, "PROCESSING") // forward state change
	if got := complaintEventJobs(t, s); got != 2 {
		t.Fatalf("state change did not enqueue one more event: %d", got)
	}
	// A re-sync at the new state enqueues nothing.
	syncComplaint(t, s, 7, "CMP-202", o.TradeNo, "PROCESSING")
	if got := complaintEventJobs(t, s); got != 2 {
		t.Fatalf("re-sync at processing enqueued a duplicate: %d", got)
	}
}

func TestComplaintEvent201UnsubscribedMerchantGetsNothingDelivered(t *testing.T) {
	s := testStore(t)
	feeMerchant(t, s, 30203, 0, "platform")
	o := complaintVerifiedPaid231(t, s, 30203, "complaint-order-3", &flowProvider{})
	// No subscription saved for this merchant.
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	syncComplaint(t, s, 7, "CMP-203", o.TradeNo, "PENDING")
	// An event is still CAPTURED (so a later subscribe can begin fresh), but
	// delivery is skipped because the merchant has no active subscription.
	j, e := (jobs.Queue{Pool: s.pool}).Claim(t.Context(), []string{refundevent201.ComplaintJobKind}, time.Minute)
	if e != nil {
		t.Fatal("expected a captured event job even without a subscription", e)
	}
	calls := 0
	client := complaintTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return complaintResponse(200, "success"), nil
	})
	if e = s.DeliverMerchantComplaintEvent201(t.Context(), j, key, client); e != nil {
		t.Fatal("deliver", e)
	}
	if calls != 0 {
		t.Fatalf("unsubscribed merchant received a complaint webhook: %d calls", calls)
	}
}

func TestComplaintEvent201UnknownOwnerCapturesNothing(t *testing.T) {
	s := testStore(t)
	// A complaint whose trade_no matches no order has no owner, so no event is
	// captured (nothing to deliver to).
	if e := s.upsertComplaint133(context.Background(), 7, provider.ComplaintRecord{
		ThirdID: "CMP-204", PayType: "wxpay", Status: "PENDING", Times: 1, TradeNos: []string{"no-such-order-204"},
	}); e != nil {
		t.Fatal(e)
	}
	if got := complaintEventJobs(t, s); got != 0 {
		t.Fatalf("unknown-owner complaint enqueued an event: %d", got)
	}
}
