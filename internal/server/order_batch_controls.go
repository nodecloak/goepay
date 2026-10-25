package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

type orderBatchItem struct {
	TradeNo          string `json:"trade_no"`
	ExpectedRevision string `json:"expected_revision"`
}
type orderBatchInput struct {
	ExternalNo string           `json:"external_no"`
	Action     string           `json:"action"`
	Reason     string           `json:"reason"`
	Items      []orderBatchItem `json:"items"`
}
type orderBatchResultItem struct {
	TradeNo string            `json:"trade_no"`
	Control orderAdminControl `json:"control"`
}
type orderBatchResult struct {
	Items    []orderBatchResultItem `json:"items"`
	Replayed bool                   `json:"replayed"`
}

func (s *Server) orderBatchControlsConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 3 || parts[1] != "orders" || parts[2] != "batch-control" {
		return false
	}
	if role != "admin" || session.Issuer != nil || (session.User.AdminClass != "super" && session.User.AdminClass != "operator") {
		response(w, 403, -1, "own administrator session required", nil)
		return true
	}
	if len(parts) != 3 || r.URL.RawQuery != "" || r.Method != "POST" {
		response(w, 405, -1, "batch POST required", nil)
		return true
	}
	var in orderBatchInput
	if !decode(w, r, &in) {
		return true
	}
	switch in.Action {
	case "mark-paid", "mark-unpaid", "archive", "restore":
	default:
		response(w, 422, -1, "unsupported batch control", nil)
		return true
	}
	if len(in.Items) < 1 || len(in.Items) > 100 || len(in.ExternalNo) < 8 || len(in.ExternalNo) > 96 {
		response(w, 422, -1, "select one to one hundred orders and a stable request number", nil)
		return true
	}
	seen := map[string]bool{}
	for _, item := range in.Items {
		check := orderAdminInput{ExternalNo: in.ExternalNo, Reason: in.Reason, ExpectedRevision: item.ExpectedRevision}
		if !validOrderAdminInput(&check) || item.TradeNo == "" || len(item.TradeNo) > 64 || strings.ContainsAny(item.TradeNo, "\x00\r\n") || seen[item.TradeNo] {
			response(w, 422, -1, "invalid or duplicate selected order", nil)
			return true
		}
		seen[item.TradeNo] = true
		in.Reason = check.Reason
	}
	sort.Slice(in.Items, func(i, j int) bool { return in.Items[i].TradeNo < in.Items[j].TradeNo })
	out, e := s.applyOrderBatchControl(r.Context(), session.User, in)
	if e != nil {
		s.fail(w, e)
	} else {
		response(w, 200, 0, "batch backoffice controls saved atomically; payment facts retained", out)
	}
	return true
}
func (s *Server) applyOrderBatchControl(ctx context.Context, actor identity.User, in orderBatchInput) (orderBatchResult, error) {
	var out orderBatchResult
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/order-batch/%d/%s/%s", actor.ID, actor.Username, in.ExternalNo)); e != nil {
		return out, e
	}
	raw, _ := json.Marshal(in)
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	var prior []byte
	e = tx.QueryRow(ctx, `SELECT changes FROM backoffice_audit WHERE actor_role='admin' AND actor_id=$1 AND actor_name=$2 AND resource='order-batches' AND operation='control' AND changes->>'external_no'=$3 ORDER BY id LIMIT 1`, actor.ID, actor.Username, in.ExternalNo).Scan(&prior)
	if e == nil {
		var saved struct {
			Hash   string           `json:"request_hash"`
			Result orderBatchResult `json:"result"`
		}
		if json.Unmarshal(prior, &saved) != nil || saved.Hash != hash {
			return out, payment.ErrConflict
		}
		out = saved.Result
		out.Replayed = true
		return out, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	trades := make([]string, len(in.Items))
	for i, item := range in.Items {
		trades[i] = item.TradeNo
	}
	// Consistent ordering across batches and single-order controls avoids lock
	// inversion; no partial batch survives a stale row or audit failure.
	rows, e := tx.Query(ctx, `SELECT id,trade_no FROM orders WHERE trade_no=ANY($1::text[]) ORDER BY id FOR UPDATE`, trades)
	if e != nil {
		return out, e
	}
	ids := map[string]int64{}
	for rows.Next() {
		var id int64
		var trade string
		if e = rows.Scan(&id, &trade); e != nil {
			rows.Close()
			return out, e
		}
		ids[trade] = id
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(ids) != len(in.Items) {
		return out, payment.ErrNotFound
	}
	for _, item := range in.Items {
		control, e := s.applyOrderAdminControlTx(ctx, tx, ids[item.TradeNo], actor, in.Action, orderAdminInput{ExternalNo: "batch:" + in.ExternalNo, Reason: in.Reason, ExpectedRevision: item.ExpectedRevision})
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, orderBatchResultItem{TradeNo: item.TradeNo, Control: control})
	}
	changes, _ := json.Marshal(map[string]any{"external_no": in.ExternalNo, "request_hash": hash, "action": in.Action, "result": out})
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'order-batches',0,'control',$3)`, actor.ID, actor.Username, changes); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
