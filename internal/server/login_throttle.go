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
	// One atomic statement keeps the same source-before-account ordering, but
	// never holds a hot counter row across client round trips. Rejected attempts
	// still count. Database failures remain fail-closed within the same budget.
	var allowed bool
	e = s.Pool.QueryRow(ctx, `WITH source_budget AS (
 INSERT INTO console_login_throttle(key_hash,window_start,attempts) VALUES($1,statement_timestamp(),1)
 ON CONFLICT(key_hash) DO UPDATE SET
 window_start=CASE WHEN console_login_throttle.window_start<=statement_timestamp()-interval '15 minutes' THEN statement_timestamp() ELSE console_login_throttle.window_start END,
 attempts=CASE WHEN console_login_throttle.window_start<=statement_timestamp()-interval '15 minutes' THEN 1 ELSE LEAST(console_login_throttle.attempts+1,301) END,
 updated_at=statement_timestamp() RETURNING attempts
), account_budget AS (
 INSERT INTO console_login_throttle(key_hash,window_start,attempts)
 SELECT $2,statement_timestamp(),1 FROM source_budget WHERE attempts<=300
 ON CONFLICT(key_hash) DO UPDATE SET
 window_start=CASE WHEN console_login_throttle.window_start<=statement_timestamp()-interval '15 minutes' THEN statement_timestamp() ELSE console_login_throttle.window_start END,
 attempts=CASE WHEN console_login_throttle.window_start<=statement_timestamp()-interval '15 minutes' THEN 1 ELSE LEAST(console_login_throttle.attempts+1,11) END,
 updated_at=statement_timestamp() RETURNING attempts
)
SELECT attempts<=300 AND COALESCE((SELECT attempts<=10 FROM account_budget),false) FROM source_budget`, source[:], account[:]).Scan(&allowed)
	return allowed, e
}
