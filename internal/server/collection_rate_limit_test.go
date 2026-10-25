package server

import (
	"testing"
	"time"
)

func TestCollectionBudgetIsBoundedAndIndependentOfLogin(t *testing.T) {
	s := &Server{}
	now := time.Now()
	for i := 0; i < 120; i++ {
		if !s.allowCollectionAttempt("127.0.0.1:1234", "1", now) {
			t.Fatalf("early limit %d", i)
		}
	}
	if s.allowCollectionAttempt("127.0.0.1:9999", "1", now) {
		t.Fatal("source port bypassed limit")
	}
	if !s.allowCollectionAttempt("127.0.0.2:1234", "1", now) || !s.allowCollectionAttempt("127.0.0.1:1234", "2", now) {
		t.Fatal("unrelated source blocked")
	}
	if !s.allowCollectionAttempt("127.0.0.1:1234", "1", now.Add(time.Minute)) {
		t.Fatal("budget did not reset")
	}
	for i := 0; i < 10; i++ {
		if !s.allowLogin("127.0.0.1:1234", "merchant", "merchant", now) {
			t.Fatal("collection consumed login budget")
		}
	}
	if s.allowLogin("127.0.0.1:1234", "merchant", "merchant", now) {
		t.Fatal("login protection weakened")
	}
}
