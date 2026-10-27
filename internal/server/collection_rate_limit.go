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
	limits := s.sharedRequestLimits()
	ip, _, e := net.SplitHostPort(remote)
	if e != nil {
		ip = remote
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(ip+"\x00"+id)))
	limits.mu.Lock()
	defer limits.mu.Unlock()
	if limits.collectionAttempts == nil {
		limits.collectionAttempts = map[string]attempt{}
	}
	if limits.lastCollectionCleanup.IsZero() || now.Sub(limits.lastCollectionCleanup) >= time.Minute {
		for k, a := range limits.collectionAttempts {
			if now.Sub(a.since) >= time.Minute {
				delete(limits.collectionAttempts, k)
			}
		}
		limits.lastCollectionCleanup = now
	}
	a, exists := limits.collectionAttempts[key]
	if !exists && len(limits.collectionAttempts) >= 10000 {
		return false
	}
	if now.Sub(a.since) >= time.Minute {
		a = attempt{since: now}
	}
	a.count++
	limits.collectionAttempts[key] = a
	return a.count <= 120
}
