package server

import (
	"context"
	"crypto/sha256"
	"net"
	"net/http"
	"strings"
	"time"
)

// At most four unauthenticated login flows in this process may perform the
// 64-MiB Argon2 calculation. Saturation is explicit; no unbounded wait queue.
var loginVerificationSlots = make(chan struct{}, 4)

func claimLoginVerification() bool {
	select {
	case loginVerificationSlots <- struct{}{}:
		return true
	default:
		return false
	}
}
func releaseLoginVerification() { <-loginVerificationSlots }

func (s *Server) allowLoginRequest(r *http.Request, role, username string) (bool, error) {
	if s.Pool == nil {
		return s.allowLogin(r.RemoteAddr, role, username, time.Now()), nil
	}
	ip, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		ip = r.RemoteAddr
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		ip = "invalid-remote"
	} else {
		ip = parsed.String()
	}
	// A broad source budget prevents rotating usernames from creating an
	// unbounded set of account counters. Account/source budgets remain separate,
	// so one exhausted account does not deny unrelated users behind a proxy.
	source := sha256.Sum256([]byte("goepay/login/source/v1\x00" + ip))
	account := sha256.Sum256([]byte("goepay/login/account-source/v1\x00" + ip + "\x00" + role + "\x00" + strings.ToLower(username)))
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(ctx)
	allowed := true
	// All replicas acquire source before account. Rejected attempts still count;
	// errors roll back and fail closed, never falling back to process memory.
	for _, budget := range []struct {
		key   [32]byte
		limit int
	}{{source, 300}, {account, 10}} {
		var count int
		e = tx.QueryRow(ctx, `INSERT INTO console_login_throttle(key_hash,window_start,attempts) VALUES($1,statement_timestamp(),1)
   ON CONFLICT(key_hash) DO UPDATE SET
    window_start=CASE WHEN console_login_throttle.window_start<=statement_timestamp()-interval '15 minutes' THEN statement_timestamp() ELSE console_login_throttle.window_start END,
    attempts=CASE WHEN console_login_throttle.window_start<=statement_timestamp()-interval '15 minutes' THEN 1 ELSE LEAST(console_login_throttle.attempts+1,$2::integer+1) END,
    updated_at=statement_timestamp() RETURNING attempts`, budget.key[:], budget.limit).Scan(&count)
		if e != nil {
			return false, e
		}
		if count > budget.limit {
			allowed = false
			break
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return false, e
	}
	return allowed, nil
}
