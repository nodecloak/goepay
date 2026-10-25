package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"goepay/internal/identity"
	"net/http"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
)

// The request records only an idempotency digest and a durable delivery job.
// The worker always uses the order's stored merchant and URL, never caller input.
func (s *Server) notificationResendConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) != 4 || parts[1] != "orders" || parts[3] != "resend-notification" {
		return false
	}
	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	var in struct {
		ExternalNo string `json:"external_no"`
	}
	if !decode(w, r, &in) {
		return true
	}
	if in.ExternalNo == "" || len(in.ExternalNo) > 128 || strings.TrimSpace(in.ExternalNo) != in.ExternalNo || strings.IndexFunc(in.ExternalNo, unicode.IsControl) >= 0 {
		response(w, 422, -1, "valid external_no required", nil)
		return true
	}
	tx, e := s.Pool.Begin(r.Context())
	if e != nil {
		s.fail(w, e)
		return true
	}
	defer tx.Rollback(r.Context())
	var id, mid int64
	var status, target string
	e = tx.QueryRow(r.Context(), "SELECT id,merchant_id,status,notify_url FROM orders WHERE trade_no=$1 FOR UPDATE", parts[2]).Scan(&id, &mid, &status, &target)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && role != "admin" && mid != session.User.ID {
		response(w, 404, -1, "order not found", nil)
		return true
	}
	if e != nil {
		s.fail(w, e)
		return true
	}
	if status != "paid" || target == "" {
		response(w, 409, -1, "paid order with stored notification URL required", nil)
		return true
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("goepay:notification-resend:v1\x00"+in.ExternalNo)))
	var jobID int64
	var jobState string
	respond := func() {
		code := 200
		if jobState == "pending" || jobState == "running" {
			code = 202
		}
		response(w, code, 0, "notification delivery status", map[string]any{"job_id": fmt.Sprint(jobID), "state": jobState})
	}
	e = tx.QueryRow(r.Context(), "SELECT j.id,j.state FROM notification_resend_requests n JOIN jobs j ON j.id=n.job_id WHERE n.order_id=$1 AND n.request_hash=$2", id, digest).Scan(&jobID, &jobState)
	if e == nil {
		if e = tx.Commit(r.Context()); e != nil {
			s.fail(w, e)
		} else {
			respond()
		}
		return true
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		s.fail(w, e)
		return true
	}
	// Coalesce with an automatic or manually queued delivery; never steal a lease.
	e = tx.QueryRow(r.Context(), "SELECT id,state FROM jobs WHERE kind='payment.notify' AND payload->>'trade_no'=$1 AND state IN ('pending','running') ORDER BY created_at DESC,id DESC LIMIT 1", parts[2]).Scan(&jobID, &jobState)
	created := false
	if errors.Is(e, pgx.ErrNoRows) {
		permitted, reserveErr := reserveManualNotification(r.Context(), tx, id)
		e = reserveErr
		if e != nil {
			s.fail(w, e)
			return true
		}
		if !permitted {
			w.Header().Set("Retry-After", "60")
			response(w, 429, -1, "notification resend cooldown", nil)
			return true
		}
		e = tx.QueryRow(r.Context(), "INSERT INTO jobs(event_key,kind,payload) VALUES($1,'payment.notify',jsonb_build_object('trade_no',$2::text)) RETURNING id,state", fmt.Sprintf("notify-manual:%d:%s", id, digest), parts[2]).Scan(&jobID, &jobState)
		created = true
	}
	if e != nil {
		s.fail(w, e)
		return true
	}
	_, e = tx.Exec(r.Context(), "INSERT INTO notification_resend_requests(order_id,request_hash,job_id,actor_role,actor_id) VALUES($1,$2,$3,$4,$5)", id, digest, jobID, role, session.User.ID)
	if e != nil {
		s.fail(w, e)
		return true
	}
	operation := "resend-notification-coalesced"
	if created {
		operation = "resend-notification"
	}
	_, e = tx.Exec(r.Context(), "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'orders',$4,$5,jsonb_build_object('trade_no',$6::text,'job_id',$7::text))", role, session.User.ID, session.User.Username, id, operation, parts[2], fmt.Sprint(jobID))
	if e != nil {
		s.fail(w, e)
		return true
	}
	if e = tx.Commit(r.Context()); e != nil {
		s.fail(w, e)
		return true
	}
	respond()
	return true
}

// Both manual entry points hold the original order lock before this reservation.
// A failed job/audit transaction rolls the cooldown back with the enqueue.
func reserveManualNotification(ctx context.Context, tx pgx.Tx, orderID int64) (bool, error) {
	tag, e := tx.Exec(ctx, "INSERT INTO notification_send_controls(order_id,last_manual_at) VALUES($1,statement_timestamp()) ON CONFLICT(order_id) DO UPDATE SET last_manual_at=statement_timestamp() WHERE notification_send_controls.last_manual_at<=statement_timestamp()-interval '60 seconds'", orderID)
	return tag.RowsAffected() == 1, e
}
