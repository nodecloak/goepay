package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
)

const consoleOrderControlJoin = ` LEFT JOIN LATERAL (SELECT a.id,a.changes FROM backoffice_audit a WHERE a.actor_role='admin' AND a.resource='orders' AND a.resource_id=o.id AND a.operation='console-order-control' ORDER BY a.id DESC LIMIT 1) console_control ON true`
const consoleAdminStatusExpression = "COALESCE(NULLIF(console_control.changes->>'status',''),o.status)"

type orderAdminControl struct {
	Status   string `json:"status"`
	Archived bool   `json:"archived"`
	Revision string `json:"revision"`
}
type orderAdminInput struct {
	Status           string `json:"status,omitempty"`
	ExternalNo       string `json:"external_no"`
	Reason           string `json:"reason"`
	ExpectedRevision string `json:"expected_revision"`
}
type orderControlReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readOrderAdminControl(ctx context.Context, q orderControlReader, orderID int64) (orderAdminControl, error) {
	control := orderAdminControl{Revision: "0"}
	var id int64
	var raw []byte
	e := q.QueryRow(ctx, `SELECT id,changes FROM backoffice_audit WHERE actor_role='admin' AND resource='orders' AND resource_id=$1 AND operation='console-order-control' ORDER BY id DESC LIMIT 1`, orderID).Scan(&id, &raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return control, nil
	}
	if e != nil {
		return control, e
	}
	var stored struct {
		Status   string `json:"status"`
		Archived bool   `json:"archived"`
	}
	if e = json.Unmarshal(raw, &stored); e != nil {
		return control, e
	}
	if stored.Status != "" && stored.Status != "paid" && stored.Status != "pending" {
		return control, payment.ErrState
	}
	return orderAdminControl{Status: stored.Status, Archived: stored.Archived, Revision: strconv.FormatInt(id, 10)}, nil
}
func validOrderAdminInput(in *orderAdminInput) bool {
	in.Reason = strings.TrimSpace(in.Reason)
	revision, e := strconv.ParseInt(in.ExpectedRevision, 10, 64)
	return e == nil && revision >= 0 && strconv.FormatInt(revision, 10) == in.ExpectedRevision && in.ExternalNo != "" && len(in.ExternalNo) <= 128 && !strings.ContainsAny(in.ExternalNo, "\x00\r\n") && in.Reason != "" && len(in.Reason) <= 240 && !strings.ContainsAny(in.Reason, "\x00\r\n")
}
func (s *Server) applyOrderAdminControl(ctx context.Context, orderID int64, actor identity.User, action string, in orderAdminInput) (orderAdminControl, error) {
	var zero orderAdminControl
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return zero, e
	}
	defer tx.Rollback(ctx)
	control, e := s.applyOrderAdminControlTx(ctx, tx, orderID, actor, action, in)
	if e != nil {
		return zero, e
	}
	return control, tx.Commit(ctx)
}
func (s *Server) applyOrderAdminControlTx(ctx context.Context, tx pgx.Tx, orderID int64, actor identity.User, action string, in orderAdminInput) (orderAdminControl, error) {
	var zero orderAdminControl
	var e error
	var locked int64
	if e = tx.QueryRow(ctx, "SELECT id FROM orders WHERE id=$1 FOR UPDATE", orderID).Scan(&locked); e != nil {
		return zero, e
	}
	control, e := readOrderAdminControl(ctx, tx, orderID)
	if e != nil {
		return zero, e
	}
	raw, _ := json.Marshal(struct {
		Action string
		Input  orderAdminInput
	}{action, in})
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	var prior string
	e = tx.QueryRow(ctx, `SELECT changes->>'request_hash' FROM backoffice_audit WHERE actor_role='admin' AND resource='orders' AND resource_id=$1 AND operation='console-order-control' AND changes->>'external_no'=$2 ORDER BY id LIMIT 1`, orderID, in.ExternalNo).Scan(&prior)
	if e == nil {
		if prior != hash {
			return zero, payment.ErrConflict
		}
		return control, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return zero, e
	}
	if in.ExpectedRevision != control.Revision {
		return zero, payment.ErrConflict
	}
	switch action {
	case "mark-paid":
		control.Status = "paid"
	case "mark-unpaid":
		control.Status = "pending"
	case "archive":
		control.Archived = true
	case "restore":
		control.Archived = false
	case "auto":
		control.Status = ""
	default:
		return zero, payment.ErrInvalid
	}
	payload, e := json.Marshal(map[string]any{"action": action, "external_no": in.ExternalNo, "reason": in.Reason, "request_hash": hash, "status": control.Status, "archived": control.Archived, "previous_revision": in.ExpectedRevision})
	if e != nil {
		return zero, e
	}
	var id int64
	if e = tx.QueryRow(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'orders',$3,'console-order-control',$4) RETURNING id`, actor.ID, actor.Username, orderID, payload).Scan(&id); e != nil {
		return zero, e
	}
	control.Revision = strconv.FormatInt(id, 10)
	return control, nil
}

func (s *Server) orderAdminActions(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) != 4 || parts[1] != "orders" {
		return false
	}
	action := parts[3]
	switch action {
	case "admin-status", "archive", "restore", "manual-refund", "manual-complete":
	default:
		return false
	}
	if role != "admin" || session.Issuer != nil || (session.User.AdminClass != "super" && session.User.AdminClass != "operator") {
		response(w, 403, -1, "original administrator required", nil)
		return true
	}
	if action == "manual-refund" && session.User.AdminClass != "super" {
		response(w, 403, -1, "super administrator required for manual refund", nil)
		return true
	}
	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if r.URL.RawQuery != "" {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "order service unavailable", nil)
		return true
	}
	o, e := s.Store.OrderByTradeNo(r.Context(), parts[2])
	if e != nil {
		s.fail(w, e)
		return true
	}
	if action == "manual-refund" {
		var in struct {
			Amount           string `json:"amount"`
			ExternalNo       string `json:"external_no"`
			Reason           string `json:"reason"`
			OfflineConfirmed bool   `json:"offline_confirmed"`
		}
		if !decode(w, r, &in) {
			return true
		}
		amount, err := ledger.Parse(in.Amount)
		if err != nil || amount <= 0 {
			response(w, 422, -1, "valid refund amount required", nil)
			return true
		}
		refund, err := s.Store.ManualRefund(r.Context(), o.MerchantID, o.TradeNo, in.ExternalNo, amount, payment.IncomeHoldActor{ID: session.User.ID, Name: session.User.Username, Class: session.User.AdminClass}, in.Reason, in.OfflineConfirmed)
		if isFundsAuthorizationError(err) {
			fundsHTTPError(w, err)
		} else if err != nil {
			s.fail(w, err)
		} else {
			response(w, 200, 0, "offline refund recorded; no provider refund sent", map[string]any{"refund": refund, "offline": true, "provider_refund_sent": false})
		}
		return true
	}
	var in orderAdminInput
	if !decode(w, r, &in) {
		return true
	}
	if !validOrderAdminInput(&in) {
		response(w, 422, -1, "reason, external_no and canonical expected_revision required", nil)
		return true
	}
	if action == "admin-status" {
		if in.Status == "paid" {
			action = "mark-paid"
		} else if in.Status == "pending" {
			action = "mark-unpaid"
		} else {
			response(w, 422, -1, "paid or pending marker required", nil)
			return true
		}
	} else if in.Status != "" {
		response(w, 422, -1, "status not accepted for this action", nil)
		return true
	}
	if action == "manual-complete" {
		raw, _ := json.Marshal(map[string]string{"external_no": in.ExternalNo, "reason": in.Reason})
		if _, e = s.Pool.Exec(r.Context(), `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'orders',$3,'manual-complete-intent',$4)`, session.User.ID, session.User.Username, o.ID, raw); e != nil {
			s.fail(w, e)
			return true
		}
		if o.Status != "paid" {
			if s.ProviderFlow == nil {
				response(w, 503, -1, "original payment query unavailable", nil)
				return true
			}
			_, e = s.ProviderFlow.Query(r.Context(), o.MerchantID, o.TradeNo)
			if e == nil {
				o, e = s.Store.OrderByTradeNo(r.Context(), o.TradeNo)
			}
			if e != nil || o.Status != "paid" {
				response(w, 202, 0, "original payment is not verified paid; no completion recorded", map[string]any{"completed": false, "status": o.Status})
				return true
			}
		}
		action = "auto"
	}
	control, e := s.applyOrderAdminControl(r.Context(), o.ID, session.User, action, in)
	if e != nil {
		s.fail(w, e)
		return true
	}
	if action == "auto" {
		response(w, 200, 0, "original payment verified and posting recovered", map[string]any{"completed": true, "order": o, "admin_control": control})
	} else {
		response(w, 200, 0, "backoffice order control saved; payment facts retained", control)
	}
	return true
}
