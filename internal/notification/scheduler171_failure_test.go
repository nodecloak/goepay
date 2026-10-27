package notification

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
)

func TestScheduler171OnlyActualDeliveryErrorCanSupplyFailureCode(t *testing.T) {
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer sink.Close()
	repo := fakeRepo{o: payment.Order{TradeNo: "synthetic171", MerchantID: 1, OutTradeNo: "synthetic171-out", Amount: 100, NotifyURL: sink.URL, Status: "paid", Version: "v1"}, m: payment.Merchant{ID: 1, APIKey: "synthetic-only"}}
	h := Handler{Store: repo, Client: sink.Client()}
	j := jobs.Job{ID: 171, Kind: "payment.notify", EventKey: "notify:synthetic171", Payload: []byte(`{"trade_no":"synthetic171"}`), Attempts: 2, Token: "synthetic-original-lease"}
	e := h.Handle(context.Background(), j)
	if code, ok := FailureCode(e, j); e == nil || !ok || code != "ack" {
		t.Fatal("actual HTTP rejection not classified", code, ok, e)
	}
	for _, other := range []jobs.Job{{ID: 172, Kind: j.Kind, Payload: j.Payload, Attempts: j.Attempts, Token: j.Token}, {ID: j.ID, Kind: j.Kind, Payload: j.Payload, Attempts: j.Attempts, Token: "another"}, {ID: j.ID, Kind: j.Kind, Payload: j.Payload, Attempts: 3, Token: j.Token}, {ID: j.ID, Kind: j.Kind, Payload: []byte(`{"trade_no":"other"}`), Attempts: j.Attempts, Token: j.Token}} {
		if _, ok := FailureCode(e, other); ok {
			t.Fatal("failure adopted a different original job/lease")
		}
	}
	repo.o.Status = "created"
	h.Store = repo
	e = h.Handle(context.Background(), j)
	if _, ok := FailureCode(e, j); e == nil || ok {
		t.Fatal("non-delivery failure counted as merchant callback failure", e)
	}
}
