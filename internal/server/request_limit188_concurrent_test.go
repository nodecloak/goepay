package server

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestServer188ConcurrentViewsShareOneBoundedBudget(t *testing.T) {
	root := &Server{}
	views := []*Server{root, root.executionView(), root.executionView(), root.executionView()}
	var loginAccepted, collectionAccepted atomic.Int64
	now := time.Now()
	var group sync.WaitGroup
	for i := range 40 {
		group.Add(1)
		go func() {
			defer group.Done()
			s := views[i%len(views)]
			for range 20 {
				if s.allowLogin("198.51.100.188:188", "admin", "concurrent188", now) {
					loginAccepted.Add(1)
				}
				if s.allowCollectionAttempt("198.51.100.188:188", "concurrent188", now) {
					collectionAccepted.Add(1)
				}
			}
		}()
	}
	group.Wait()
	if loginAccepted.Load() != 10 || collectionAccepted.Load() != 120 {
		t.Fatal("execution views bypass original shared budgets", loginAccepted.Load(), collectionAccepted.Load())
	}
	if !root.allowLogin("198.51.100.189:188", "admin", "concurrent188", now) || !views[1].allowCollectionAttempt("198.51.100.188:188", "another188", now) {
		t.Fatal("unrelated client/checkout was blocked")
	}
}
