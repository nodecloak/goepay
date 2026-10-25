package server

import (
	"errors"
	"goepay/internal/backoffice"
	"goepay/internal/identity"
	"net/http"
	"strconv"
)

func (s *Server) invitationCodesConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "invitation-codes" {
		return false
	}
	if role != "admin" || session.Issuer != nil {
		response(w, 403, -1, "own administrator session required", nil)
		return true
	}
	svc := backoffice.New(s.Pool, s.Store)
	p := backoffice.Principal{Role: role, SubjectID: session.User.ID, Username: session.User.Username, AdminClass: session.User.AdminClass}
	fail := func(e error) {
		switch {
		case errors.Is(e, backoffice.ErrInvalid):
			response(w, 400, -1, "invalid invitation input", nil)
		case errors.Is(e, backoffice.ErrConflict):
			response(w, 409, -1, "invitation request changed; use original request", nil)
		case errors.Is(e, backoffice.ErrForbidden):
			response(w, 403, -1, "invitation access forbidden", nil)
		default:
			s.fail(w, e)
		}
	}
	if len(parts) == 2 && r.Method == "GET" {
		q := r.URL.Query()
		for _, key := range []string{"page", "limit", "search", "status"} {
			if len(q[key]) > 1 {
				fail(backoffice.ErrInvalid)
				return true
			}
		}
		page, limit := 1, 20
		var e error
		if q.Has("page") {
			page, e = strconv.Atoi(q.Get("page"))
			if e != nil || page < 1 {
				fail(backoffice.ErrInvalid)
				return true
			}
		}
		if q.Has("limit") {
			limit, e = strconv.Atoi(q.Get("limit"))
			if e != nil || limit < 1 {
				fail(backoffice.ErrInvalid)
				return true
			}
		}
		items, total, e := svc.Invitations(r.Context(), p, backoffice.Query{Page: page, Limit: limit, Search: q.Get("search")}, q.Get("status"))
		if e != nil {
			fail(e)
		} else {
			response(w, 200, 0, "", map[string]any{"items": items, "total": total, "page": page, "limit": limit})
		}
		return true
	}
	if len(parts) == 3 && r.Method == "POST" {
		switch parts[2] {
		case "generate":
			var in struct {
				RequestID string `json:"request_id"`
				Count     int    `json:"count"`
			}
			if !decode(w, r, &in) {
				return true
			}
			out, e := svc.GenerateInvitations(r.Context(), p, in.RequestID, in.Count)
			if e != nil {
				fail(e)
			} else {
				response(w, 200, 0, "", out)
			}
			return true
		case "clear":
			var in struct {
				RequestID string `json:"request_id"`
				Scope     string `json:"scope"`
				Confirm   bool   `json:"confirm"`
			}
			if !decode(w, r, &in) {
				return true
			}
			out, e := svc.ClearInvitations(r.Context(), p, in.RequestID, in.Scope, in.Confirm)
			if e != nil {
				fail(e)
			} else {
				response(w, 200, 0, "", out)
			}
			return true
		}
	}
	response(w, 405, -1, "unsupported invitation action", nil)
	return true
}
