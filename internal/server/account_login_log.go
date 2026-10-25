package server

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

type accountLoginLogRow struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	IP        string    `json:"ip"`
	City      string    `json:"city"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) accountLoginLog(w http.ResponseWriter, r *http.Request, session identity.Session, parts []string) bool {
	if session.User.Role != "merchant" || session.User.ID <= 0 || session.Issuer != nil {
		response(w, 403, -1, "own merchant session required", nil)
		return true
	}
	if len(parts) != 3 {
		response(w, 404, -1, "not found", nil)
		return true
	}
	return s.merchantLoginLogPage(w, r, session.User.ID, false)
}

func (s *Server) adminMerchantLoginLog(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 4 || parts[1] != "merchants" || parts[3] != "login-log" {
		return false
	}
	if role != "admin" || session.User.Role != "admin" || session.User.ID <= 0 || session.Issuer != nil || (session.User.AdminClass != "operator" && session.User.AdminClass != "super") {
		response(w, 403, -1, "administrator session required", nil)
		return true
	}
	if len(parts) != 4 {
		response(w, 404, -1, "not found", nil)
		return true
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != parts[2] {
		response(w, 400, -1, "invalid merchant id", nil)
		return true
	}
	return s.merchantLoginLogPage(w, r, id, true)
}

func (s *Server) merchantLoginLogPage(w http.ResponseWriter, r *http.Request, merchantID int64, requireTarget bool) bool {
	if r.Method != "GET" {
		response(w, 405, -1, "GET required", nil)
		return true
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		response(w, 400, -1, "invalid login log query", nil)
		return true
	}
	page, limit := 1, 20
	for key, values := range query {
		if (key != "page" && key != "limit") || len(values) != 1 {
			response(w, 400, -1, "invalid login log query", nil)
			return true
		}
		n, e := strconv.Atoi(values[0])
		max := 100000
		if key == "limit" {
			max = 100
		}
		if e != nil || n < 1 || n > max || strconv.Itoa(n) != values[0] {
			response(w, 400, -1, "invalid login log pagination", nil)
			return true
		}
		if key == "page" {
			page = n
		} else {
			limit = n
		}
	}
	tx, err := s.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		s.fail(w, err)
		return true
	}
	defer tx.Rollback(r.Context())
	if requireTarget {
		var exists bool
		if err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM merchants WHERE id=$1)", merchantID).Scan(&exists); err != nil {
			s.fail(w, err)
			return true
		}
		if !exists {
			response(w, 404, -1, "merchant not found", nil)
			return true
		}
	}
	const owner = " FROM console_login_audit WHERE role='merchant' AND success AND subject_id=$1"
	var total int64
	if err = tx.QueryRow(r.Context(), "SELECT count(*)"+owner, merchantID).Scan(&total); err != nil {
		s.fail(w, err)
		return true
	}
	rows, err := tx.Query(r.Context(), "SELECT id::text,ip,created_at"+owner+" ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3", merchantID, limit, (page-1)*limit)
	if err != nil {
		s.fail(w, err)
		return true
	}
	items := []accountLoginLogRow{}
	for rows.Next() {
		var item accountLoginLogRow
		if err = rows.Scan(&item.ID, &item.IP, &item.CreatedAt); err != nil {
			break
		}
		items = append(items, item)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		s.fail(w, err)
		return true
	}
	// Existing audit facts have no authenticated method or geography. Never
	// infer them from username, headers, or an outbound IP lookup.
	response(w, 200, 0, "verified target successful logins only; unrecorded method/location remain empty", map[string]any{"items": items, "total": total, "page": page, "limit": limit})
	return true
}
