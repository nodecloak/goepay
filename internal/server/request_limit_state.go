package server

import (
	"sync"
	"time"
)

// Only initialization of the per-server pointer uses this lock. The actual
// bounded budgets and cleanup timestamps have their own shared state lock.
var requestLimitInitialization sync.Mutex

type requestLimitState struct {
	mu                    sync.Mutex
	attempts              map[string]attempt
	lastAttemptCleanup    time.Time
	collectionAttempts    map[string]attempt
	lastCollectionCleanup time.Time
}

func (s *Server) sharedRequestLimits() *requestLimitState {
	root := s
	for root.subchannelPolicySource != nil {
		root = root.subchannelPolicySource
	}
	requestLimitInitialization.Lock()
	defer requestLimitInitialization.Unlock()
	if root.limitState == nil {
		root.limitState = &requestLimitState{}
	}
	return root.limitState
}

// Payment views replace only execution material. Mutable request budgets stay
// on the original shared pointer; no mutex is copied with the server view.
func (s *Server) executionView() *Server {
	limits := s.sharedRequestLimits()
	view := *s
	view.limitState = limits
	if view.subchannelPolicySource == nil {
		view.subchannelPolicySource = s
	}
	return &view
}
