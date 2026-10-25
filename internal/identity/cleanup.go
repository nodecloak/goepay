package identity

import "time"

// CleanupExpired counts or removes only sessions already expired at cutoff.
func (s *Sessions) CleanupExpired(cutoff time.Time, remove bool) int {
	if s.pool != nil {
		return s.cleanupPersistent(cutoff, remove)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for key, v := range s.items {
		if !v.Expires.After(cutoff) {
			n++
			if remove {
				delete(s.items, key)
			}
		}
	}
	return n
}
