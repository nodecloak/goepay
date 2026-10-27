package server

import (
	"context"
	"testing"
	"time"
)

func TestServer188ExecutionViewSharesOriginalRequestBudgets(t *testing.T) {
	f, _, _ := subchannel168BProductLocal(t)
	if out := f.pay(t, "request-limit188-original", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	view, err := f.s.paymentIntentServer(context.Background(), 1, "request-limit188-original", 7)
	if err != nil || view == f.s || view == nil || view.ProviderFlow.SubchannelID() == 0 {
		t.Fatal("actual original child execution not restored", err)
	}
	now := time.Now()
	for range 10 {
		if !f.s.allowLogin("198.51.100.188:1000", "admin", "limit188", now) {
			t.Fatal("original login budget changed")
		}
	}
	if view.allowLogin("198.51.100.188:2000", "admin", "LIMIT188", now) {
		t.Error("child execution split the original case-insensitive login budget")
	}
	for range 120 {
		if !f.s.allowCollectionAttempt("198.51.100.188:1000", "188", now) {
			t.Fatal("original payment budget changed")
		}
	}
	if view.allowCollectionAttempt("198.51.100.188:2000", "188", now) {
		t.Error("child execution split the original payment budget")
	}
	if !view.allowLogin("198.51.100.188:2000", "merchant", "limit188", now) {
		t.Error("role isolation changed")
	}
	if !view.allowCollectionAttempt("198.51.100.188:2000", "another188", now) {
		t.Error("checkout identity isolation changed")
	}
	if !view.allowCollectionAttempt("198.51.100.188:2000", "188", now.Add(time.Minute)) {
		t.Error("payment window no longer expires")
	}
	if !view.allowLogin("198.51.100.188:2000", "admin", "LIMIT188", now.Add(16*time.Minute)) {
		t.Error("login window no longer expires")
	}
}
