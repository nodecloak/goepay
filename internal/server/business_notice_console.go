package server

import (
	"context"
	"errors"
	"goepay/internal/businessnotice"
	"goepay/internal/identity"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func (s *Server) businessNoticeConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, p []string) bool {
	if len(p) < 2 || p[1] != "business-notices" {
		return false
	}
	if role != "admin" || session.User.Role != "admin" || session.User.ID <= 0 || session.User.Username == "" || session.Issuer != nil || session.User.AdminClass != "super" && session.User.AdminClass != "operator" {
		response(w, 403, -1, "own authorized administrator required", nil)
		return true
	}
	if len(p) != 2 {
		response(w, 404, -1, "business notice operation unavailable", nil)
		return true
	}
	if r.Method != "GET" {
		response(w, 405, -1, "read-only business notice history", nil)
		return true
	}
	before, limit := int64(0), 20
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		response(w, 400, -1, "canonical business notice pagination required", nil)
		return true
	}
	for key, values := range query {
		if len(values) != 1 || key != "before_id" && key != "limit" {
			response(w, 400, -1, "canonical business notice pagination required", nil)
			return true
		}
		n, e := strconv.ParseInt(values[0], 10, 64)
		if e != nil || n < 0 || strconv.FormatInt(n, 10) != values[0] || key == "limit" && (n < 1 || n > 100) {
			response(w, 400, -1, "canonical business notice pagination required", nil)
			return true
		}
		if key == "limit" {
			limit = int(n)
		} else {
			before = n
		}
	}
	if s.BusinessNotice == nil {
		response(w, 503, -1, "business notice history unavailable", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	items, err := s.BusinessNotice.Events(ctx, before, limit)
	if err != nil {
		if errors.Is(err, businessnotice.ErrConflict) || errors.Is(err, businessnotice.ErrState) || errors.Is(err, businessnotice.ErrNotFound) {
			response(w, 409, -1, "original business notice proof conflict", nil)
		} else {
			s.fail(w, err)
		}
		return true
	}
	cursor := ""
	if len(items) == limit {
		cursor = strconv.FormatInt(items[len(items)-1].ID, 10)
	}
	identityHeaders(w)
	response(w, 200, 0, "", struct {
		Items      []businessnotice.EventView `json:"items"`
		NextCursor string                     `json:"next_cursor"`
	}{items, cursor})
	return true
}
