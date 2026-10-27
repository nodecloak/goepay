package backoffice

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/ledger"
)

// This is a merchant read projection only. Counterparty visibility does not
// participate in the write, review, cancellation or funds authorization paths.
func (s *Service) serveMerchantInternalTransferList(w http.ResponseWriter, r *http.Request, p Principal) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, ErrInvalid)
		return
	}
	for _, key := range []string{"direction", "status"} {
		values, present := q[key]
		if !present {
			continue
		}
		if len(values) != 1 {
			writeError(w, ErrInvalid)
			return
		}
		allowed := false
		if key == "direction" {
			allowed = values[0] == "in" || values[0] == "out"
		} else {
			allowed = values[0] == "pending" || values[0] == "approved" || values[0] == "rejected"
		}
		if !allowed {
			writeError(w, ErrInvalid)
			return
		}
	}
	direction, status := q.Get("direction"), q.Get("status")
	q.Del("direction")
	q.Del("status")
	scoped := r.Clone(r.Context())
	scopedURL := *r.URL
	scoped.URL = &scopedURL
	scoped.URL.RawQuery = q.Encode()
	page, limit, mid, err := workflowListFilters(scoped, p)
	if err != nil {
		writeError(w, err)
		return
	}
	tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	const predicate = `r.kind='internal-transfers'
  AND (r.merchant_id=$1 OR r.recipient_id=$1)
  AND ($2='' OR $2='out' AND r.merchant_id=$1 OR $2='in' AND r.recipient_id=$1)
  AND ($3='' OR r.status=$3)`
	var total int64
	if err = tx.QueryRow(r.Context(), "SELECT count(*) FROM ledger_requests r WHERE "+predicate, mid, direction, status).Scan(&total); err != nil {
		writeError(w, err)
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT r.id,
  CASE WHEN r.merchant_id=$1 THEN 'out' ELSE 'in' END,
  CASE WHEN r.merchant_id=$1 THEN r.recipient_id ELSE r.merchant_id END,
  r.amount,r.status,r.created_at,terminal.created_at
  FROM ledger_requests r LEFT JOIN LATERAL (
   SELECT e.created_at FROM ledger_request_events e
   WHERE e.request_id=r.id AND e.status=r.status AND r.status IN ('approved','rejected')
   ORDER BY e.id DESC LIMIT 1
  ) terminal ON true WHERE `+predicate+" ORDER BY r.id DESC LIMIT $4 OFFSET $5", mid, direction, status, limit, (page-1)*limit)
	if err != nil {
		writeError(w, err)
		return
	}
	items := []map[string]any{}
	for rows.Next() {
		var id, peer, amount int64
		var direction, state string
		var created time.Time
		var completed *time.Time
		if err = rows.Scan(&id, &direction, &peer, &amount, &state, &created, &completed); err != nil {
			rows.Close()
			writeError(w, err)
			return
		}
		// No original remarks are persisted by the current request writer. Do not
		// manufacture one from the administrator identity or private request key.
		items = append(items, map[string]any{"id": strconv.FormatInt(id, 10), "direction": direction,
			"peer_merchant_id": strconv.FormatInt(peer, 10), "amount": ledger.Format(amount),
			"status": state, "created_at": created, "completed_at": completed, "remark": nil})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		writeError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total, "page": page, "limit": limit}, "")
}
