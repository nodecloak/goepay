package server

import (
	"errors"
	"goepay/internal/backoffice"
	"goepay/internal/identity"
	"net/http"
)

func (s *Server) orderBlacklistConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) != 4 || parts[1] != "orders" || parts[3] != "blacklist" {
		return false
	}
	if role != "admin" || session.Issuer != nil || (session.User.AdminClass != "super" && session.User.AdminClass != "operator") {
		response(w, 403, -1, "own administrator session required", nil)
		return true
	}
	if r.Method != "POST" || r.URL.RawQuery != "" {
		response(w, 405, -1, "blacklist POST required", nil)
		return true
	}
	var in struct {
		Kind          string `json:"kind"`
		ExpectedValue string `json:"expected_value"`
		Days          int    `json:"days"`
		Reason        string `json:"reason"`
		ExternalNo    string `json:"external_no"`
	}
	if !decode(w, r, &in) {
		return true
	}
	o, e := s.Store.OrderByTradeNo(r.Context(), parts[2])
	if e != nil {
		s.fail(w, e)
		return true
	}
	item, e := backoffice.New(s.Pool, s.Store).AddOrderBlacklist(r.Context(), backoffice.Principal{Role: role, SubjectID: session.User.ID, Username: session.User.Username, AdminClass: session.User.AdminClass}, o.ID, in.Kind, in.ExpectedValue, in.Days, in.Reason, in.ExternalNo)
	switch {
	case errors.Is(e, backoffice.ErrInvalid):
		response(w, 422, -1, "invalid blacklist value or expiry", nil)
	case errors.Is(e, backoffice.ErrConflict):
		response(w, 409, -1, "order source or original request changed", nil)
	case errors.Is(e, backoffice.ErrForbidden):
		response(w, 403, -1, "blacklist access denied", nil)
	case errors.Is(e, backoffice.ErrNotFound):
		response(w, 404, -1, "original blacklist record no longer exists", nil)
	case e != nil:
		s.fail(w, e)
	default:
		status := 201
		if item.Replayed {
			status = 200
		}
		response(w, status, 0, "", item)
	}
	return true
}
