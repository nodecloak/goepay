package server

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"goepay/internal/identity"
)

func (s *Server) legacyRefundHistoryConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, p []string) bool {
	if len(p) < 2 || p[1] != "legacy-refund-history" {
		return false
	}
	if session.Issuer != nil || session.User.ID <= 0 || session.User.Role != role || session.User.Username == "" {
		response(w, 403, -1, "original session required", nil)
		return true
	}
	if len(p) != 2 {
		response(w, 404, -1, "historical refunds are read only", nil)
		return true
	}
	if r.Method != http.MethodGet {
		response(w, 405, -1, "historical refunds are read only", nil)
		return true
	}
	q, e := strictOAuthQuery(r.URL.RawQuery)
	if e != nil {
		response(w, 400, -1, "invalid history query", nil)
		return true
	}
	for k := range q {
		if k != "merchant_id" && k != "after_id" && k != "limit" {
			response(w, 400, -1, "unknown history parameter", nil)
			return true
		}
	}
	mid := session.User.ID
	if role == "admin" {
		var ok bool
		mid, ok = positiveFundsID(q.Get("merchant_id"))
		if !ok {
			response(w, 400, -1, "explicit merchant_id required", nil)
			return true
		}
	} else if role != "merchant" || q.Has("merchant_id") {
		response(w, 403, -1, "merchant scope comes from original session", nil)
		return true
	}
	after := int64(0)
	limit := 20
	if q.Has("after_id") {
		v := q.Get("after_id")
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 || strconv.FormatInt(n, 10) != v {
			response(w, 400, -1, "canonical after_id required", nil)
			return true
		}
		after = n
	}
	if q.Has("limit") {
		v := q.Get("limit")
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 || strconv.Itoa(n) != v {
			response(w, 400, -1, "bounded limit required", nil)
			return true
		}
		limit = n
	}
	if s.LegacyRefundHistory == nil {
		response(w, 503, -1, "legacy history reader unavailable", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rows, err := s.LegacyRefundHistory.List(ctx, mid, after, limit)
	if err != nil {
		response(w, 409, -1, "historical source verification failed", nil)
		return true
	}
	next := ""
	if len(rows) == limit {
		next = strconv.FormatInt(rows[len(rows)-1].ID, 10)
	}
	response(w, 200, 0, "historical assertions only; no verified payment source", map[string]any{"items": rows, "next_cursor": next})
	return true
}
