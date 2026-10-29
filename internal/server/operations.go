package server

import (
	"context"
	"encoding/json"
	"errors"
	"goepay/internal/identity"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// Passwords, cookies and request bodies are never written to the login audit.
func (s *Server) recordLogin(r *http.Request, role, username string, success *bool, verified ...identity.User) {
	if s.Pool == nil {
		return
	}
	if len(username) > 128 {
		username = username[:128]
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = "unknown"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if *success && role == "merchant" && len(verified) == 1 && verified[0].Role == role && verified[0].Username == username && verified[0].ID > 0 && verified[0].AuthVersion > 0 {
		if err = s.recordVerifiedMerchantLogin(ctx, verified[0], ip); err != nil {
			slog.Error("login audit persistence failed")
		}
		return
	}
	if _, err = s.Pool.Exec(ctx, "INSERT INTO console_login_audit(username,role,ip,success) VALUES($1,$2,$3,$4)", username, role, ip, *success); err != nil {
		slog.Error("login audit persistence failed")
	}
}

func (s *Server) operations(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "operations" {
		return false
	}
	if role != "admin" {
		response(w, 403, -1, "administrator required", nil)
		return true
	}
	if len(parts) < 3 {
		response(w, 404, -1, "operation not found", nil)
		return true
	}
	name := parts[2]
	if name == "system-log" {
		s.systemLogConsole(w, r, session, parts)
		return true
	}
	if name == "runtime-log" {
		s.runtimeLogConsole(w, r, session, parts)
		return true
	}
	if name == "cleanup" && len(parts) == 3 {
		s.cleanup(w, r, session)
		return true
	}
	if name == "jobs" && len(parts) == 5 && parts[4] == "retry" && r.Method == "POST" {
		id, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil || id < 1 {
			response(w, 400, -1, "invalid job id", nil)
			return true
		}
		tx, err := s.Pool.Begin(r.Context())
		if err != nil {
			s.fail(w, err)
			return true
		}
		defer tx.Rollback(r.Context())
		var retryKind, retryTrade string
		if err = tx.QueryRow(r.Context(), "SELECT kind,COALESCE(payload->>'trade_no','') FROM jobs WHERE id=$1", id).Scan(&retryKind, &retryTrade); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				response(w, 409, -1, "known dead job required", nil)
			} else {
				s.fail(w, err)
			}
			return true
		}
		if retryKind == "payment.notify" {
			var orderID int64
			// Match the resend lock order: original order first, job second.
			if err = tx.QueryRow(r.Context(), "SELECT id FROM orders WHERE trade_no=$1 AND status='paid' AND notify_url<>'' FOR UPDATE", retryTrade).Scan(&orderID); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					response(w, 409, -1, "paid notification order required", nil)
				} else {
					s.fail(w, err)
				}
				return true
			}
			var active bool
			if err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM jobs WHERE kind='payment.notify' AND payload->>'trade_no'=$1 AND state IN ('pending','running'))", retryTrade).Scan(&active); err != nil {
				s.fail(w, err)
				return true
			}
			if active {
				response(w, 409, -1, "notification already has an active delivery", nil)
				return true
			}
			permitted, reserveErr := reserveManualNotification(r.Context(), tx, orderID)
			if reserveErr != nil {
				s.fail(w, reserveErr)
				return true
			}
			if !permitted {
				w.Header().Set("Retry-After", "60")
				response(w, 429, -1, "notification resend cooldown", nil)
				return true
			}
		}
		tag, err := tx.Exec(r.Context(), "UPDATE jobs SET state='pending',attempts=0,run_at=now(),lease_until=NULL,lease_token=NULL,last_error='' WHERE id=$1 AND state='dead' AND kind IN ('payment.notify','payment.reconcile','refund.reconcile','transfer.reconcile','transfer.bank_reconcile','authorization.release.reconcile')", id)
		if err != nil {
			s.fail(w, err)
			return true
		}
		if tag.RowsAffected() != 1 {
			response(w, 409, -1, "only known dead notification or query jobs can be retried", nil)
			return true
		}
		_, err = tx.Exec(r.Context(), "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation) VALUES('admin',$1,$2,'jobs',$3,'retry')", session.User.ID, session.User.Username, id)
		if err == nil {
			err = tx.Commit(r.Context())
		}
		if err != nil {
			s.fail(w, err)
			return true
		}
		response(w, 200, 0, "job scheduled", nil)
		return true
	}
	if r.Method != "GET" || len(parts) != 3 {
		response(w, 405, -1, "GET required", nil)
		return true
	}
	if name == "system-info" {
		items := []map[string]any{{"name": "Go runtime", "value": runtime.Version(), "description": "当前构建运行时"}, {"name": "mode", "value": map[bool]string{true: "local sandbox", false: "restricted"}[s.Sandbox], "description": "资金测试环境"}, {"name": "database", "value": "PostgreSQL", "description": "不可变复式账本"}}
		if s.ProviderFlow != nil {
			for id, p := range s.ProviderFlow.Providers {
				items = append(items, map[string]any{"name": p.ID(), "value": id, "description": p.Capabilities()})
			}
		}
		response(w, 200, 0, "", map[string]any{"items": items, "total": len(items)})
		return true
	}
	page, limit := 1, 20
	for key, target := range map[string]*int{"page": &page, "limit": &limit} {
		if raw := r.URL.Query().Get(key); raw != "" {
			v, e := strconv.Atoi(raw)
			if e != nil || v < 1 || v > 1000000 {
				response(w, 400, -1, "invalid pagination", nil)
				return true
			}
			*target = v
		}
	}
	if limit > 100 {
		response(w, 400, -1, "limit exceeds 100", nil)
		return true
	}
	var from, columns string
	switch name {
	case "login-log":
		from = "console_login_audit"
		columns = "id::text,username,role,ip,success,created_at"
	case "audit-log":
		from = "backoffice_audit"
		columns = "id::text,actor_name AS actor,resource,operation AS action,resource_id::text AS target_id,created_at"
	case "jobs":
		from = "jobs"
		columns = "id::text,kind,state AS status,attempts,last_error,run_at AS next_run_at"
	default:
		response(w, 404, -1, "operation not found", nil)
		return true
	}
	var total int64
	if err := s.Pool.QueryRow(r.Context(), "SELECT count(*) FROM "+from).Scan(&total); err != nil {
		s.fail(w, err)
		return true
	}
	rows, err := s.Pool.Query(r.Context(), "SELECT "+columns+" FROM "+from+" ORDER BY id DESC LIMIT $1 OFFSET $2", limit, (page-1)*limit)
	if err != nil {
		s.fail(w, err)
		return true
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		values, e := rows.Values()
		if e != nil {
			s.fail(w, e)
			return true
		}
		item := map[string]any{}
		for i, f := range rows.FieldDescriptions() {
			item[f.Name] = values[i]
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		s.fail(w, err)
		return true
	}
	response(w, 200, 0, "", map[string]any{"items": items, "total": total, "page": page, "limit": limit})
	return true
}

