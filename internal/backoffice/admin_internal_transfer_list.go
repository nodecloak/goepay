package backoffice

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/ledger"
)

// serveAdminInternalTransferList is the admin 站内转账 read projection. The original
// console filtered by 发起方 (initiator uid) / 收款方 (recipient touid) / 状态 /
// 日期范围 and showed 来源 (管理员/商户), 备注/拒绝原因 and 完成时间. GOEPAY records
// the creating actor on the request's first ledger event and the review's reason
// on the decision event, so every column is derived from existing immutable rows.
// This path never participates in the write/review/authorization flow.
func (s *Service) serveAdminInternalTransferList(w http.ResponseWriter, r *http.Request, p Principal) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, ErrInvalid)
		return
	}
	allowed := map[string]bool{"page": true, "limit": true, "merchant_id": true, "recipient_id": true, "status": true, "from": true, "to": true}
	for key, values := range q {
		if !allowed[key] || len(values) != 1 || strings.ContainsAny(values[0], "\x00\r\n") {
			writeError(w, ErrInvalid)
			return
		}
	}
	page, limit := 1, 20
	for key, target := range map[string]*int{"page": &page, "limit": &limit} {
		if raw := q.Get(key); raw != "" {
			n, e := strconv.ParseInt(raw, 10, 64)
			max := int64(1000000)
			if key == "limit" {
				max = 100
			}
			if e != nil || n <= 0 || n > max || strconv.FormatInt(n, 10) != raw {
				writeError(w, ErrInvalid)
				return
			}
			*target = int(n)
		}
	}
	id := func(raw string) (int64, bool) {
		n, e := strconv.ParseInt(raw, 10, 64)
		return n, e == nil && n > 0 && strconv.FormatInt(n, 10) == raw
	}
	initiator, recipient := int64(0), int64(0)
	if raw := q.Get("merchant_id"); raw != "" {
		v, ok := id(raw)
		if !ok {
			writeError(w, ErrInvalid)
			return
		}
		initiator = v
	}
	if raw := q.Get("recipient_id"); raw != "" {
		v, ok := id(raw)
		if !ok {
			writeError(w, ErrInvalid)
			return
		}
		recipient = v
	}
	status := q.Get("status")
	if status != "" && status != "pending" && status != "approved" && status != "rejected" {
		writeError(w, ErrInvalid)
		return
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	var fromTime, toTime time.Time
	for _, key := range []string{"from", "to"} {
		if raw := q.Get(key); raw != "" {
			d, e := time.ParseInLocation("2006-01-02", raw, zone)
			if e != nil || d.Format("2006-01-02") != raw {
				writeError(w, ErrInvalid)
				return
			}
			if key == "from" {
				fromTime = d
			} else {
				toTime = d.AddDate(0, 0, 1)
			}
		}
	}
	if !fromTime.IsZero() && !toTime.IsZero() && fromTime.After(toTime) {
		writeError(w, ErrInvalid)
		return
	}
	tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	// $1 initiator, $2 recipient, $3 status, $4 from, $5 to — each 0/''/zero means
	// "unfiltered". created_at is compared half-open [from, to).
	const predicate = `r.kind='internal-transfers'
  AND ($1::bigint=0 OR r.merchant_id=$1)
  AND ($2::bigint=0 OR r.recipient_id=$2)
  AND ($3='' OR r.status=$3)
  AND ($4::timestamptz IS NULL OR r.created_at>=$4)
  AND ($5::timestamptz IS NULL OR r.created_at<$5)`
	var fromArg, toArg *time.Time
	if !fromTime.IsZero() {
		fromArg = &fromTime
	}
	if !toTime.IsZero() {
		toArg = &toTime
	}
	var total int64
	if err = tx.QueryRow(r.Context(), "SELECT count(*) FROM ledger_requests r WHERE "+predicate, initiator, recipient, status, fromArg, toArg).Scan(&total); err != nil {
		writeError(w, err)
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT r.id,r.merchant_id,r.recipient_id,r.amount,r.status,r.created_at,
  origin.actor,terminal.created_at,terminal.reason
  FROM ledger_requests r
  LEFT JOIN LATERAL (
   SELECT e.actor FROM ledger_request_events e WHERE e.request_id=r.id ORDER BY e.id ASC LIMIT 1
  ) origin ON true
  LEFT JOIN LATERAL (
   SELECT e.created_at,e.reason FROM ledger_request_events e
   WHERE e.request_id=r.id AND e.status=r.status AND r.status IN ('approved','rejected')
   ORDER BY e.id DESC LIMIT 1
  ) terminal ON true
  WHERE `+predicate+" ORDER BY r.id DESC LIMIT $6 OFFSET $7", initiator, recipient, status, fromArg, toArg, limit, (page-1)*limit)
	if err != nil {
		writeError(w, err)
		return
	}
	items := []map[string]any{}
	for rows.Next() {
		var rid, from, to, amount int64
		var state string
		var created time.Time
		var actor, reason *string
		var completed *time.Time
		if err = rows.Scan(&rid, &from, &to, &amount, &state, &created, &actor, &completed, &reason); err != nil {
			rows.Close()
			writeError(w, err)
			return
		}
		// 来源: the creating actor's role prefix. admin:... → 管理员, merchant:... →
		// 商户; an unrecognised or absent actor is left unknown rather than guessed.
		origin := ""
		if actor != nil {
			if strings.HasPrefix(*actor, "admin:") {
				origin = "admin"
			} else if strings.HasPrefix(*actor, "merchant:") {
				origin = "merchant"
			}
		}
		items = append(items, map[string]any{
			"id":           strconv.FormatInt(rid, 10),
			"merchant_id":  strconv.FormatInt(from, 10),
			"recipient_id": strconv.FormatInt(to, 10),
			"amount":       ledger.Format(amount),
			"status":       state,
			"created_at":   created,
			"completed_at": completed,
			"origin":       origin,
			"reason":       reason,
		})
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
