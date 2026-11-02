package server

import (
	"encoding/json"
	"errors"
	"goepay/internal/identity"
	"goepay/internal/voiceconfig230"
	"net/http"
)

func (s *Server) voiceSettings230(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) != 3 || parts[1] != "settings" || parts[2] != "voice" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.Issuer != nil || session.User.AdminClass != "super" {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	store, e := voiceconfig230.New(s.Pool, s.CheckoutKey)
	if e != nil {
		response(w, 503, -1, "voice configuration unavailable", nil)
		return true
	}
	a := voiceconfig230.Actor{User: session.User, Token: token, CSRF: session.CSRF}
	var out voiceconfig230.View
	switch r.Method {
	case "GET":
		out, e = store.Read(r.Context(), a)
	case "PUT":
		var in voiceconfig230.Input
		var fields map[string]json.RawMessage
		if !decodeFundsJSON(w, r, &fields) {
			return true
		}
		if len(fields) != 5 {
			response(w, 400, -1, "complete voice settings required", nil)
			return true
		}
		for _, key := range []string{"expected_revision", "enabled", "credential_action", "app_id", "secret"} {
			raw, ok := fields[key]
			if !ok || string(raw) == "null" {
				response(w, 400, -1, "complete voice settings required", nil)
				return true
			}
		}
		raw, _ := json.Marshal(fields)
		if json.Unmarshal(raw, &in) != nil {
			response(w, 400, -1, "invalid voice settings types", nil)
			return true
		}
		out, e = store.Update(r.Context(), a, in)
	default:
		w.Header().Set("Allow", "GET, PUT")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if e != nil {
		status := 503
		if errors.Is(e, identity.ErrAdminSession) {
			status = 403
		} else if errors.Is(e, voiceconfig230.ErrInvalid) {
			status = 422
		} else if errors.Is(e, voiceconfig230.ErrConflict) {
			status = 409
		}
		response(w, status, -1, "voice configuration could not be confirmed; reload before retrying", nil)
	} else {
		response(w, 200, 0, "", out)
	}
	return true
}
