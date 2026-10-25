package server

import (
	"crypto/sha256"
	"fmt"
	"net"
	"time"
)

// Public payment traffic has its own bounded one-minute budget. Sharing the
// authentication map would make checkout retries consume login protection.
// Reverse-proxy headers are deliberately not trusted here.
func (s *Server) allowCollectionAttempt(remote, id string, now time.Time) bool {
	ip, _, e := net.SplitHostPort(remote)
	if e != nil {
		ip = remote
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(ip+"\x00"+id)))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.collectionAttempts == nil {
		s.collectionAttempts = map[string]attempt{}
	}
	if s.lastCollectionCleanup.IsZero() || now.Sub(s.lastCollectionCleanup) >= time.Minute {
		for k, a := range s.collectionAttempts {
			if now.Sub(a.since) >= time.Minute {
				delete(s.collectionAttempts, k)
			}
		}
		s.lastCollectionCleanup = now
	}
	a, exists := s.collectionAttempts[key]
	if !exists && len(s.collectionAttempts) >= 10000 {
		return false
	}
	if now.Sub(a.since) >= time.Minute {
		a = attempt{since: now}
	}
	a.count++
	s.collectionAttempts[key] = a
	return a.count <= 120
}
