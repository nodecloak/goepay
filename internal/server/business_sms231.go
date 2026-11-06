package server

import (
	"encoding/json"
	"errors"
	"goepay/internal/businesssms"
	"goepay/internal/identity"
	"goepay/internal/smsgateway"
	"net/http"
)

func (s *Server) businessSMSSettings231(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) != 3 || parts[1] != "settings" || parts[2] != "business-sms" {
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
	store, e := businesssms.New(s.Pool, s.CheckoutKey)
	if e != nil {
		response(w, 503, -1, "business SMS unavailable", nil)
		return true
	}
	actor := smsgateway.Actor{User: session.User, Token: token, Delegated: session.Issuer != nil}
	var v businesssms.View
	switch r.Method {
	case "GET":
		v, e = store.Read(r.Context(), actor)
	case "PUT":
		var fields map[string]json.RawMessage
		if !decodeFundsJSON(w, r, &fields) {
			return true
		}
		if len(fields) != 2 || fields["expected_revision"] == nil || fields["templates"] == nil || string(fields["expected_revision"]) == "null" || string(fields["templates"]) == "null" {
			response(w, 400, -1, "complete business SMS snapshot required", nil)
			return true
		}
		raw, _ := json.Marshal(fields)
		var templates map[string]json.RawMessage
		if json.Unmarshal(fields["templates"], &templates) != nil || len(templates) != 9 {
			response(w, 400, -1, "complete string templates required", nil)
			return true
		}
		for _, value := range templates {
			var text string
			if string(value) == "null" || json.Unmarshal(value, &text) != nil {
				response(w, 400, -1, "explicit template strings required", nil)
				return true
			}
		}
		var in businesssms.Input
		if json.Unmarshal(raw, &in) != nil {
			response(w, 400, -1, "invalid business SMS field types", nil)
			return true
		}
		v, e = store.Save(r.Context(), actor, in)
	default:
		w.Header().Set("Allow", "GET, PUT")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if e != nil {
		status := 503
		if errors.Is(e, businesssms.ErrConflict) {
			status = 409
		} else if errors.Is(e, businesssms.ErrInvalid) {
			status = 422
		} else if errors.Is(e, identity.ErrAdminSession) {
			status = 403
		}
		response(w, status, -1, "business SMS configuration not confirmed; reload before retrying", nil)
	} else {
		response(w, 200, 0, "", v)
	}
	return true
}
