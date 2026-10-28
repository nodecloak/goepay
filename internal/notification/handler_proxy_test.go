package notification

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
)

type planFunc func(context.Context) (Delivery, error)

func (f planFunc) DeliveryPlan(ctx context.Context) (Delivery, error) { return f(ctx) }

type apiFunc func(context.Context, string, string, string) (int, string, error)

func (f apiFunc) Deliver(ctx context.Context, a, k, t string) (int, string, error) {
	return f(ctx, a, k, t)
}

func notifyJob() jobs.Job {
	return jobs.Job{Kind: "payment.notify", Payload: []byte(`{"trade_no":"123"}`)}
}
func paidRepo(notifyURL string) fakeRepo {
	return fakeRepo{o: payment.Order{TradeNo: "123", MerchantID: 1, OutTradeNo: "m1", Type: "alipay", Name: "g", Amount: 100, NotifyURL: notifyURL, Status: "paid", Version: "v1"}, m: payment.Merchant{ID: 1, APIKey: "secret"}}
}

func TestNotifyModeOneSendsThroughTheProxyClient(t *testing.T) {
	var hits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		io.WriteString(w, "success")
	}))
	defer target.Close()
	// The proxy "client" is just a distinct client the planner chose; the
	// handler must use it instead of the direct one.
	proxied := target.Client()
	direct := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("mode 1 used the direct client")
		return nil, nil
	})}
	h := Handler{Store: paidRepo(target.URL), Client: direct, Planner: planFunc(func(context.Context) (Delivery, error) {
		return Delivery{Mode: 1, Client: proxied}, nil
	})}
	if e := h.Handle(context.Background(), notifyJob()); e != nil || hits != 1 {
		t.Fatalf("mode 1 delivery: hits=%d err=%v", hits, e)
	}
}

func TestNotifyModeTwoPostsToRelayAndUsesRelayAck(t *testing.T) {
	var gotTarget, gotKey string
	h := Handler{Store: paidRepo("https://merchant.example/notify"), Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("mode 2 dialed the merchant directly")
		return nil, nil
	})}, Planner: planFunc(func(context.Context) (Delivery, error) {
		return Delivery{Mode: 2, APIURL: "https://relay.example/api", APIKey: "k207", API: apiFunc(func(_ context.Context, a, k, target string) (int, string, error) {
			gotKey, gotTarget = k, target
			return 200, `{"r":"SUCCESS"}`, nil
		})}, nil
	})}
	if e := h.Handle(context.Background(), notifyJob()); e != nil {
		t.Fatalf("mode 2 delivery: %v", e)
	}
	if gotKey != "k207" || gotTarget == "" {
		t.Fatalf("relay call wrong: key=%q target=%q", gotKey, gotTarget)
	}
	// A relay body with no success marker is not acknowledged.
	h.Planner = planFunc(func(context.Context) (Delivery, error) {
		return Delivery{Mode: 2, APIURL: "https://relay.example/api", APIKey: "k", API: apiFunc(func(context.Context, string, string, string) (int, string, error) {
			return 200, "pending", nil
		})}, nil
	})
	if e := h.Handle(context.Background(), notifyJob()); e == nil {
		t.Fatal("mode 2 accepted a non-success relay body")
	}
}

func TestNotifyProxyPlanErrorFailsClosedNeverDirect(t *testing.T) {
	direct := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("a failed proxy plan fell back to a direct send")
		return nil, nil
	})}
	h := Handler{Store: paidRepo("https://merchant.example/notify"), Client: direct, Planner: planFunc(func(context.Context) (Delivery, error) {
		return Delivery{}, errors.New("secret unavailable")
	})}
	e := h.Handle(context.Background(), notifyJob())
	if e == nil {
		t.Fatal("configured proxy that could not resolve was silently bypassed")
	}
	if code, ok := FailureCode(e, notifyJob()); !ok || code != "proxy" {
		t.Fatalf("expected a proxy delivery failure, got %v (%q,%v)", e, code, ok)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