func (s *Server) cleanup(w http.ResponseWriter, r *http.Request, session identity.Session) {
	if r.Method == "GET" {
		response(w, 200, 0, "only expired sessions, login throttle entries and login-audit rows are eligible; financial records are immutable", map[string]any{"cutoff": time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)})
		return
	}
	if r.Method != "POST" {
		response(w, 405, -1, "POST required", nil)
		return
	}
	var in struct {
		Preview *bool  `json:"preview"`
		Cutoff  string `json:"cutoff"`
	}
	if !decode(w, r, &in) {
		return
	}
	cutoff, err := time.Parse(time.RFC3339, in.Cutoff)
	if err != nil || in.Preview == nil || cutoff.After(time.Now().Add(-24*time.Hour)) || cutoff.Before(time.Now().AddDate(-1, 0, 0)) {
		response(w, 400, -1, "cutoff must be 1 to 365 days ago", nil)
		return
	}
	limits := s.sharedRequestLimits()
	limits.mu.Lock()
	count := 0
	for _, v := range limits.attempts {
		if !v.since.After(cutoff) {
			count++
		}
	}
	limits.mu.Unlock()
	var throttleCount, loginAuditCount int64
	if err = s.Pool.QueryRow(r.Context(), "SELECT count(*) FROM console_login_throttle WHERE updated_at<=$1", cutoff).Scan(&throttleCount); err != nil {
		s.fail(w, err)
		return
	}
	// console_login_audit grows unbounded and carries no immutable financial
	// fact, so it is age-prunable here. Business tables (orders, settlements,
	// ledger, transfers, profit sharing) are deliberately never deleted.
	if err = s.Pool.QueryRow(r.Context(), "SELECT count(*) FROM console_login_audit WHERE created_at<=$1", cutoff).Scan(&loginAuditCount); err != nil {
		s.fail(w, err)
		return
	}
	if !*in.Preview {
		tx, beginErr := s.Pool.Begin(r.Context())
		if beginErr != nil {
			s.fail(w, beginErr)
			return
		}
		defer tx.Rollback(r.Context())
		changes, _ := json.Marshal(map[string]any{"cutoff": cutoff, "expired_attempts": count, "eligible_login_throttle_keys": throttleCount, "eligible_login_audit_rows": loginAuditCount})
		_, err = tx.Exec(r.Context(), "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'cleanup',0,'expired-cache',$3)", session.User.ID, session.User.Username, changes)
		if err == nil {
			deleted, deleteErr := tx.Exec(r.Context(), "DELETE FROM console_login_throttle WHERE updated_at<=$1", cutoff)
			err = deleteErr
			throttleCount = deleted.RowsAffected()
		}
		if err == nil {
			deletedAudit, auditErr := tx.Exec(r.Context(), "DELETE FROM console_login_audit WHERE created_at<=$1", cutoff)
			err = auditErr
			loginAuditCount = deletedAudit.RowsAffected()
		}
		if err == nil {
			err = tx.Commit(r.Context())
		}
		if err == nil {
			limits.mu.Lock()
			for k, v := range limits.attempts {
				if !v.since.After(cutoff) {
					delete(limits.attempts, k)
				}
			}
			limits.mu.Unlock()
		}
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	expired := s.Sessions.CleanupExpired(cutoff, !*in.Preview)
	if expired < 0 {
		response(w, 503, -1, "session cleanup unavailable", nil)
		return
	}
	response(w, 200, 0, "", map[string]any{"expired_sessions": expired, "expired_attempts": count, "expired_login_throttle_keys": throttleCount, "expired_login_audit_rows": loginAuditCount, "cutoff": in.Cutoff, "preview": *in.Preview})
}
